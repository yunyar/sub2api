package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAutoModelCatalogFilterUsesGroupAllowlistSemantics(t *testing.T) {
	filter, err := normalizeGroupModelAllowlist(GroupModelAllowlist{
		Enabled: true,
		Models:  []string{" gpt-5.5-* ", "GPT-5.5-*", "claude-sonnet-4.5", " "},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5-*", "claude-sonnet-4.5"}, filter.Models)

	_, err = normalizeGroupModelAllowlist(GroupModelAllowlist{Enabled: true, Models: []string{" ", ""}})
	require.Error(t, err)
}

func TestFilterAutoModelCapabilitiesOnlyReturnsCatalogModels(t *testing.T) {
	capabilities := []AutoModelCapability{
		{ModelID: "gpt-5.5"},
		{ModelID: "gpt-5.5-codex"},
		{ModelID: "claude-sonnet-4.5"},
	}

	disabled := filterAutoModelCapabilities(capabilities, &GroupModelAllowlist{Models: []string{"claude-*"}})
	require.Equal(t, capabilities, disabled)

	filtered := filterAutoModelCapabilities(capabilities, &GroupModelAllowlist{
		Enabled: true,
		Models:  []string{"gpt-5.5-*", "missing-model", "CLAUDE-SONNET-4.5"},
	})
	require.Equal(t, []AutoModelCapability{
		{ModelID: "gpt-5.5-codex"},
		{ModelID: "claude-sonnet-4.5"},
	}, filtered)

	empty := filterAutoModelCapabilities(capabilities, &GroupModelAllowlist{Enabled: true})
	require.Empty(t, empty)
}

func TestFilterAutoModelCapabilitiesPreservesCatalogIDsAndRuleOrder(t *testing.T) {
	capabilities := []AutoModelCapability{
		{ModelID: "gpt-5.5", Platform: PlatformOpenAI},
		{ModelID: "claude-sonnet-4-5", Platform: PlatformAnthropic},
		{ModelID: "gemini-3.1-pro", Platform: PlatformAntigravity},
	}
	filtered := filterAutoModelCapabilities(capabilities, &GroupModelAllowlist{
		Enabled: true,
		Models: []string{
			"claude-sonnet-4-5-thinking",
			"GEMINI-*",
			"gpt-5.5",
			"claude-*",
			"*",
		},
	})
	require.Equal(t, []AutoModelCapability{capabilities[2], capabilities[0], capabilities[1]}, filtered)
	require.Empty(t, filterAutoModelCapabilities(capabilities, &GroupModelAllowlist{
		Enabled: true, Models: []string{"claude-sonnet-4-5-thinking"},
	}))
}

type autoRoutingModelCatalogFilterRepoStub struct {
	AutoRoutingRepository
	profile *AutoRoutingProfile
	updated *AutoRoutingProfile
}

func (repo *autoRoutingModelCatalogFilterRepoStub) GetAutoRoutingProfile(context.Context, int64) (*AutoRoutingProfile, error) {
	profile := cloneAutoRoutingProfile(repo.profile)
	return &profile, nil
}

func (repo *autoRoutingModelCatalogFilterRepoStub) UpdateAutoRoutingProfile(
	_ context.Context,
	profile *AutoRoutingProfile,
	expectedRevision int64,
) error {
	if expectedRevision != repo.profile.Revision {
		return ErrAutoRoutingRevisionConflict
	}
	updated := cloneAutoRoutingProfile(profile)
	updated.Revision = expectedRevision + 1
	repo.profile = &updated
	repo.updated = &updated
	profile.Revision = updated.Revision
	return nil
}

type autoRoutingModelCatalogFilterGroupRepoStub struct {
	GroupRepository
}

func (autoRoutingModelCatalogFilterGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	return &Group{
		ID: id, Platform: PlatformOpenAI, Status: StatusActive,
		SubscriptionType: SubscriptionTypeStandard, Hydrated: true,
	}, nil
}

func TestUpdateAutoRoutingProfilePreservesFilterWhenOmitted(t *testing.T) {
	storedProfile := &AutoRoutingProfile{
		ID: 12, Name: "Auto", Status: AutoRoutingProfileStatusActive, Revision: 4,
		MaxCandidates: 5, AllowCustomOrder: true,
		Members: []AutoRoutingProfileMember{{GroupID: 8}},
		ModelCatalogFilter: &GroupModelAllowlist{
			Enabled: true, Models: []string{"gpt-5.5-*"},
		},
	}
	repo := &autoRoutingModelCatalogFilterRepoStub{profile: storedProfile}
	routing := &AutoRoutingService{
		repo:      repo,
		groupRepo: autoRoutingModelCatalogFilterGroupRepoStub{},
	}

	updated, err := routing.UpdateProfile(context.Background(), &AutoRoutingProfile{
		ID: 12, Name: "Auto", Status: AutoRoutingProfileStatusActive,
		MaxCandidates: 5, AllowCustomOrder: true,
		Members: []AutoRoutingProfileMember{{GroupID: 8}},
	}, 4)

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5-*"}, updated.ModelCatalogFilter.Models)
	require.Equal(t, int64(5), updated.Revision)
	require.Equal(t, []string{"gpt-5.5-*"}, repo.updated.ModelCatalogFilter.Models)

	disabled, err := routing.UpdateProfile(context.Background(), &AutoRoutingProfile{
		ID: 12, Name: "Auto", Status: AutoRoutingProfileStatusActive,
		MaxCandidates: 5, AllowCustomOrder: true,
		Members: []AutoRoutingProfileMember{{GroupID: 8}},
		ModelCatalogFilter: &GroupModelAllowlist{
			Enabled: false, Models: []string{},
		},
	}, 5)
	require.NoError(t, err)
	require.False(t, disabled.ModelCatalogFilter.Enabled)
	require.Empty(t, disabled.ModelCatalogFilter.Models)
}

func TestPrepareRequestKeyFiltersDirectoryButNotGeneration(t *testing.T) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.repo.profile.ModelCatalogFilter = &GroupModelAllowlist{
		Enabled: true,
		Models:  []string{"routed-model", "not-real-model"},
	}

	directory, err := fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
		IsModelDirectory: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"routed-model"}, autoModelCapabilityIDs(directory.Capabilities))

	singleModel, err := fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
		IsModelDirectory: true,
		ModelID:          "revoked-model",
	})
	require.NoError(t, err)
	require.Empty(t, singleModel.Capabilities)

	_, err = fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
		Endpoint:  AutoModelEndpointChat,
		ModelID:   "revoked-model",
		RequestID: "model-catalog-filter:direct-request",
		Body:      []byte(`{"model":"revoked-model"}`),
	})
	require.NoError(t, err)
}

func TestAutoRoutingProfileFilterJSONUsesStableAPIShape(t *testing.T) {
	filter := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5"}}
	content, err := json.Marshal(filter)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":true,"models":["gpt-5.5"]}`, string(content))
}

func autoModelCapabilityIDs(capabilities []AutoModelCapability) []string {
	modelIDs := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		modelIDs = append(modelIDs, capability.ModelID)
	}
	return modelIDs
}
