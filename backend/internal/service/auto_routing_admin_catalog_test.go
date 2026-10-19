package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type autoAdminCatalogGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
	calls  int
}

func (r *autoAdminCatalogGroupRepo) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	r.calls++
	if group := r.groups[id]; group != nil {
		return group, nil
	}
	return nil, ErrGroupNotFound
}

type autoAdminCatalogAccountRepo struct {
	AccountRepository
	accounts map[int64][]Account
	err      error
}

func (r *autoAdminCatalogAccountRepo) ListModelAvailabilityCandidates(
	_ context.Context, groupID *int64, _ []string, _ bool,
) ([]Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.accounts[*groupID], nil
}

func TestAutoModelCatalogAdminPreviewUsesSelectedGroupsAndFreshConfiguration(t *testing.T) {
	groups := &autoAdminCatalogGroupRepo{groups: map[int64]*Group{
		11: {ID: 11, Platform: PlatformOpenAI, Status: StatusActive,
			ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"local-only", "shared"}}},
		12: {ID: 12, Platform: PlatformAnthropic, Status: StatusActive},
		13: {ID: 13, Platform: PlatformOpenAI, Status: StatusDisabled},
	}}
	accounts := &autoAdminCatalogAccountRepo{accounts: map[int64][]Account{
		11: {{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{
				"local-only": "gpt-6-luna", "shared": "gpt-6-luna", "hidden": "gpt-6-luna",
			}}}},
		12: {{ID: 102, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive,
			Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{
				"claude-local": "claude-opus-5-5", "shared": "claude-opus-5-5",
			}}}},
	}}
	routing := NewAutoRoutingService(nil, groups, nil, NewAutoModelCatalog(accounts))
	result, err := routing.PreviewModelCatalog(context.Background(), []int64{11, 12, 13})
	require.NoError(t, err)
	modelIDs := make([]string, 0, len(result))
	for _, capability := range result {
		modelIDs = append(modelIDs, capability.ModelID)
	}
	require.Equal(t, []string{"local-only", "shared", "claude-local"}, modelIDs)
	require.ElementsMatch(t, []string{PlatformOpenAI, PlatformAnthropic}, result[1].SourcePlatforms)
	require.ElementsMatch(t, []string{"gpt-6-luna", "claude-opus-5-5"}, result[1].UpstreamModels)
	require.Equal(t, 3, groups.calls)

	accounts.accounts[12][0].Credentials["model_mapping"] = map[string]any{"updated-local": "claude-opus-5-5"}
	result, err = routing.PreviewModelCatalog(context.Background(), []int64{12})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "updated-local", result[0].ModelID)
}

func TestAutoModelCatalogAdminPreviewRejectsInvalidSelections(t *testing.T) {
	groups := &autoAdminCatalogGroupRepo{groups: map[int64]*Group{
		1: {ID: 1, Platform: PlatformComposite, Status: StatusActive},
	}}
	accounts := &autoAdminCatalogAccountRepo{}
	routing := NewAutoRoutingService(nil, groups, nil, NewAutoModelCatalog(accounts))
	for _, groupIDs := range [][]int64{{0}, {-1}, {2, 2}, make([]int64, maxAutoRoutingProfileCandidates+1)} {
		_, err := routing.PreviewModelCatalog(context.Background(), groupIDs)
		require.ErrorIs(t, err, ErrAutoRoutingProfileInvalid)
	}
	require.Zero(t, groups.calls)
	_, err := routing.PreviewModelCatalog(context.Background(), []int64{1})
	require.ErrorIs(t, err, ErrAutoRoutingProfileInvalid)
	_, err = routing.PreviewModelCatalog(context.Background(), []int64{99})
	require.ErrorIs(t, err, ErrGroupNotFound)
	empty, err := routing.PreviewModelCatalog(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestAutoModelCatalogAdminPreviewFailsClosedOnLookupError(t *testing.T) {
	groups := &autoAdminCatalogGroupRepo{groups: map[int64]*Group{
		11: {ID: 11, Platform: PlatformOpenAI, Status: StatusActive},
	}}
	accounts := &autoAdminCatalogAccountRepo{err: errors.New("catalog lookup unavailable")}
	routing := NewAutoRoutingService(nil, groups, nil, NewAutoModelCatalog(accounts))
	result, err := routing.PreviewModelCatalog(context.Background(), []int64{11})
	require.Error(t, err)
	require.Nil(t, result)
	var unavailable *AutoRoutingService
	_, err = unavailable.PreviewModelCatalog(context.Background(), []int64{11})
	require.ErrorIs(t, err, ErrAutoRoutingUnavailable)
}
