package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type autoRoutingServiceRepoStub struct {
	AutoRoutingRepository
	profile       *AutoRoutingProfile
	keyConfig     *APIKeyAutoRoutingConfig
	replaceCalls  int
	profileWrites int
}

func (r *autoRoutingServiceRepoStub) CreateAutoRoutingProfile(_ context.Context, profile *AutoRoutingProfile) error {
	r.profileWrites++
	profile.ID = 17
	profile.Revision = 1
	cloned := cloneAutoRoutingProfile(profile)
	r.profile = &cloned
	return nil
}

func (r *autoRoutingServiceRepoStub) GetAPIKeyAutoRouting(context.Context, int64) (*APIKeyAutoRoutingConfig, error) {
	return cloneAPIKeyAutoRoutingConfig(r.keyConfig), nil
}

func (r *autoRoutingServiceRepoStub) ReplaceAPIKeyAutoRouting(
	context.Context,
	int64,
	*APIKeyAutoRoutingConfig,
	*int64,
	int64,
) error {
	r.replaceCalls++
	return nil
}

type autoRoutingServiceGroupRepoStub struct {
	GroupRepository
	group *Group
}

func (r *autoRoutingServiceGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.group == nil || r.group.ID != id {
		return nil, ErrGroupNotFound
	}
	cloned := *r.group
	return &cloned, nil
}

type autoRoutingCandidateGroupRepoStub struct {
	GroupRepository
	groups map[int64]*Group
	errors map[int64]error
}

func (r *autoRoutingCandidateGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if err := r.errors[id]; err != nil {
		return nil, err
	}
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cloned := *group
	return &cloned, nil
}

type autoRoutingServiceAPIKeyRepoStub struct {
	APIKeyRepository
	key *APIKey
}

func (r *autoRoutingServiceAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	if r.key == nil {
		return nil, ErrAPIKeyNotFound
	}
	cloned := *r.key
	return &cloned, nil
}

type autoRoutingServiceRateRepoStub struct {
	UserGroupRateRepository
	rate        *float64
	rpmOverride *int
	rateErr     error
	rpmErr      error
}

func (r *autoRoutingServiceRateRepoStub) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	return r.rate, r.rateErr
}

func (r *autoRoutingServiceRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return r.rpmOverride, r.rpmErr
}

type autoRoutingServiceUserRepoStub struct {
	UserRepository
	user *User
}

func (r *autoRoutingServiceUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	cloned := *r.user
	return &cloned, nil
}

type autoRoutingServiceSubscriptionRepoStub struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
}

func (r *autoRoutingServiceSubscriptionRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), r.subscriptions...), nil
}

func TestCreateAutoRoutingProfileStartsDisabled(t *testing.T) {
	repo := &autoRoutingServiceRepoStub{}
	groupRepo := &autoRoutingServiceGroupRepoStub{group: &Group{
		ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
	}}
	service := &AutoRoutingService{repo: repo, groupRepo: groupRepo}

	profile, err := service.CreateProfile(context.Background(), &AutoRoutingProfile{
		Name: "Auto", Status: AutoRoutingProfileStatusActive,
		Members: []AutoRoutingProfileMember{{GroupID: 3, SortOrder: 0}},
	})

	require.NoError(t, err)
	require.Equal(t, AutoRoutingProfileStatusDisabled, profile.Status)
	require.Equal(t, AutoRoutingProfileStatusDisabled, repo.profile.Status)
	require.Equal(t, 1, repo.profileWrites)
}

func TestResolveCandidatesFiltersRemovedCustomMembersAndDeletedGroups(t *testing.T) {
	group := &Group{
		ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
	}
	service := &AutoRoutingService{
		apiKeys: &APIKeyService{
			userRepo:    &autoRoutingServiceUserRepoStub{user: &User{ID: 4, Status: StatusActive}},
			userSubRepo: &autoRoutingServiceSubscriptionRepoStub{},
		},
		groupRepo: &autoRoutingCandidateGroupRepoStub{
			groups: map[int64]*Group{2: group},
		},
	}
	profile := &AutoRoutingProfile{Members: []AutoRoutingProfileMember{
		{GroupID: 2, SortOrder: 0},
		{GroupID: 3, SortOrder: 1},
	}}
	config := &APIKeyAutoRoutingConfig{
		SelectionMode: AutoRoutingSelectionCustom,
		GroupIDs:      []int64{1, 2, 3},
	}

	candidates, _, err := service.resolveCandidates(context.Background(), 4, profile, config)

	require.NoError(t, err)
	require.Equal(t, []int64{2}, candidateIDs(candidates))
}

func TestResolveCandidatesRejectsCustomSelectionWhenAllMembersAreRemoved(t *testing.T) {
	service := &AutoRoutingService{
		apiKeys: &APIKeyService{
			userRepo:    &autoRoutingServiceUserRepoStub{user: &User{ID: 4, Status: StatusActive}},
			userSubRepo: &autoRoutingServiceSubscriptionRepoStub{},
		},
		groupRepo: &autoRoutingCandidateGroupRepoStub{},
	}
	profile := &AutoRoutingProfile{Members: []AutoRoutingProfileMember{
		{GroupID: 2, SortOrder: 0},
	}}
	config := &APIKeyAutoRoutingConfig{
		SelectionMode: AutoRoutingSelectionCustom,
		GroupIDs:      []int64{1},
	}

	candidates, _, err := service.resolveCandidates(context.Background(), 4, profile, config)

	require.ErrorIs(t, err, ErrAutoRoutingCandidateUnavailable)
	require.Empty(t, candidates)
}

func TestResolveCandidatesRejectsFreshlyDisabledUser(t *testing.T) {
	service := &AutoRoutingService{
		apiKeys: &APIKeyService{
			userRepo: &autoRoutingServiceUserRepoStub{user: &User{
				ID: 4, Status: StatusDisabled,
			}},
			userSubRepo: &autoRoutingServiceSubscriptionRepoStub{},
		},
		groupRepo: &autoRoutingCandidateGroupRepoStub{
			groups: map[int64]*Group{
				2: {ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true},
			},
		},
	}
	profile := &AutoRoutingProfile{Members: []AutoRoutingProfileMember{{GroupID: 2}}}
	config := &APIKeyAutoRoutingConfig{
		SelectionMode: AutoRoutingSelectionCustom,
		GroupIDs:      []int64{2},
	}

	candidates, _, err := service.resolveCandidates(context.Background(), 4, profile, config)

	require.ErrorIs(t, err, ErrUserNotActive)
	require.Empty(t, candidates)
}

func TestResolveCandidatesFailsClosedOnGroupRepositoryError(t *testing.T) {
	groupRepo := &autoRoutingCandidateGroupRepoStub{
		groups: map[int64]*Group{
			2: {ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true},
		},
		errors: map[int64]error{3: errors.New("database unavailable")},
	}
	service := &AutoRoutingService{
		apiKeys: &APIKeyService{
			userRepo:    &autoRoutingServiceUserRepoStub{user: &User{ID: 4, Status: StatusActive}},
			userSubRepo: &autoRoutingServiceSubscriptionRepoStub{},
		},
		groupRepo: groupRepo,
	}
	profile := &AutoRoutingProfile{Members: []AutoRoutingProfileMember{
		{GroupID: 2, SortOrder: 0},
		{GroupID: 3, SortOrder: 1},
	}}
	config := &APIKeyAutoRoutingConfig{
		SelectionMode: AutoRoutingSelectionCustom,
		GroupIDs:      []int64{2, 3},
	}

	candidates, _, err := service.resolveCandidates(context.Background(), 4, profile, config)

	require.ErrorIs(t, err, ErrAutoRoutingUnavailable)
	require.Empty(t, candidates)
}

func TestExecutionPreflightSkipsSubscriptionCapsAndUsesNextCandidate(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		group func() *Group
		sub   func() *UserSubscription
	}{
		{
			name: "daily",
			group: func() *Group {
				limit := 1.0
				return &Group{
					ID: 1, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit,
				}
			},
			sub: func() *UserSubscription {
				return &UserSubscription{
					ID: 1, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
					DailyUsageUSD: 1, DailyWindowStart: &now,
				}
			},
		},
		{
			name: "weekly",
			group: func() *Group {
				limit := 1.0
				return &Group{
					ID: 1, SubscriptionType: SubscriptionTypeSubscription, WeeklyLimitUSD: &limit,
				}
			},
			sub: func() *UserSubscription {
				return &UserSubscription{
					ID: 1, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
					WeeklyUsageUSD: 1, WeeklyWindowStart: &now,
				}
			},
		},
		{
			name: "monthly",
			group: func() *Group {
				limit := 1.0
				return &Group{
					ID: 1, SubscriptionType: SubscriptionTypeSubscription, MonthlyLimitUSD: &limit,
				}
			},
			sub: func() *UserSubscription {
				return &UserSubscription{
					ID: 1, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
					MonthlyUsageUSD: 1, MonthlyWindowStart: &now,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := autoRoutingCandidate{group: test.group(), subscription: test.sub()}
			second := autoRoutingCandidate{group: &Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true}}

			filtered := filterAutoRoutingExecutionCandidates([]autoRoutingCandidate{first, second}, false, now)

			require.Equal(t, []int64{2}, candidateIDs(filtered))
		})
	}
}

func TestExecutionPreflightAppliesExpiredWindowResetsWithoutMutation(t *testing.T) {
	now := time.Now()
	dailyLimit := 1.0
	weeklyLimit := 1.0
	monthlyLimit := 1.0
	dailyWindowStart := now.Add(-48 * time.Hour)
	weeklyWindowStart := now.Add(-8 * 24 * time.Hour)
	monthlyWindowStart := now.Add(-31 * 24 * time.Hour)
	subscription := &UserSubscription{
		ID: 1, Status: SubscriptionStatusActive,
		StartsAt: now.Add(-100 * 24 * time.Hour), ExpiresAt: now.Add(100 * 24 * time.Hour),
		DailyUsageUSD: 2, WeeklyUsageUSD: 3, MonthlyUsageUSD: 4,
		DailyWindowStart: &dailyWindowStart, WeeklyWindowStart: &weeklyWindowStart,
		MonthlyWindowStart: &monthlyWindowStart,
	}
	candidate := autoRoutingCandidate{
		group: &Group{
			ID: 1, SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD: &dailyLimit, WeeklyLimitUSD: &weeklyLimit, MonthlyLimitUSD: &monthlyLimit,
		},
		subscription: subscription,
	}

	filtered := filterAutoRoutingExecutionCandidates([]autoRoutingCandidate{candidate}, false, now)

	require.Equal(t, []int64{1}, candidateIDs(filtered))
	require.Equal(t, 2.0, subscription.DailyUsageUSD)
	require.Equal(t, 3.0, subscription.WeeklyUsageUSD)
	require.Equal(t, 4.0, subscription.MonthlyUsageUSD)
}

func TestExecutionPreflightPreservesSimpleModeSubscriptionBehavior(t *testing.T) {
	now := time.Now()
	limit := 1.0
	candidate := autoRoutingCandidate{
		group: &Group{
			ID: 1, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit,
		},
		subscription: &UserSubscription{
			ID: 1, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
			DailyUsageUSD: 1, DailyWindowStart: &now,
		},
	}

	filtered := filterAutoRoutingExecutionCandidates([]autoRoutingCandidate{candidate}, true, now)

	require.Equal(t, []int64{1}, candidateIDs(filtered))
}

func TestGetKeyRoutingStateUsesAuthoritativeFixedRevision(t *testing.T) {
	key := &APIKey{ID: 9, UserID: 4, RoutingMode: AutoRoutingModeFixed, RoutingRevision: 12}
	apiKeys := &APIKeyService{apiKeyRepo: &autoRoutingServiceAPIKeyRepoStub{key: key}}
	service := &AutoRoutingService{
		apiKeys: apiKeys,
		repo:    &autoRoutingServiceRepoStub{},
	}

	state, err := service.GetKeyRoutingState(context.Background(), key.UserID, key.ID)

	require.NoError(t, err)
	require.Equal(t, AutoRoutingModeFixed, state.Mode)
	require.Equal(t, int64(12), state.Revision)
	require.Nil(t, state.Auto)
	require.Nil(t, state.FixedGroupID)
}

func TestUpdateKeyRoutingRejectsStaleAuthoritativeRevision(t *testing.T) {
	key := &APIKey{ID: 9, UserID: 4, Key: "api-key", RoutingMode: AutoRoutingModeFixed, RoutingRevision: 12}
	repo := &autoRoutingServiceRepoStub{}
	service := &AutoRoutingService{
		apiKeys: &APIKeyService{apiKeyRepo: &autoRoutingServiceAPIKeyRepoStub{key: key}},
		repo:    repo,
	}

	_, err := service.UpdateKeyRouting(context.Background(), key.UserID, key.ID, nil, nil, 11)

	require.ErrorIs(t, err, ErrAutoRoutingRevisionConflict)
	require.Zero(t, repo.replaceCalls)
}

func TestFreezeAutoRoutingBillingSnapshotsSelectedGroupRates(t *testing.T) {
	groupRate := 1.75
	rpmOverride := 23
	rateRepo := &autoRoutingServiceRateRepoStub{rate: &groupRate, rpmOverride: &rpmOverride}
	service := &AutoRoutingService{apiKeys: &APIKeyService{userGroupRateRepo: rateRepo}}
	key := &APIKey{
		AutoRouting: &AutoRoutingMetadata{ProfileID: 2},
		User:        &User{UserGroupRPMOverride: intPointer(99)},
	}
	group := &Group{ID: 6, RateMultiplier: 1.25}

	err := service.freezeAutoRoutingBilling(context.Background(), key, group, 4)

	require.NoError(t, err)
	require.NotNil(t, key.AutoRouting.RateMultiplier)
	require.Equal(t, 1.75, *key.AutoRouting.RateMultiplier)
	require.False(t, key.AutoRouting.PricingAt.IsZero())
	require.Equal(t, 23, *key.User.UserGroupRPMOverride)
	require.NotSame(t, &rpmOverride, key.User.UserGroupRPMOverride)
}

func TestFreezeAutoRoutingBillingFailsClosedOnRateLookupErrors(t *testing.T) {
	tests := []struct {
		name     string
		rateErr  error
		rpmError error
	}{
		{name: "group rate lookup", rateErr: errors.New("database unavailable")},
		{name: "RPM override lookup", rpmError: errors.New("database unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rateRepo := &autoRoutingServiceRateRepoStub{rateErr: test.rateErr, rpmErr: test.rpmError}
			service := &AutoRoutingService{apiKeys: &APIKeyService{userGroupRateRepo: rateRepo}}
			key := &APIKey{
				AutoRouting: &AutoRoutingMetadata{ProfileID: 2},
				User:        &User{},
			}
			group := &Group{ID: 6, RateMultiplier: 1.25}

			err := service.freezeAutoRoutingBilling(context.Background(), key, group, 4)

			require.ErrorIs(t, err, ErrAutoRoutingUnavailable)
			require.Nil(t, key.AutoRouting.RateMultiplier)
			require.True(t, key.AutoRouting.PricingAt.IsZero())
		})
	}
}

func TestCloneAutoRoutingAPIKeyCopiesFrozenRate(t *testing.T) {
	rate := 1.5
	pricedAt := time.Now()
	key := &APIKey{AutoRouting: &AutoRoutingMetadata{
		RateMultiplier: &rate,
		PricingAt:      pricedAt,
	}}

	cloned := cloneAutoRoutingAPIKey(key)
	*cloned.AutoRouting.RateMultiplier = 2

	require.Equal(t, 1.5, *key.AutoRouting.RateMultiplier)
	require.Equal(t, pricedAt, cloned.AutoRouting.PricingAt)
}

func intPointer(value int) *int {
	return &value
}
