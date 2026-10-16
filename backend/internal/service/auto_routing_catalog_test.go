package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
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
