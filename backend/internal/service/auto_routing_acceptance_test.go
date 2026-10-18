package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type autoRoutingAcceptanceUserRepo struct {
	UserRepository
	user *User
}

func (r *autoRoutingAcceptanceUserRepo) GetByID(context.Context, int64) (*User, error) {
	if r.user == nil {
		return nil, nil
	}
	return cloneAutoRoutingUser(r.user), nil
}

type autoRoutingAcceptanceSubscriptionRepo struct {
	UserSubscriptionRepository
}

func (autoRoutingAcceptanceSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

type autoRoutingAcceptanceGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *autoRoutingAcceptanceGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, nil
	}
	return cloneAutoRoutingGroup(group), nil
}

type autoRoutingAcceptanceRateRepo struct {
	UserGroupRateRepository
	rates      map[int64]float64
	rpm        map[int64]int
	rateGroups []int64
	rpmGroups  []int64
}

func (r *autoRoutingAcceptanceRateRepo) GetByUserAndGroup(_ context.Context, userID, groupID int64) (*float64, error) {
	if userID <= 0 {
		return nil, errors.New("invalid test user")
	}
	r.rateGroups = append(r.rateGroups, groupID)
	rate, ok := r.rates[groupID]
	if !ok {
		return nil, nil
	}
	return &rate, nil
}

func (r *autoRoutingAcceptanceRateRepo) GetRPMOverrideByUserAndGroup(_ context.Context, userID, groupID int64) (*int, error) {
	if userID <= 0 {
		return nil, errors.New("invalid test user")
	}
	r.rpmGroups = append(r.rpmGroups, groupID)
	rpm, ok := r.rpm[groupID]
	if !ok {
		return nil, nil
	}
	return &rpm, nil
}

type autoRoutingAcceptanceAccountRepo struct {
	AccountRepository
	accounts map[int64][]Account
}

func (r *autoRoutingAcceptanceAccountRepo) ListModelAvailabilityCandidates(
	_ context.Context,
	groupID *int64,
	_ []string,
	_ bool,
) ([]Account, error) {
	if groupID == nil {
		return nil, errors.New("expected group-scoped model lookup")
	}
	return append([]Account(nil), r.accounts[*groupID]...), nil
}

type autoRoutingAcceptanceAPIKeyRepo struct {
	APIKeyRepository
	key *APIKey
}

func (r *autoRoutingAcceptanceAPIKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	return cloneAutoRoutingAPIKey(r.key), nil
}

type autoRoutingAcceptanceRPMCache struct {
	UserRPMCache
	userID             int64
	groupID            int64
	executionID        int64
	userLimit          int
	groupLimit         int
	result             int
	err                error
	admissionCalls     int
	userGroupIncrCalls int
	userIncrCalls      int
}

func (c *autoRoutingAcceptanceRPMCache) AdmitAutoRoutingRPM(
	_ context.Context,
	userID, groupID, executionID, _ int64,
	userLimit, groupLimit int,
) (int, error) {
	c.admissionCalls++
	c.userID = userID
	c.groupID = groupID
	c.executionID = executionID
	c.userLimit = userLimit
	c.groupLimit = groupLimit
	return c.result, c.err
}

func (c *autoRoutingAcceptanceRPMCache) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	c.userGroupIncrCalls++
	return c.userGroupIncrCalls, nil
}

func (c *autoRoutingAcceptanceRPMCache) IncrementUserRPM(context.Context, int64) (int, error) {
	c.userIncrCalls++
	return c.userIncrCalls, nil
}

func (*autoRoutingAcceptanceRPMCache) GetUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (*autoRoutingAcceptanceRPMCache) GetUserRPM(context.Context, int64) (int, error) {
	return 0, nil
}

type autoRoutingAcceptanceBillingCache struct {
	BillingCache
	balance float64
}

func (c *autoRoutingAcceptanceBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

type autoRoutingAcceptanceRoutingRepo struct {
	AutoRoutingRepository
	profile        *AutoRoutingProfile
	config         *APIKeyAutoRoutingConfig
	execution      *AutoRoutingExecution
	createCalls    int
	executionReads int
}

func (r *autoRoutingAcceptanceRoutingRepo) GetAutoRoutingProfile(context.Context, int64) (*AutoRoutingProfile, error) {
	if r.profile == nil {
		return nil, nil
	}
	profile := cloneAutoRoutingProfile(r.profile)
	return &profile, nil
}

func (r *autoRoutingAcceptanceRoutingRepo) GetAPIKeyAutoRouting(context.Context, int64) (*APIKeyAutoRoutingConfig, error) {
	return cloneAPIKeyAutoRoutingConfig(r.config), nil
}

func (r *autoRoutingAcceptanceRoutingRepo) GetAutoRoutingExecution(_ context.Context, apiKeyID int64, requestID string) (*AutoRoutingExecution, error) {
	r.executionReads++
	if r.execution == nil || r.execution.APIKeyID != apiKeyID || r.execution.RequestID != requestID {
		return nil, nil
	}
	return cloneAutoRoutingExecution(r.execution), nil
}

func (r *autoRoutingAcceptanceRoutingRepo) CreateOrGetAutoRoutingExecution(
	_ context.Context,
	input AutoRoutingExecutionInput,
) (*AutoRoutingExecution, bool, error) {
	r.createCalls++
	if r.execution != nil {
		return cloneAutoRoutingExecution(r.execution), false, nil
	}
	r.execution = &AutoRoutingExecution{
		ID:                501,
		APIKeyID:          input.APIKeyID,
		RequestID:         input.RequestID,
		PayloadHash:       input.PayloadHash,
		ProfileID:         input.ProfileID,
		ProfileRevision:   input.ProfileRevision,
		RoutingRevision:   input.RoutingRevision,
		CandidateGroupIDs: append([]int64(nil), input.CandidateGroupIDs...),
		Status:            AutoRouteExecutionPrepared,
		ActualGroupID:     int64Pointer(input.SelectedGroupID),
		SettlementStatus:  "pending",
	}
	return cloneAutoRoutingExecution(r.execution), true, nil
}

func (r *autoRoutingAcceptanceRoutingRepo) ClaimAutoRoutingAttemptDispatch(_ context.Context, attempt *AutoRoutingAttempt) (bool, error) {
	if r.execution == nil ||
		r.execution.ID != attempt.ExecutionID ||
		r.execution.Status != AutoRouteExecutionPrepared ||
		r.execution.ActualGroupID == nil ||
		*r.execution.ActualGroupID != attempt.GroupID {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionDispatching
	r.execution.CurrentAttempt = attempt.AttemptNo
	return true, nil
}

func (r *autoRoutingAcceptanceRoutingRepo) CompleteAutoRoutingAttempt(_ context.Context, executionID int64, attemptNo int, resultReference string) (bool, error) {
	if r.execution == nil ||
		r.execution.ID != executionID ||
		r.execution.Status != AutoRouteExecutionDispatching ||
		r.execution.CurrentAttempt != attemptNo {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionCompleted
	r.execution.ResultReference = &resultReference
	return true, nil
}

func (r *autoRoutingAcceptanceRoutingRepo) MarkAutoRoutingAttemptUnknown(_ context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error) {
	if r.execution == nil ||
		r.execution.ID != executionID ||
		r.execution.Status != AutoRouteExecutionDispatching ||
		r.execution.CurrentAttempt != attemptNo {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionUnknown
	r.execution.EvidenceCode = &evidenceCode
	return true, nil
}

func TestAutoRoutingAcceptanceSelectsAuthorizedGroupAndFreezesItsBilling(t *testing.T) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.rateRepo.rates[202] = 2.75
	fixture.rateRepo.rpm[202] = 0

	preparation, err := fixture.routing.PrepareRequestKey(
		context.Background(),
		fixture.key,
		AutoRoutingRequest{
			Endpoint:  AutoModelEndpointChat,
			ModelID:   "routed-model",
			RequestID: "acceptance:route-1",
			Body:      []byte(`{"model":"routed-model"}`),
		},
	)
	require.NoError(t, err)
	require.NotNil(t, preparation)
	require.Equal(t, int64(202), *preparation.EffectiveKey.GroupID)
	require.Equal(t, int64(202), preparation.EffectiveKey.Group.ID)
	require.Equal(t, 1.6, preparation.EffectiveKey.Group.RateMultiplier)
	require.Equal(t, 2.75, *preparation.EffectiveKey.AutoRouting.RateMultiplier)
	require.False(t, preparation.EffectiveKey.AutoRouting.PricingAt.IsZero())
	require.NotNil(t, preparation.EffectiveKey.User.UserGroupRPMOverride)
	require.Zero(t, *preparation.EffectiveKey.User.UserGroupRPMOverride)
	multiplier, _, _ := (&GatewayService{}).recordUsageMultipliers(
		context.Background(),
		preparation.EffectiveKey,
		preparation.EffectiveKey.User,
		preparation.EffectiveKey.AutoRouting.PricingAt,
	)
	require.Equal(t, 2.75, multiplier)
	require.Equal(t, []int64{202}, fixture.rateRepo.rateGroups)
	require.Equal(t, []int64{202}, fixture.rateRepo.rpmGroups)
	require.Nil(t, fixture.key.GroupID)
	require.Nil(t, fixture.key.AutoRouting)
	require.Equal(t, 1, fixture.repo.createCalls)

	preparation.EffectiveKey.User.RPMLimit = 17
	rpmCache := &autoRoutingAcceptanceRPMCache{}
	billing := &BillingCacheService{
		cache:        &autoRoutingAcceptanceBillingCache{balance: 100},
		userRPMCache: rpmCache,
	}
	for range 2 {
		require.NoError(t, billing.CheckBillingEligibility(
			context.Background(),
			preparation.EffectiveKey.User,
			preparation.EffectiveKey,
			preparation.EffectiveKey.Group,
			nil,
			"",
		))
	}
	require.Zero(t, rpmCache.admissionCalls, "Auto eligibility must defer RPM admission until dispatch")
	require.Zero(t, rpmCache.userGroupIncrCalls)
	require.Zero(t, rpmCache.userIncrCalls)

	fixture.routing.SetDispatchBilling(billing)
	require.NoError(t, fixture.routing.AdmitDispatch(context.Background(), preparation.EffectiveKey))
	require.Equal(t, 1, rpmCache.admissionCalls)
	require.Equal(t, int64(42), rpmCache.userID)
	require.Equal(t, int64(202), rpmCache.groupID)
	require.Equal(t, int64(501), rpmCache.executionID)
	require.Equal(t, 17, rpmCache.userLimit)
	require.Zero(t, rpmCache.groupLimit, "the selected group's zero override disables only its group cap")

	usage := &UsageLog{GroupID: int64Pointer(*preparation.EffectiveKey.GroupID)}
	applyAutoRoutingUsageMetadata(usage, preparation.EffectiveKey)
	require.Equal(t, int64(202), *usage.GroupID)
	require.Equal(t, "auto", usage.RoutingMode)
	require.Equal(t, int64(9), *usage.AutoProfileID)
	require.Equal(t, int64(4), usage.RoutingRevision)
	require.Equal(t, 1, usage.RouteAttemptCount)
}

func TestAutoRoutingAcceptanceAntigravityGeminiEndpoints(t *testing.T) {
	testCases := []struct {
		name            string
		groupID         int64
		groupPlatform   string
		modelID         string
		accountType     string
		mixedScheduling bool
	}{
		{
			name:          "antigravity group",
			groupID:       27,
			groupPlatform: PlatformAntigravity,
			modelID:       "gemini-3-flash",
			accountType:   AccountTypeAPIKey,
		},
		{
			name:          "antigravity group GPT OSS model",
			groupID:       27,
			groupPlatform: PlatformAntigravity,
			modelID:       "gpt-oss-120b-medium",
			accountType:   AccountTypeAPIKey,
		},
		{
			name:            "gemini group mixed antigravity account",
			groupID:         28,
			groupPlatform:   PlatformGemini,
			modelID:         "gemini-4-argon",
			accountType:     AccountTypeOAuth,
			mixedScheduling: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, endpoint := range []AutoModelEndpoint{
				AutoModelEndpointChat,
				AutoModelEndpointResponses,
			} {
				t.Run(string(endpoint), func(t *testing.T) {
					fixture := newAutoRoutingAcceptancePlatformFixture(
						testCase.groupID,
						testCase.groupPlatform,
						acceptanceAntigravityAccount(
							testCase.groupID+1000,
							testCase.modelID,
							testCase.mixedScheduling,
							testCase.accountType,
						),
					)
					fixture.rateRepo.rates[testCase.groupID] = 2.75
					fixture.rateRepo.rpm[testCase.groupID] = 0

					body := []byte(`{"model":"` + testCase.modelID + `","messages":[{"role":"user","content":"hello"}]}`)
					if endpoint == AutoModelEndpointResponses {
						body = []byte(`{"model":"` + testCase.modelID + `","input":"hello"}`)
					}
					preparation, err := fixture.routing.PrepareRequestKey(
						context.Background(),
						fixture.key,
						AutoRoutingRequest{
							Endpoint:  endpoint,
							ModelID:   testCase.modelID,
							RequestID: "acceptance:" + testCase.name + ":" + string(endpoint),
							Body:      body,
						},
					)
					if testCase.groupPlatform == PlatformGemini && endpoint == AutoModelEndpointResponses {
						require.ErrorIs(t, err, ErrAutoRoutingCandidateUnavailable)
						require.Nil(t, preparation)
						require.Zero(t, fixture.repo.createCalls)
						return
					}
					require.NoError(t, err)
					require.NotNil(t, preparation)
					require.Equal(t, endpoint, preparation.Plan.Endpoint)
					require.Equal(t, testCase.modelID, preparation.Plan.ModelID)
					require.Equal(t, []int64{testCase.groupID}, preparation.Plan.CandidateGroupIDs)
					require.Equal(t, testCase.groupID, preparation.Plan.SelectedGroupID)
					require.Equal(t, testCase.groupID, *preparation.Execution.ActualGroupID)
					require.Equal(t, testCase.groupPlatform, preparation.EffectiveKey.Group.Platform)
					require.Equal(t, testCase.groupID, *preparation.EffectiveKey.GroupID)
					require.Equal(t, testCase.groupID, preparation.EffectiveKey.Group.ID)
					require.Equal(t, 1.6, preparation.EffectiveKey.Group.RateMultiplier)
					require.Equal(t, RoleUser, preparation.EffectiveKey.User.Role)
					require.Equal(t, 2.75, *preparation.EffectiveKey.AutoRouting.RateMultiplier)
					require.Equal(t, int64(2), preparation.EffectiveKey.AutoRouting.ProfileID)
					require.Equal(t, int64(4), preparation.EffectiveKey.AutoRouting.RoutingRevision)
					require.False(t, preparation.EffectiveKey.AutoRouting.PricingAt.IsZero())
					multiplier, _, _ := (&GatewayService{}).recordUsageMultipliers(
						context.Background(),
						preparation.EffectiveKey,
						preparation.EffectiveKey.User,
						preparation.EffectiveKey.AutoRouting.PricingAt,
					)
					require.Equal(t, 2.75, multiplier)
					require.Equal(t, []int64{testCase.groupID}, fixture.rateRepo.rateGroups)
					require.Equal(t, []int64{testCase.groupID}, fixture.rateRepo.rpmGroups)
					require.Equal(t, int64(411), fixture.key.ID)
					require.Nil(t, fixture.key.GroupID)
					require.Nil(t, fixture.key.Group)
					require.Nil(t, fixture.key.AutoRouting)
					require.Equal(t, 1, fixture.repo.createCalls)
				})
			}
		})
	}
}

func TestAutoRoutingAcceptanceGPTResponses(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		model         string
		upstreamModel string
		accountType   string
	}{
		{"GPT 5.4 API key", "gpt-5.4", "gpt-5.4", AccountTypeAPIKey},
		{"GPT 5.5 API key", "gpt-5.5", "gpt-5.5", AccountTypeAPIKey},
		{"GPT 6 API key", "gpt-6", "gpt-6", AccountTypeAPIKey},
		{"GPT 6 Luna API key", "gpt-6-luna", "gpt-6-luna", AccountTypeAPIKey},
		{"GPT 6 Luna OAuth", "gpt-6-luna", "gpt-6-luna", AccountTypeOAuth},
		{"GPT 6 Astra API key", "gpt-6-astra", "gpt-6-astra", AccountTypeAPIKey},
		{"mapped GPT alias API key", "gpt-5.6-luna", "gpt-5.6-terra", AccountTypeAPIKey},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			account := acceptanceOpenAIAccount(90, testCase.model)
			account.Type = testCase.accountType
			account.Credentials["model_mapping"] = map[string]any{testCase.model: testCase.upstreamModel}
			fixture := newAutoRoutingAcceptancePlatformFixture(15, PlatformOpenAI, account)

			preparation, err := fixture.routing.PrepareRequestKey(
				context.Background(),
				fixture.key,
				AutoRoutingRequest{
					Endpoint:  AutoModelEndpointResponses,
					ModelID:   testCase.model,
					RequestID: "acceptance:gpt:" + testCase.name,
					Body:      []byte(`{"model":"` + testCase.model + `","input":"hello","stream":true}`),
				},
			)
			require.NoError(t, err)
			require.NotNil(t, preparation)
			require.Equal(t, int64(15), preparation.Plan.SelectedGroupID)
			require.Equal(t, testCase.model, preparation.Plan.ModelID)
			require.Equal(t, RoleUser, preparation.EffectiveKey.User.Role)
			require.Equal(t, PlatformOpenAI, preparation.EffectiveKey.Group.Platform)

			capability, supported, err := fixture.catalog.ResolveGroupModelCapability(
				context.Background(), fixture.groups[15], testCase.model, AutoModelEndpointResponses,
			)
			require.NoError(t, err)
			require.True(t, supported)
			require.Equal(t, []string{testCase.upstreamModel}, capability.UpstreamModels)
		})
	}
}

func TestAutoRoutingAcceptanceRestrictedAntigravityResponsesCannotUseUnlistedPublicGroup(t *testing.T) {
	fixture := newAutoRoutingAcceptancePlatformFixture(
		27, PlatformAntigravity, acceptanceAntigravityAccount(146, "gemini-4-argon", false, AccountTypeAPIKey),
	)
	fixture.userRepo.user.RestrictPublicGroups = true
	fixture.userRepo.user.AllowedGroups = []int64{101}

	_, err := fixture.routing.PrepareRequestKey(
		context.Background(),
		fixture.key,
		AutoRoutingRequest{
			Endpoint:  AutoModelEndpointResponses,
			ModelID:   "gemini-4-argon",
			RequestID: "acceptance:restricted-antigravity-responses",
			Body:      []byte(`{"model":"gemini-4-argon","input":"hello"}`),
		},
	)
	require.ErrorIs(t, err, ErrAutoRoutingCandidateUnavailable)
	require.Zero(t, fixture.repo.createCalls)
	require.Nil(t, fixture.repo.execution)
	require.Nil(t, fixture.key.GroupID)
	require.Nil(t, fixture.key.Group)
	require.Nil(t, fixture.key.AutoRouting)
}

func TestAutoRoutingAcceptanceDispatchRPMDenialsMapToLimitErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		result int
		want   error
	}{
		{name: "group", result: 1, want: ErrGroupRPMExceeded},
		{name: "user", result: 2, want: ErrUserRPMExceeded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rpmCache := &autoRoutingAcceptanceRPMCache{result: testCase.result}
			billing := &BillingCacheService{userRPMCache: rpmCache}
			routing := &AutoRoutingService{}
			routing.SetDispatchBilling(billing)
			group := &Group{ID: 202, RPMLimit: 5}
			key := &APIKey{
				User:        &User{ID: 42, RPMLimit: 10},
				Group:       group,
				GroupID:     int64Pointer(group.ID),
				AutoRouting: &AutoRoutingMetadata{ExecutionID: 501},
			}

			require.ErrorIs(t, routing.AdmitDispatch(context.Background(), key), testCase.want)
			require.Equal(t, 1, rpmCache.admissionCalls)
		})
	}
}

func TestAutoRoutingAcceptanceFixedKeyRPMRemainsInEligibility(t *testing.T) {
	rpmCache := &autoRoutingAcceptanceRPMCache{}
	billing := &BillingCacheService{
		cache:        &autoRoutingAcceptanceBillingCache{balance: 100},
		userRPMCache: rpmCache,
	}
	user := &User{ID: 42, RPMLimit: 10}
	group := &Group{ID: 202, RPMLimit: 10}
	groupID := group.ID
	key := &APIKey{ID: 88, User: user, Group: group, GroupID: &groupID}

	require.NoError(t, billing.CheckBillingEligibility(context.Background(), user, key, group, nil, ""))
	require.Zero(t, rpmCache.admissionCalls)
	require.Equal(t, 1, rpmCache.userGroupIncrCalls)
	require.Equal(t, 1, rpmCache.userIncrCalls)
}

func TestAutoRoutingAcceptanceRevokedCustomGroupIsNotRouted(t *testing.T) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.repo.config.SelectionMode = AutoRoutingSelectionCustom
	fixture.repo.config.GroupIDs = []int64{101}
	fixture.userRepo.user.RestrictPublicGroups = true
	fixture.userRepo.user.AllowedGroups = []int64{202}

	_, err := fixture.routing.PrepareRequestKey(
		context.Background(),
		fixture.key,
		AutoRoutingRequest{
			Endpoint:  AutoModelEndpointChat,
			ModelID:   "revoked-model",
			RequestID: "acceptance:revoked-1",
			Body:      []byte(`{"model":"revoked-model"}`),
		},
	)
	require.ErrorIs(t, err, ErrAutoRoutingCandidateUnavailable)
	require.Zero(t, fixture.repo.createCalls)
}

func TestAutoRoutingAcceptanceRejectsCorruptCustomEmptyWithoutInheritance(t *testing.T) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.repo.config.SelectionMode = AutoRoutingSelectionCustom
	fixture.repo.config.GroupIDs = nil

	_, err := fixture.routing.PrepareRequestKey(
		context.Background(),
		fixture.key,
		AutoRoutingRequest{
			Endpoint:  AutoModelEndpointChat,
			ModelID:   "routed-model",
			RequestID: "acceptance:empty-1",
			Body:      []byte(`{"model":"routed-model"}`),
		},
	)
	require.ErrorIs(t, err, ErrAutoRoutingUnavailable)
	require.Zero(t, fixture.repo.createCalls)
	require.Zero(t, fixture.repo.executionReads)
}

func TestAutoRoutingAcceptanceFixedKeyStateRetainsItsConfiguredGroup(t *testing.T) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.repo.config = nil
	fixedGroupID := int64(101)
	fixedKey := &APIKey{
		ID:              88,
		UserID:          fixture.user.ID,
		RoutingMode:     AutoRoutingModeFixed,
		GroupID:         &fixedGroupID,
		Group:           fixture.groups[fixedGroupID],
		User:            fixture.user,
		Status:          StatusAPIKeyActive,
		RoutingRevision: 0,
	}
	apiKeys := &APIKeyService{
		apiKeyRepo: &autoRoutingAcceptanceAPIKeyRepo{key: fixedKey},
	}
	routing := NewAutoRoutingService(apiKeys, fixture.groupRepo, fixture.repo, fixture.catalog)

	state, err := routing.GetKeyRoutingState(context.Background(), fixture.user.ID, fixedKey.ID)
	require.NoError(t, err)
	require.Equal(t, AutoRoutingModeFixed, state.Mode)
	require.Equal(t, int64(101), *state.FixedGroupID)
	require.Nil(t, state.Auto)
	require.True(t, reflect.DeepEqual(fixedKey.Group, fixture.groups[fixedGroupID]))
}

type autoRoutingAcceptanceFixture struct {
	user      *User
	key       *APIKey
	userRepo  *autoRoutingAcceptanceUserRepo
	groupRepo *autoRoutingAcceptanceGroupRepo
	rateRepo  *autoRoutingAcceptanceRateRepo
	repo      *autoRoutingAcceptanceRoutingRepo
	catalog   *AutoModelCatalog
	routing   *AutoRoutingService
	groups    map[int64]*Group
}

func newAutoRoutingAcceptanceFixture() autoRoutingAcceptanceFixture {
	user := &User{
		ID:                   42,
		Status:               StatusActive,
		Balance:              100,
		AllowedGroups:        []int64{101, 202},
		RestrictPublicGroups: true,
	}
	groups := map[int64]*Group{
		101: {
			ID:               101,
			Name:             "first",
			Platform:         PlatformOpenAI,
			Status:           StatusActive,
			SubscriptionType: SubscriptionTypeStandard,
			RateMultiplier:   1.2,
			IsExclusive:      true,
			Hydrated:         true,
		},
		202: {
			ID:               202,
			Name:             "second",
			Platform:         PlatformOpenAI,
			Status:           StatusActive,
			SubscriptionType: SubscriptionTypeStandard,
			RateMultiplier:   1.6,
			IsExclusive:      true,
			Hydrated:         true,
		},
	}
	config := &APIKeyAutoRoutingConfig{
		APIKeyID:      77,
		Mode:          AutoRoutingModeAuto,
		ProfileID:     9,
		SelectionMode: AutoRoutingSelectionInherit,
		Revision:      4,
	}
	repo := &autoRoutingAcceptanceRoutingRepo{
		config: config,
		profile: &AutoRoutingProfile{
			ID:               9,
			Name:             "Auto",
			Status:           AutoRoutingProfileStatusActive,
			Revision:         3,
			MaxCandidates:    5,
			AllowCustomOrder: true,
			Members: []AutoRoutingProfileMember{
				{GroupID: 101, SortOrder: 0},
				{GroupID: 202, SortOrder: 1},
			},
		},
	}
	userRepo := &autoRoutingAcceptanceUserRepo{user: user}
	groupRepo := &autoRoutingAcceptanceGroupRepo{groups: groups}
	rateRepo := &autoRoutingAcceptanceRateRepo{
		rates: map[int64]float64{},
		rpm:   map[int64]int{},
	}
	apiKeys := &APIKeyService{
		userRepo:          userRepo,
		groupRepo:         groupRepo,
		userSubRepo:       &autoRoutingAcceptanceSubscriptionRepo{},
		userGroupRateRepo: rateRepo,
	}
	key := &APIKey{
		ID:              77,
		UserID:          user.ID,
		RoutingMode:     AutoRoutingModeAuto,
		RoutingRevision: 4,
		Status:          StatusAPIKeyActive,
		User:            cloneAutoRoutingUser(user),
	}
	catalog := NewAutoModelCatalog(&autoRoutingAcceptanceAccountRepo{
		accounts: map[int64][]Account{
			101: {acceptanceOpenAIAccount(1, "revoked-model")},
			202: {acceptanceOpenAIAccount(2, "routed-model")},
		},
	})
	routing := NewAutoRoutingService(apiKeys, groupRepo, repo, catalog)

	return autoRoutingAcceptanceFixture{
		user: user, key: key, userRepo: userRepo, groupRepo: groupRepo,
		rateRepo: rateRepo, repo: repo, catalog: catalog, routing: routing, groups: groups,
	}
}

func newAutoRoutingAcceptancePlatformFixture(
	groupID int64,
	groupPlatform string,
	account Account,
) autoRoutingAcceptanceFixture {
	fixture := newAutoRoutingAcceptanceFixture()
	group := fixture.groups[202]
	group.ID = groupID
	group.Name = groupPlatform + "-public"
	group.Platform = groupPlatform
	group.IsExclusive = false
	fixture.groups = map[int64]*Group{groupID: group}
	fixture.groupRepo.groups = fixture.groups
	fixture.repo.config.APIKeyID = 411
	fixture.repo.config.ProfileID = 2
	fixture.repo.profile.ID = 2
	fixture.repo.profile.Members = []AutoRoutingProfileMember{{GroupID: groupID}}
	fixture.userRepo.user.ID = 70
	fixture.userRepo.user.Role = RoleUser
	fixture.userRepo.user.AllowedGroups = nil
	fixture.userRepo.user.RestrictPublicGroups = false
	fixture.key.ID = 411
	fixture.key.UserID = 70
	fixture.key.User = cloneAutoRoutingUser(fixture.userRepo.user)
	fixture.rateRepo.rates = map[int64]float64{}
	fixture.rateRepo.rpm = map[int64]int{}
	fixture.catalog.accountRepo = &autoRoutingAcceptanceAccountRepo{
		accounts: map[int64][]Account{
			groupID: {account},
		},
	}
	return fixture
}

func acceptanceAntigravityAccount(id int64, model string, mixedScheduling bool, accountType string) Account {
	upstreamModel := model
	if model == "gemini-4-argon" {
		upstreamModel = "gemini-4-argon-high"
	}
	return Account{
		ID:          id,
		Platform:    PlatformAntigravity,
		Type:        accountType,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{"mixed_scheduling": mixedScheduling},
		Credentials: map[string]any{
			"model_mapping": map[string]any{model: upstreamModel},
		},
	}
}

func acceptanceOpenAIAccount(id int64, model string) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{model: "upstream-" + model}},
	}
}
