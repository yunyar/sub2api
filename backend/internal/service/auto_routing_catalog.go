package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type AutoModelEndpoint string

const (
	AutoModelEndpointChat             AutoModelEndpoint = "chat"
	AutoModelEndpointResponses        AutoModelEndpoint = "responses"
	AutoModelEndpointMessages         AutoModelEndpoint = "messages"
	AutoModelEndpointImagesGeneration AutoModelEndpoint = "images_generation"
	AutoModelEndpointImagesEdit       AutoModelEndpoint = "images_edit"
)

var (
	ErrAutoModelCatalogUnavailable      = errors.New("auto model catalog is unavailable")
	ErrAutoModelCatalogInvalidGroup     = errors.New("auto model catalog requires a valid group")
	ErrAutoModelCatalogUnsupportedGroup = errors.New("auto model catalog does not support this group")
)

// AutoModelCapability describes one public model and the endpoints that can
// serve it through persistently schedulable accounts in a normal group.
type AutoModelCapability struct {
	ModelID            string              `json:"model_id"`
	Platform           string              `json:"platform"`
	SourcePlatforms    []string            `json:"source_platforms"`
	UpstreamModels     []string            `json:"upstream_models"`
	SupportedEndpoints []AutoModelEndpoint `json:"supported_endpoints"`
	AccountSchedulable bool                `json:"account_schedulable"`
}

// AutoModelCatalog resolves group capabilities without invoking HTTP handlers
// or mutating scheduler, reservation, or rate-limit state.
type AutoModelCatalog struct {
	accountRepo AccountRepository
}

func NewAutoModelCatalog(accountRepo AccountRepository) *AutoModelCatalog {
	return &AutoModelCatalog{accountRepo: accountRepo}
}

// ListGroupModelCapabilities returns the sorted public-model union for a normal
// group. It uses the same account mappings used by request scheduling and
// ignores transient load, rate limits, and temporary account state.
func (c *AutoModelCatalog) ListGroupModelCapabilities(
	ctx context.Context,
	group *Group,
) ([]AutoModelCapability, error) {
	if err := validateAutoModelCatalogGroup(group); err != nil {
		return nil, err
	}
	if !group.IsActive() {
		return []AutoModelCapability{}, nil
	}
	accounts, err := c.listEligibleAccounts(ctx, group)
	if err != nil {
		return nil, err
	}
	modelIDs := autoModelCatalogModelIDs(group, accounts)
	capabilities := make([]AutoModelCapability, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(modelID) {
			continue
		}
		capability, ok := autoModelCatalogResolveFromAccounts(group, accounts, modelID, "")
		if ok {
			capabilities = append(capabilities, capability)
		}
	}
	return capabilities, nil
}

// ResolveGroupModelCapability reports whether a public model is supported by
// at least one persistently eligible account for the requested endpoint.
// AccountSchedulable separately reports whether any supporting account is
// schedulable right now, including transient account state.
// A false result with nil error means unsupported; an error means the lookup
// could not establish support and callers must fail closed.
func (c *AutoModelCatalog) ResolveGroupModelCapability(
	ctx context.Context,
	group *Group,
	modelID string,
	endpoint AutoModelEndpoint,
) (AutoModelCapability, bool, error) {
	if err := validateAutoModelCatalogGroup(group); err != nil {
		return AutoModelCapability{}, false, err
	}
	if !group.IsActive() || !autoModelEndpointSupported(endpoint) || strings.TrimSpace(modelID) == "" {
		return AutoModelCapability{}, false, nil
	}
	if group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(modelID) {
		return AutoModelCapability{}, false, nil
	}
	accounts, err := c.listEligibleAccounts(ctx, group)
	if err != nil {
		return AutoModelCapability{}, false, err
	}
	modelID = strings.TrimSpace(modelID)
	capability, ok := autoModelCatalogResolveFromAccounts(group, accounts, modelID, endpoint)
	if !ok {
		return AutoModelCapability{}, false, nil
	}
	capability.AccountSchedulable = autoModelCatalogHasSchedulableAccount(group, accounts, modelID, endpoint)
	return capability, ok, nil
}

func (c *AutoModelCatalog) listEligibleAccounts(ctx context.Context, group *Group) ([]Account, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrAutoModelCatalogInvalidGroup)
	}
	if c == nil || c.accountRepo == nil {
		return nil, ErrAutoModelCatalogUnavailable
	}
	groupID := group.ID
	platforms := []string{group.Platform}
	if group.Platform == PlatformGemini {
		platforms = append(platforms, PlatformAntigravity)
	}
	accounts, err := c.accountRepo.ListModelAvailabilityCandidates(ctx, &groupID, platforms, false)
	if err != nil {
		return nil, fmt.Errorf("list auto model catalog accounts: %w", err)
	}

	eligible := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.Status != StatusActive || !account.Schedulable {
			continue
		}
		if account.Platform != group.Platform &&
			!(group.Platform == PlatformGemini && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled()) {
			continue
		}
		eligible = append(eligible, *account)
	}
	return eligible, nil
}

func validateAutoModelCatalogGroup(group *Group) error {
	if group == nil || group.ID <= 0 {
		return ErrAutoModelCatalogInvalidGroup
	}
	if group.Platform == PlatformComposite || !domain.IsConcretePlatform(group.Platform) {
		return fmt.Errorf("%w: platform %q", ErrAutoModelCatalogUnsupportedGroup, group.Platform)
	}
	return nil
}

func autoModelEndpointSupported(endpoint AutoModelEndpoint) bool {
	switch endpoint {
	case AutoModelEndpointChat,
		AutoModelEndpointResponses,
		AutoModelEndpointMessages,
		AutoModelEndpointImagesGeneration,
		AutoModelEndpointImagesEdit:
		return true
	default:
		return false
	}
}

func autoModelCatalogModelIDs(group *Group, accounts []Account) []string {
	modelSet := make(map[string]struct{})
	hasModelMappings := false
	hasUnmappedOpenAI := false
	for i := range accounts {
		account := &accounts[i]
		mapping := account.GetModelMapping()
		if account.Platform == PlatformOpenAI && account.IsOpenAIPassthroughEnabled() {
			hasUnmappedOpenAI = true
			continue
		}
		if len(mapping) == 0 {
			if account.Platform == PlatformOpenAI {
				hasUnmappedOpenAI = true
			}
			continue
		}
		hasModelMappings = true
		for modelID, upstreamModel := range mapping {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" || strings.Contains(modelID, "*") || strings.TrimSpace(upstreamModel) == "" {
				continue
			}
			if group.Platform == PlatformGemini &&
				account.Platform == PlatformAntigravity &&
				!mixedListingModelAllowed(group.Platform, modelID) {
				continue
			}
			modelSet[modelID] = struct{}{}
		}
	}

	if !hasModelMappings {
		for _, modelID := range autoModelCatalogDefaultModelIDs(group.Platform) {
			modelSet[modelID] = struct{}{}
		}
	} else if group.Platform == PlatformOpenAI && hasUnmappedOpenAI {
		for _, modelID := range openai.DefaultModelIDs() {
			modelSet[modelID] = struct{}{}
		}
	}

	if group.Platform == PlatformOpenAI || group.Platform == PlatformGrok || IsMultiProtocolAPIKeyProvider(group.Platform) {
		for _, modelID := range claude.DefaultModelIDs() {
			modelSet[modelID] = struct{}{}
		}
	}

	modelIDs := make([]string, 0, len(modelSet))
	for modelID := range modelSet {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	return modelIDs
}

func autoModelCatalogDefaultModelIDs(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformAnthropic:
		return claude.DefaultModelIDs()
	case PlatformGemini:
		modelIDs := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			modelIDs = append(modelIDs, model.ID)
		}
		return modelIDs
	case PlatformGrok:
		return autoModelCatalogMappedModelIDs(PlatformGrok)
	case PlatformAntigravity:
		return autoModelCatalogMappedModelIDs(PlatformAntigravity)
	case PlatformOpenCodeGo:
		return DefaultOpenCodeGoModelIDs()
	default:
		return nil
	}
}

func autoModelCatalogMappedModelIDs(platform string) []string {
	var account Account
	account.Platform = platform
	modelIDs := make([]string, 0, len(account.GetModelMapping()))
	for modelID, upstreamModel := range account.GetModelMapping() {
		if strings.TrimSpace(modelID) != "" && strings.TrimSpace(upstreamModel) != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	sort.Strings(modelIDs)
	return modelIDs
}

func autoModelCatalogResolveFromAccounts(
	group *Group,
	accounts []Account,
	modelID string,
	requiredEndpoint AutoModelEndpoint,
) (AutoModelCapability, bool) {
	capability := AutoModelCapability{
		ModelID:  modelID,
		Platform: group.Platform,
	}
	sourcePlatforms := make(map[string]struct{})
	upstreamModels := make(map[string]struct{})
	supportedEndpoints := make(map[AutoModelEndpoint]struct{})
	for i := range accounts {
		account := &accounts[i]
		for _, endpoint := range autoModelCatalogEndpoints {
			if requiredEndpoint != "" && endpoint != requiredEndpoint {
				continue
			}
			upstreamModel, supported := autoModelCatalogAccountEndpointModel(group, account, modelID, endpoint)
			if !supported {
				continue
			}
			supportedEndpoints[endpoint] = struct{}{}
			sourcePlatforms[account.Platform] = struct{}{}
			upstreamModels[upstreamModel] = struct{}{}
		}
	}
	if len(supportedEndpoints) == 0 {
		return AutoModelCapability{}, false
	}
	for _, endpoint := range autoModelCatalogEndpoints {
		if _, ok := supportedEndpoints[endpoint]; ok {
			capability.SupportedEndpoints = append(capability.SupportedEndpoints, endpoint)
		}
	}
	capability.SourcePlatforms = sortedAutoModelCatalogSet(sourcePlatforms)
	capability.UpstreamModels = sortedAutoModelCatalogSet(upstreamModels)
	capability.AccountSchedulable = true
	return capability, true
}

func autoModelCatalogHasSchedulableAccount(
	group *Group,
	accounts []Account,
	modelID string,
	endpoint AutoModelEndpoint,
) bool {
	for i := range accounts {
		account := &accounts[i]
		if !account.IsSchedulable() {
			continue
		}
		if _, supported := autoModelCatalogAccountEndpointModel(group, account, modelID, endpoint); supported {
			return true
		}
	}
	return false
}

var autoModelCatalogEndpoints = [...]AutoModelEndpoint{
	AutoModelEndpointChat,
	AutoModelEndpointResponses,
	AutoModelEndpointMessages,
	AutoModelEndpointImagesGeneration,
	AutoModelEndpointImagesEdit,
}

func autoModelCatalogAccountEndpointModel(
	group *Group,
	account *Account,
	modelID string,
	endpoint AutoModelEndpoint,
) (string, bool) {
	requestModel := modelID
	if endpoint == AutoModelEndpointMessages && domain.UsesOpenAIGateway(group.Platform) {
		switch {
		case group.Platform == PlatformOpenAI:
			if !group.AllowMessagesDispatch {
				return "", false
			}
			requestModel = group.ResolveMessagesDispatchModel(modelID)
		case group.Platform == PlatformGrok:
			requestModel = group.ResolveMessagesDispatchModel(modelID)
		case IsMultiProtocolAPIKeyProvider(group.Platform):
		default:
			return "", false
		}
		if requestModel == "" {
			return "", false
		}
	}
	if !autoModelCatalogAccountSupportsModel(group, account, requestModel) {
		return "", false
	}
	upstreamModel := account.GetMappedModel(requestModel)
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = requestModel
	}
	if !autoModelCatalogEndpointAllowed(group, account, modelID, requestModel, upstreamModel, endpoint) {
		return "", false
	}
	return upstreamModel, true
}

func autoModelCatalogAccountSupportsModel(group *Group, account *Account, modelID string) bool {
	if account.Platform == PlatformAntigravity {
		if group.Platform == PlatformGemini && !mixedListingModelAllowed(group.Platform, modelID) {
			return false
		}
		return mapAntigravityModel(account, modelID) != ""
	}
	return account.IsModelSupported(modelID)
}

func autoModelCatalogEndpointAllowed(
	group *Group,
	account *Account,
	publicModel string,
	requestModel string,
	upstreamModel string,
	endpoint AutoModelEndpoint,
) bool {
	switch endpoint {
	case AutoModelEndpointChat:
		if group.ClaudeCodeOnly || isOpenAIImageGenerationModel(publicModel) {
			return false
		}
		if domain.UsesOpenAIGateway(group.Platform) {
			return account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions)
		}
		return group.Platform == PlatformAnthropic ||
			group.Platform == PlatformGemini ||
			group.Platform == PlatformAntigravity
	case AutoModelEndpointResponses:
		if group.ClaudeCodeOnly {
			return false
		}
		if domain.UsesOpenAIGateway(group.Platform) {
			if group.Platform == PlatformOpenAI &&
				(isOpenAIImageGenerationModel(publicModel) || isOpenAIImageGenerationModel(requestModel) || isOpenAIImageGenerationModel(upstreamModel)) {
				return account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses)
			}
			return account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions)
		}
		return group.Platform == PlatformAnthropic
	case AutoModelEndpointMessages:
		if domain.UsesOpenAIGateway(group.Platform) {
			return account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions)
		}
		return group.Platform == PlatformAnthropic ||
			group.Platform == PlatformGemini ||
			group.Platform == PlatformAntigravity
	case AutoModelEndpointImagesGeneration:
		if !group.AllowImageGeneration {
			return false
		}
		switch group.Platform {
		case PlatformOpenAI:
			return autoOpenAIImagesModelSupported(account, publicModel, requestModel, upstreamModel)
		case PlatformGrok:
			if !isGrokImageGenerationModel(publicModel) && !isGrokImageGenerationModel(requestModel) && !isGrokImageGenerationModel(upstreamModel) {
				return false
			}
			eligible, _ := account.GrokMediaGenerationEligibility()
			return eligible && account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration)
		default:
			return false
		}
	case AutoModelEndpointImagesEdit:
		if !group.AllowImageGeneration || group.Platform != PlatformOpenAI {
			return false
		}
		// The shared Playground image-edit route is currently OpenAI-only.
		return autoOpenAIImagesModelSupported(account, publicModel, requestModel, upstreamModel)
	default:
		return false
	}
}

func autoOpenAIImagesModelSupported(account *Account, models ...string) bool {
	hasImageModel := false
	for _, model := range models {
		if IsGPTImageGenerationModel(model) || isGeminiCompatibleImageModel(model) {
			hasImageModel = true
			break
		}
	}
	if !hasImageModel || account.Platform != PlatformOpenAI {
		return false
	}
	return account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic) ||
		account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityNative) ||
		account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityAPIKey)
}

func sortedAutoModelCatalogSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
