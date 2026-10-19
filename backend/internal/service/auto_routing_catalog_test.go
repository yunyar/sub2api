package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type autoModelCatalogAccountRepoStub struct {
	AccountRepository
	accounts       []Account
	err            error
	groupID        int64
	platforms      []string
	includeGrouped bool
	calls          int
}

func (r *autoModelCatalogAccountRepoStub) ListModelAvailabilityCandidates(
	_ context.Context,
	groupID *int64,
	platforms []string,
	includeGrouped bool,
) ([]Account, error) {
	r.calls++
	if groupID != nil {
		r.groupID = *groupID
	}
	r.platforms = append([]string(nil), platforms...)
	r.includeGrouped = includeGrouped
	if r.err != nil {
		return nil, r.err
	}
	return append([]Account(nil), r.accounts...), nil
}

func TestAutoModelCatalogListsMappedModelsAndIgnoresTransientAccountState(t *testing.T) {
	future := time.Now().Add(time.Hour)
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{
			{
				ID:               1,
				Platform:         PlatformOpenAI,
				Type:             AccountTypeAPIKey,
				Status:           StatusActive,
				Schedulable:      true,
				Credentials:      map[string]any{"model_mapping": map[string]any{"shared-model": "upstream-z", "private-*": "wildcard-target"}},
				RateLimitResetAt: &future,
				OverloadUntil:    &future,
			},
			{
				ID:                     2,
				Platform:               PlatformOpenAI,
				Type:                   AccountTypeAPIKey,
				Status:                 StatusActive,
				Schedulable:            true,
				Credentials:            map[string]any{"model_mapping": map[string]any{"shared-model": "upstream-a"}},
				TempUnschedulableUntil: &future,
			},
		},
	}
	group := &Group{
		ID:       19,
		Platform: PlatformOpenAI,
		Status:   StatusActive,
		ModelAllowlist: GroupModelAllowlist{
			Enabled: true,
			Models:  []string{"shared-model"},
		},
	}

	catalog := NewAutoModelCatalog(repo)
	got, err := catalog.ListGroupModelCapabilities(context.Background(), group)
	if err != nil {
		t.Fatalf("ListGroupModelCapabilities() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListGroupModelCapabilities() returned %d models, want one: %#v", len(got), got)
	}
	capability := got[0]
	if capability.ModelID != "shared-model" ||
		capability.Platform != PlatformOpenAI ||
		!capability.AccountSchedulable {
		t.Fatalf("unexpected capability: %#v", capability)
	}
	if !reflect.DeepEqual(capability.SourcePlatforms, []string{PlatformOpenAI}) {
		t.Fatalf("SourcePlatforms = %#v", capability.SourcePlatforms)
	}
	if !reflect.DeepEqual(capability.UpstreamModels, []string{"upstream-a", "upstream-z"}) {
		t.Fatalf("UpstreamModels = %#v", capability.UpstreamModels)
	}
	if !reflect.DeepEqual(capability.SupportedEndpoints, []AutoModelEndpoint{
		AutoModelEndpointChat,
		AutoModelEndpointResponses,
	}) {
		t.Fatalf("SupportedEndpoints = %#v", capability.SupportedEndpoints)
	}
	if repo.groupID != group.ID ||
		!reflect.DeepEqual(repo.platforms, []string{PlatformOpenAI}) ||
		repo.includeGrouped {
		t.Fatalf("unexpected account lookup: group=%d platforms=%v includeGrouped=%v", repo.groupID, repo.platforms, repo.includeGrouped)
	}
}

func TestAutoModelCatalogDoesNotInventModelsForUnmappedAPIKeys(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		mapping  map[string]any
		want     []string
	}{
		{name: "OpenAI API key without mapping", platform: PlatformOpenAI},
		{name: "OpenAI wildcard-only mapping", platform: PlatformOpenAI, mapping: map[string]any{"gpt-*": "upstream-*"}},
		{name: "Grok API key without mapping", platform: PlatformGrok},
		{name: "multi-protocol API key without mapping", platform: PlatformDeepseek},
		{
			name:     "explicit aliases only",
			platform: PlatformOpenAI,
			mapping: map[string]any{
				"public-alias": "upstream-model",
				"another-*":    "wildcard-target",
				"empty-target": " ",
			},
			want: []string{"public-alias"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			group := &Group{ID: 18, Platform: test.platform, Status: StatusActive}
			accounts := []Account{{
				ID: 18, Platform: test.platform, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"model_mapping": test.mapping},
			}}
			repo := &autoModelCatalogAccountRepoStub{accounts: accounts}
			got, err := NewAutoModelCatalog(repo).ListGroupModelCapabilities(context.Background(), group)
			require.NoError(t, err)
			if len(test.want) == 0 {
				require.Empty(t, got)
				return
			}
			require.Equal(t, test.want, autoModelCapabilityIDs(got))
		})
	}
}

func TestAutoModelCatalogKeepsNativeOAuthAndAntigravityMappings(t *testing.T) {
	t.Run("OpenAI OAuth native defaults", func(t *testing.T) {
		group := &Group{ID: 19, Platform: PlatformOpenAI, Status: StatusActive}
		repo := &autoModelCatalogAccountRepoStub{accounts: []Account{{
			ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true,
		}}}
		got, err := NewAutoModelCatalog(repo).ListGroupModelCapabilities(context.Background(), group)
		require.NoError(t, err)
		require.Contains(t, autoModelCapabilityIDs(got), "gpt-5.4")
	})

	t.Run("Antigravity OAuth native mapping", func(t *testing.T) {
		group := &Group{ID: 20, Platform: PlatformAntigravity, Status: StatusActive}
		repo := &autoModelCatalogAccountRepoStub{accounts: []Account{{
			ID: 20, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true,
		}}}
		got, err := NewAutoModelCatalog(repo).ListGroupModelCapabilities(context.Background(), group)
		require.NoError(t, err)
		require.Contains(t, autoModelCapabilityIDs(got), "gemini-4-argon")
	})

	t.Run("Antigravity API key requires an explicit mapping", func(t *testing.T) {
		group := &Group{ID: 21, Platform: PlatformAntigravity, Status: StatusActive}
		repo := &autoModelCatalogAccountRepoStub{accounts: []Account{{
			ID: 21, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true,
		}}}
		got, err := NewAutoModelCatalog(repo).ListGroupModelCapabilities(context.Background(), group)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestBuildAutoCodexModelsManifestUsesOnlyResponsesCapabilities(t *testing.T) {
	body, err := BuildAutoCodexModelsManifest([]AutoModelCapability{
		{ModelID: "responses-model", SupportedEndpoints: []AutoModelEndpoint{AutoModelEndpointResponses}},
		{ModelID: "chat-only-model", SupportedEndpoints: []AutoModelEndpoint{AutoModelEndpointChat}},
		{ModelID: "gpt-image-2", SupportedEndpoints: []AutoModelEndpoint{AutoModelEndpointResponses}},
		{ModelID: "wildcard-*", SupportedEndpoints: []AutoModelEndpoint{AutoModelEndpointResponses}},
	})
	require.NoError(t, err)
	require.Contains(t, string(body), `"slug":"responses-model"`)
	require.NotContains(t, string(body), "chat-only-model")
	require.NotContains(t, string(body), "gpt-image-2")
	require.NotContains(t, string(body), "wildcard-*")
}

func TestAutoModelCatalogResolveSeparatesSupportFromCurrentSchedulability(t *testing.T) {
	future := time.Now().Add(time.Hour)
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:               8,
			Platform:         PlatformOpenAI,
			Type:             AccountTypeAPIKey,
			Status:           StatusActive,
			Schedulable:      true,
			RateLimitResetAt: &future,
			Credentials:      map[string]any{"model_mapping": map[string]any{"limited-model": "upstream-model"}},
		}},
	}
	group := &Group{ID: 26, Platform: PlatformOpenAI, Status: StatusActive}
	catalog := NewAutoModelCatalog(repo)

	directory, err := catalog.ListGroupModelCapabilities(context.Background(), group)
	if err != nil {
		t.Fatalf("ListGroupModelCapabilities() error = %v", err)
	}
	if len(directory) != 1 || directory[0].ModelID != "limited-model" || !directory[0].AccountSchedulable {
		t.Fatalf("directory should remain stable under a transient rate limit: %#v", directory)
	}

	capability, ok, err := catalog.ResolveGroupModelCapability(
		context.Background(), group, "limited-model", AutoModelEndpointChat,
	)
	if err != nil || !ok {
		t.Fatalf("ResolveGroupModelCapability() = (%#v, %v, %v), want supported capability", capability, ok, err)
	}
	if capability.AccountSchedulable {
		t.Fatalf("AccountSchedulable = true with no currently schedulable supporting account: %#v", capability)
	}
}

func TestAutoModelCatalogResolvesOpenAIMessagesDispatchBeforeAccountMapping(t *testing.T) {
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          3,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"dispatch-model": "wire-model"}},
		}},
	}
	group := &Group{
		ID:                    20,
		Platform:              PlatformOpenAI,
		Status:                StatusActive,
		AllowMessagesDispatch: true,
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			ExactModelMappings: map[string]string{"claude-sonnet-4-5": "dispatch-model"},
		},
	}

	capability, ok, err := NewAutoModelCatalog(repo).ResolveGroupModelCapability(
		context.Background(),
		group,
		"claude-sonnet-4-5",
		AutoModelEndpointMessages,
	)
	if err != nil || !ok {
		t.Fatalf("ResolveGroupModelCapability() = (%#v, %v, %v), want supported", capability, ok, err)
	}
	if !reflect.DeepEqual(capability.SupportedEndpoints, []AutoModelEndpoint{AutoModelEndpointMessages}) {
		t.Fatalf("SupportedEndpoints = %#v", capability.SupportedEndpoints)
	}
	if !reflect.DeepEqual(capability.UpstreamModels, []string{"wire-model"}) {
		t.Fatalf("UpstreamModels = %#v", capability.UpstreamModels)
	}

	group.AllowMessagesDispatch = false
	_, ok, err = NewAutoModelCatalog(repo).ResolveGroupModelCapability(
		context.Background(),
		group,
		"claude-sonnet-4-5",
		AutoModelEndpointMessages,
	)
	if err != nil || ok {
		t.Fatalf("Messages dispatch disabled: got supported=%v err=%v, want unsupported without error", ok, err)
	}
}

func TestAutoModelCatalogGeminiAndAntigravityResponsesUseMappedAccounts(t *testing.T) {
	tests := []struct {
		name                   string
		groupPlatform          string
		accountPlatform        string
		accountType            string
		model                  string
		upstreamModel          string
		mapping                map[string]any
		extra                  map[string]any
		wantChatSupported      bool
		wantResponsesSupported bool
	}{
		{
			name:              "native Gemini API key",
			groupPlatform:     PlatformGemini,
			accountPlatform:   PlatformGemini,
			accountType:       AccountTypeAPIKey,
			model:             "gemini-3-flash",
			upstreamModel:     "gemini-3-flash-preview",
			mapping:           map[string]any{"gemini-3-flash": "gemini-3-flash-preview"},
			wantChatSupported: true,
		},
		{
			name:              "native Gemini OAuth",
			groupPlatform:     PlatformGemini,
			accountPlatform:   PlatformGemini,
			accountType:       AccountTypeOAuth,
			model:             "gemini-3-flash",
			upstreamModel:     "gemini-3-flash-preview",
			mapping:           map[string]any{"gemini-3-flash": "gemini-3-flash-preview"},
			wantChatSupported: true,
		},
		{
			name:                   "Antigravity API key",
			groupPlatform:          PlatformAntigravity,
			accountPlatform:        PlatformAntigravity,
			accountType:            AccountTypeAPIKey,
			model:                  "gemini-4-argon",
			upstreamModel:          "gemini-4-argon-high",
			mapping:                map[string]any{"gemini-4-argon": "gemini-4-argon-high"},
			wantChatSupported:      true,
			wantResponsesSupported: true,
		},
		{
			name:                   "Antigravity OAuth",
			groupPlatform:          PlatformAntigravity,
			accountPlatform:        PlatformAntigravity,
			accountType:            AccountTypeOAuth,
			model:                  "gemini-4-argon",
			upstreamModel:          "gemini-4-argon-high",
			mapping:                map[string]any{"gemini-4-argon": "gemini-4-argon-high"},
			wantChatSupported:      true,
			wantResponsesSupported: true,
		},
		{
			name:                   "GPT OSS model in Antigravity group",
			groupPlatform:          PlatformAntigravity,
			accountPlatform:        PlatformAntigravity,
			accountType:            AccountTypeAPIKey,
			model:                  "gpt-oss-120b-medium",
			upstreamModel:          "gpt-oss-120b-medium",
			mapping:                map[string]any{"gpt-oss-120b-medium": "gpt-oss-120b-medium"},
			wantChatSupported:      true,
			wantResponsesSupported: true,
		},
		{
			name:              "mixed Antigravity API key in Gemini group",
			groupPlatform:     PlatformGemini,
			accountPlatform:   PlatformAntigravity,
			accountType:       AccountTypeAPIKey,
			model:             "gemini-3.8-flash",
			upstreamModel:     "gemini-3.8-flash-high",
			mapping:           map[string]any{"gemini-3.8-flash": "gemini-3.8-flash-high"},
			extra:             map[string]any{"mixed_scheduling": true},
			wantChatSupported: true,
		},
		{
			name:            "Antigravity API key without mixed scheduling",
			groupPlatform:   PlatformGemini,
			accountPlatform: PlatformAntigravity,
			accountType:     AccountTypeAPIKey,
			model:           "gemini-3.8-flash",
			upstreamModel:   "gemini-3.8-flash-high",
			mapping:         map[string]any{"gemini-3.8-flash": "gemini-3.8-flash-high"},
		},
		{
			name:            "unmapped Antigravity model",
			groupPlatform:   PlatformAntigravity,
			accountPlatform: PlatformAntigravity,
			accountType:     AccountTypeAPIKey,
			model:           "gemini-4-argon",
			mapping:         map[string]any{"another-model": "gemini-4-argon-high"},
		},
	}

	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &autoModelCatalogAccountRepoStub{
				accounts: []Account{{
					ID:          int64(40 + index),
					Platform:    testCase.accountPlatform,
					Type:        testCase.accountType,
					Status:      StatusActive,
					Schedulable: true,
					Extra:       testCase.extra,
					Credentials: map[string]any{
						"model_mapping": testCase.mapping,
					},
				}},
			}
			group := &Group{
				ID:       int64(40 + index),
				Platform: testCase.groupPlatform,
				Status:   StatusActive,
				ModelAllowlist: GroupModelAllowlist{
					Enabled: true,
					Models:  []string{testCase.model},
				},
			}
			catalog := NewAutoModelCatalog(repo)

			for _, endpointCase := range []struct {
				endpoint      AutoModelEndpoint
				wantSupported bool
			}{
				{AutoModelEndpointChat, testCase.wantChatSupported},
				{AutoModelEndpointResponses, testCase.wantResponsesSupported},
			} {
				endpoint := endpointCase.endpoint
				capability, ok, err := catalog.ResolveGroupModelCapability(
					context.Background(), group, testCase.model, endpoint,
				)
				if err != nil || ok != endpointCase.wantSupported {
					t.Fatalf("%s: ResolveGroupModelCapability() = (%#v, %v, %v), want supported=%v",
						endpoint, capability, ok, err, endpointCase.wantSupported)
				}
				if !endpointCase.wantSupported {
					continue
				}
				if !reflect.DeepEqual(capability.UpstreamModels, []string{testCase.upstreamModel}) {
					t.Fatalf("%s: UpstreamModels = %#v, want mapped model %q",
						endpoint, capability.UpstreamModels, testCase.upstreamModel)
				}
			}

			if !testCase.wantChatSupported && !testCase.wantResponsesSupported {
				return
			}

			listed, err := catalog.ListGroupModelCapabilities(context.Background(), group)
			if err != nil {
				t.Fatalf("ListGroupModelCapabilities() error = %v", err)
			}
			if len(listed) != 1 || listed[0].ModelID != testCase.model {
				t.Fatalf("ListGroupModelCapabilities() = %#v, want only %q", listed, testCase.model)
			}
			hasResponses := false
			for _, endpoint := range listed[0].SupportedEndpoints {
				if endpoint == AutoModelEndpointResponses {
					hasResponses = true
					break
				}
			}
			if hasResponses != testCase.wantResponsesSupported {
				t.Fatalf("listed SupportedEndpoints = %#v, want Responses=%v",
					listed[0].SupportedEndpoints, testCase.wantResponsesSupported)
			}

			group.ClaudeCodeOnly = true
			if _, ok, err := catalog.ResolveGroupModelCapability(
				context.Background(), group, testCase.model, AutoModelEndpointResponses,
			); err != nil || ok {
				t.Fatalf("ClaudeCodeOnly Responses: supported=%v err=%v, want unsupported", ok, err)
			}
			group.ClaudeCodeOnly = false

			group.ModelAllowlist.Models = []string{"another-model"}
			if _, ok, err := catalog.ResolveGroupModelCapability(
				context.Background(), group, testCase.model, AutoModelEndpointResponses,
			); err != nil || ok {
				t.Fatalf("disallowed model Responses: supported=%v err=%v, want unsupported", ok, err)
			}
		})
	}
}

func TestAutoModelCatalogResponsesKeepsOpenAIEndpointCapabilityGate(t *testing.T) {
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          44,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"model_mapping":       map[string]any{"catalog-model": "upstream-model"},
				"openai_capabilities": []any{"embeddings"},
			},
		}},
	}
	group := &Group{
		ID:       44,
		Platform: PlatformOpenAI,
		Status:   StatusActive,
		ModelAllowlist: GroupModelAllowlist{
			Enabled: true,
			Models:  []string{"catalog-model"},
		},
	}
	catalog := NewAutoModelCatalog(repo)

	for _, endpoint := range []AutoModelEndpoint{AutoModelEndpointChat, AutoModelEndpointResponses} {
		if _, ok, err := catalog.ResolveGroupModelCapability(
			context.Background(), group, "catalog-model", endpoint,
		); err != nil || ok {
			t.Fatalf("OpenAI %s: supported=%v err=%v, want unsupported without chat capability", endpoint, ok, err)
		}
	}
}

func TestAutoModelCatalogResolveSeparatesUnsupportedFromLookupFailure(t *testing.T) {
	group := &Group{ID: 21, Platform: PlatformOpenAI, Status: StatusActive}
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          4,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"known-model": "upstream-model"}},
		}},
	}
	catalog := NewAutoModelCatalog(repo)

	_, ok, err := catalog.ResolveGroupModelCapability(
		context.Background(), group, "missing-model", AutoModelEndpointChat,
	)
	if err != nil || ok {
		t.Fatalf("unsupported model: got supported=%v err=%v, want false,nil", ok, err)
	}

	_, ok, err = catalog.ResolveGroupModelCapability(
		context.Background(), group, "known-model", AutoModelEndpoint("embeddings"),
	)
	if err != nil || ok {
		t.Fatalf("unsupported endpoint: got supported=%v err=%v, want false,nil", ok, err)
	}

	lookupErr := errors.New("database unavailable")
	repo.err = lookupErr
	_, ok, err = catalog.ResolveGroupModelCapability(
		context.Background(), group, "known-model", AutoModelEndpointChat,
	)
	if ok || !errors.Is(err, lookupErr) {
		t.Fatalf("lookup failure: got supported=%v err=%v, want false and wrapped lookup error", ok, err)
	}

	_, err = catalog.ListGroupModelCapabilities(context.Background(), group)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("ListGroupModelCapabilities() error = %v, want wrapped lookup error", err)
	}
}

func TestAutoModelCatalogRejectsCompositeGroups(t *testing.T) {
	group := &Group{ID: 22, Platform: PlatformComposite, Status: StatusActive}
	catalog := NewAutoModelCatalog(&autoModelCatalogAccountRepoStub{})

	_, err := catalog.ListGroupModelCapabilities(context.Background(), group)
	if !errors.Is(err, ErrAutoModelCatalogUnsupportedGroup) {
		t.Fatalf("ListGroupModelCapabilities() error = %v, want unsupported group", err)
	}
	_, ok, err := catalog.ResolveGroupModelCapability(
		context.Background(), group, "model", AutoModelEndpointChat,
	)
	if ok || !errors.Is(err, ErrAutoModelCatalogUnsupportedGroup) {
		t.Fatalf("ResolveGroupModelCapability() = (supported=%v, err=%v), want unsupported group", ok, err)
	}
}

func TestAutoModelCatalogHonorsPlatformImageEndpointCapabilities(t *testing.T) {
	openAIRepo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          5,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": "gpt-image-2"}},
		}},
	}
	openAIGroup := &Group{
		ID:                   23,
		Platform:             PlatformOpenAI,
		Status:               StatusActive,
		AllowImageGeneration: true,
	}
	openAICatalog := NewAutoModelCatalog(openAIRepo)

	for _, endpoint := range []AutoModelEndpoint{
		AutoModelEndpointImagesGeneration,
		AutoModelEndpointImagesEdit,
	} {
		if _, ok, err := openAICatalog.ResolveGroupModelCapability(
			context.Background(), openAIGroup, "gpt-image-2", endpoint,
		); err != nil || !ok {
			t.Fatalf("OpenAI %s capability: supported=%v err=%v, want supported", endpoint, ok, err)
		}
	}
	if _, ok, err := openAICatalog.ResolveGroupModelCapability(
		context.Background(), openAIGroup, "gpt-image-2", AutoModelEndpointChat,
	); err != nil || ok {
		t.Fatalf("OpenAI image model on chat: supported=%v err=%v, want unsupported", ok, err)
	}

	grokRepo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          6,
			Platform:    PlatformGrok,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"grok-imagine-image": "grok-imagine-image"}},
		}},
	}
	grokGroup := &Group{
		ID:                   24,
		Platform:             PlatformGrok,
		Status:               StatusActive,
		AllowImageGeneration: true,
	}
	grokCatalog := NewAutoModelCatalog(grokRepo)
	if _, ok, err := grokCatalog.ResolveGroupModelCapability(
		context.Background(), grokGroup, "grok-imagine-image", AutoModelEndpointImagesGeneration,
	); err != nil || !ok {
		t.Fatalf("Grok image generation: supported=%v err=%v, want supported", ok, err)
	}
	if _, ok, err := grokCatalog.ResolveGroupModelCapability(
		context.Background(), grokGroup, "grok-imagine-image", AutoModelEndpointImagesEdit,
	); err != nil || ok {
		t.Fatalf("Grok image edit: supported=%v err=%v, want unsupported", ok, err)
	}
}

func TestAutoModelCatalogGeminiMixedSchedulingUsesAntigravityMapping(t *testing.T) {
	repo := &autoModelCatalogAccountRepoStub{
		accounts: []Account{{
			ID:          7,
			Platform:    PlatformAntigravity,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra:       map[string]any{"mixed_scheduling": true},
			Credentials: map[string]any{"model_mapping": map[string]any{"gemini-3.8-flash": "gemini-3.8-flash"}},
		}},
	}
	group := &Group{ID: 25, Platform: PlatformGemini, Status: StatusActive}

	capability, ok, err := NewAutoModelCatalog(repo).ResolveGroupModelCapability(
		context.Background(), group, "gemini-3.8-flash", AutoModelEndpointChat,
	)
	if err != nil || !ok {
		t.Fatalf("ResolveGroupModelCapability() = (%#v, %v, %v), want supported", capability, ok, err)
	}
	if !reflect.DeepEqual(capability.SourcePlatforms, []string{PlatformAntigravity}) {
		t.Fatalf("SourcePlatforms = %#v", capability.SourcePlatforms)
	}
	if !reflect.DeepEqual(repo.platforms, []string{PlatformGemini, PlatformAntigravity}) {
		t.Fatalf("candidate platforms = %#v", repo.platforms)
	}
}
