//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type playgroundAPIKeyRepo struct {
	APIKeyRepository
	keys    []APIKey
	created *APIKey
}

func (r *playgroundAPIKeyRepo) ListByUserID(_ context.Context, _ int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	return filterAPIKeyStubKeys(7, r.keys, filters), &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize}, nil
}

func (r *playgroundAPIKeyRepo) Create(_ context.Context, key *APIKey) error {
	clone := *key
	clone.ID = 99
	key.ID = clone.ID
	r.created = &clone
	return nil
}

func (r *playgroundAPIKeyRepo) ExistsByKey(context.Context, string) (bool, error) {
	return false, nil
}

type playgroundGroupRepo struct {
	GroupRepository
	group Group
}

func (r *playgroundGroupRepo) ListActive(context.Context) ([]Group, error) {
	return []Group{r.group}, nil
}

func (r *playgroundGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if id != r.group.ID {
		return nil, ErrGroupNotFound
	}
	group := r.group
	return &group, nil
}

func newPlaygroundKeyService(repo *playgroundAPIKeyRepo) *APIKeyService {
	return &APIKeyService{
		apiKeyRepo: repo,
		userRepo: &visibilityUserRepo{user: &User{
			ID:            7,
			AllowedGroups: nil,
		}},
		groupRepo:   &playgroundGroupRepo{group: Group{ID: 4, Status: StatusActive}},
		userSubRepo: &visibilitySubRepo{},
		cfg:         &config.Config{},
	}
}

func TestResolvePlaygroundKeyReusesDedicatedActiveKey(t *testing.T) {
	groupID := int64(4)
	repo := &playgroundAPIKeyRepo{keys: []APIKey{
		{ID: 1, UserID: 7, GroupID: &groupID, Name: "Regular", Status: StatusActive, Key: "regular-key-value"},
		{ID: 2, UserID: 7, GroupID: &groupID, Name: "Playground", Status: StatusActive, Key: "playground-key-value"},
	}}

	key, err := newPlaygroundKeyService(repo).ResolvePlaygroundKey(context.Background(), 7, groupID)

	require.NoError(t, err)
	require.Equal(t, int64(2), key.ID)
	require.Nil(t, repo.created)
}

func TestResolvePlaygroundKeyCreatesUnlimitedDedicatedKey(t *testing.T) {
	groupID := int64(4)
	repo := &playgroundAPIKeyRepo{}

	key, err := newPlaygroundKeyService(repo).ResolvePlaygroundKey(context.Background(), 7, groupID)

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.Equal(t, "Playground", key.Name)
	require.Equal(t, groupID, *key.GroupID)
	require.Zero(t, key.Quota)
}

func TestResolvePlaygroundKeyRejectsUnavailableGroup(t *testing.T) {
	repo := &playgroundAPIKeyRepo{}

	_, err := newPlaygroundKeyService(repo).ResolvePlaygroundKey(context.Background(), 7, 999)

	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.Nil(t, repo.created)
}
