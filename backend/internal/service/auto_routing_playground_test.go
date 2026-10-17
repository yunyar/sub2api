package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type playgroundAutoRoutingRepoStub struct {
	AutoRoutingRepository
	profile AutoRoutingProfile
	binding *PlaygroundAutoKeyBinding
}

func (r *playgroundAutoRoutingRepoStub) GetAutoRoutingProfile(context.Context, int64) (*AutoRoutingProfile, error) {
	profile := cloneAutoRoutingProfile(&r.profile)
	return &profile, nil
}

func (r *playgroundAutoRoutingRepoStub) ListAutoRoutingProfiles(context.Context) ([]AutoRoutingProfile, error) {
	profile := cloneAutoRoutingProfile(&r.profile)
	return []AutoRoutingProfile{profile}, nil
}

func (r *playgroundAutoRoutingRepoStub) GetPlaygroundAutoKeyBinding(context.Context, int64, int64) (*PlaygroundAutoKeyBinding, error) {
	if r.binding == nil {
		return nil, nil
	}
	binding := *r.binding
	return &binding, nil
}

type playgroundAutoRoutingUserRepoStub struct {
	UserRepository
	user *User
}

func (r *playgroundAutoRoutingUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	user := *r.user
	return &user, nil
}

type playgroundAutoRoutingSubRepoStub struct {
	UserSubscriptionRepository
}

func (playgroundAutoRoutingSubRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

type playgroundAutoRoutingGroupRepoStub struct {
	GroupRepository
	groups []Group
}

func (r *playgroundAutoRoutingGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return append([]Group(nil), r.groups...), nil
}

func newPlaygroundAutoRoutingService(
	group Group,
	user *User,
) (*AutoRoutingService, *playgroundAutoRoutingRepoStub) {
	repo := &playgroundAutoRoutingRepoStub{
		profile: AutoRoutingProfile{
			ID:                     5,
			Name:                   "Auto",
			Status:                 AutoRoutingProfileStatusActive,
			Revision:               1,
			MaxCandidates:          5,
			CrossGroupRetryAllowed: true,
			Members:                []AutoRoutingProfileMember{{GroupID: group.ID, SortOrder: 0}},
		},
	}
	groups := &playgroundAutoRoutingGroupRepoStub{groups: []Group{group}}
	apiKeys := &APIKeyService{
		userRepo:    &playgroundAutoRoutingUserRepoStub{user: user},
		groupRepo:   groups,
		userSubRepo: playgroundAutoRoutingSubRepoStub{},
	}
	return &AutoRoutingService{apiKeys: apiKeys, groupRepo: groups, repo: repo}, repo
}

func TestGetPlaygroundAutoRoutingDefaultsToInheritWithoutCreatingKey(t *testing.T) {
	group := Group{ID: 8, Status: StatusActive, Platform: PlatformOpenAI}
	service, repo := newPlaygroundAutoRoutingService(group, &User{ID: 12})

	config, err := service.GetPlaygroundAutoRouting(context.Background(), 12, 5)

	require.NoError(t, err)
	require.Equal(t, AutoRoutingModeAuto, config.Mode)
	require.Equal(t, int64(5), config.ProfileID)
	require.Equal(t, AutoRoutingSelectionInherit, config.SelectionMode)
	require.Empty(t, config.GroupIDs)
	require.Zero(t, config.Revision)
	require.Nil(t, repo.binding)
}

func TestGetPlaygroundAutoRoutingHidesUnavailableProfile(t *testing.T) {
	group := Group{ID: 8, Status: StatusActive, Platform: PlatformOpenAI, IsExclusive: true}
	service, _ := newPlaygroundAutoRoutingService(group, &User{ID: 12})

	_, err := service.GetPlaygroundAutoRouting(context.Background(), 12, 5)

	require.ErrorIs(t, err, ErrAutoRoutingProfileNotFound)
}

func TestValidatePlaygroundAutoRoutingAllowsCrossGroupRetry(t *testing.T) {
	group := Group{ID: 8, Status: StatusActive, Platform: PlatformOpenAI}
	service, repo := newPlaygroundAutoRoutingService(group, &User{ID: 12})

	err := service.validateKeyRouting(
		context.Background(),
		12,
		&APIKeyAutoRoutingConfig{
			ProfileID:       5,
			SelectionMode:   AutoRoutingSelectionInherit,
			CrossGroupRetry: true,
		},
	)

	require.NoError(t, err)
	require.Nil(t, repo.binding)
}
