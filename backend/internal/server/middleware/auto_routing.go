package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AutoRoutingRequestKind uint8

const (
	AutoRoutingGenerationRequest AutoRoutingRequestKind = iota + 1
	AutoRoutingModelDirectoryRequest
)

type AutoRoutingRequestInput struct {
	Kind      AutoRoutingRequestKind
	Endpoint  string
	ModelID   string
	RequestID string
	Body      []byte
}

type AutoRoutingRequestError struct {
	Status  int
	Code    string
	Message string
}

func (e *AutoRoutingRequestError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type EffectiveGroupContext struct {
	APIKeyID        int64
	GroupID         int64
	ProfileID       int64
	RoutingRevision int64
	ExecutionID     int64
	AttemptNo       int
	RequestID       string
}

type effectiveGroupContextKey struct{}

type autoRoutingRequestState struct {
	authenticatedKey *service.APIKey
	preparation      *service.AutoRoutingPreparation
	endpoint         string
	dispatched       bool
	upstreamStarted  bool
	finished         bool
	resultReference  string
}

const (
	autoRoutingStateContextKey           = "auto_routing_request_state"
	autoRoutingConfigContextKey          = "auto_routing_key_config"
	autoRouteEndpointChat                = "chat"
	autoRouteEndpointResponses           = "responses"
	autoRouteEndpointMessages            = "messages"
	autoRouteEndpointImagesGeneration    = "images_generation"
	autoRouteEndpointImagesEdit          = "images_edit"
	autoRoutingDirectoryEndpoint         = "models"
	autoRoutingUnsupportedEndpoint       = "unsupported"
	autoRoutingMissingRequestIDCode      = "AUTO_ROUTING_REQUEST_ID_REQUIRED"
	autoRoutingInvalidRequestCode        = "AUTO_ROUTING_INVALID_REQUEST"
	autoRoutingUnsupportedEndpointCode   = "AUTO_ROUTING_UNSUPPORTED_ENDPOINT"
	autoRoutingStatefulResponseCode      = "AUTO_ROUTING_STATEFUL_RESPONSE_UNSUPPORTED"
	autoRoutingAmbiguousModelCode        = "AUTO_ROUTING_AMBIGUOUS_MODEL"
	autoRoutingModelRequiredCode         = "AUTO_ROUTING_MODEL_REQUIRED"
	autoRoutingUnsupportedManifestCode   = "AUTO_ROUTING_CODEX_MANIFEST_UNSUPPORTED"
	autoRoutingDirectoryEndpointCode     = "AUTO_ROUTING_MODEL_DIRECTORY"
	autoRoutingEffectiveGroupInvalidCode = "AUTO_ROUTING_EFFECTIVE_GROUP_INVALID"
	autoRoutingFinishTimeout             = 5 * time.Second
)

type autoModelDirectoryResponse struct {
	Object string                    `json:"object"`
	Data   []autoModelDirectoryEntry `json:"data"`
}

type autoModelDirectoryEntry struct {
	ModelID            string                      `json:"model_id"`
	Platform           string                      `json:"platform"`
	AccountSchedulable bool                        `json:"account_schedulable"`
	ID                 string                      `json:"id"`
	Object             string                      `json:"object"`
	Created            int64                       `json:"created"`
	OwnedBy            string                      `json:"owned_by"`
	Type               string                      `json:"type"`
	DisplayName        string                      `json:"display_name"`
	SourcePlatforms    []string                    `json:"source_platforms,omitempty"`
	UpstreamModels     []string                    `json:"upstream_models,omitempty"`
	SupportedEndpoints []service.AutoModelEndpoint `json:"supported_endpoints,omitempty"`
}

// PrepareAutoRouting resolves Auto requests before the existing fixed-group
// authorization checks. Fixed and legacy keys pass through unchanged.
func PrepareAutoRouting(
	c *gin.Context,
	apiKey *service.APIKey,
	routing *service.AutoRoutingService,
	cfg *config.Config,
) (*service.APIKey, bool) {
	if c == nil || c.Request == nil || apiKey == nil {
		return apiKey, false
	}
	switch apiKey.RoutingMode {
	case "", service.AutoRoutingModeFixed:
		return apiKey, false
	case service.AutoRoutingModeAuto:
	default:
		writeAutoRoutingFailure(c, service.ErrAutoRoutingRequestUnsupported, "")
		return apiKey, true
	}
	if routing == nil || apiKey.User == nil || apiKey.User.ID <= 0 {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return apiKey, true
	}

	path := autoRoutingRoutePath(c)
	if c.Request.Method == http.MethodGet && path == "/v1/sub2api/billing" {
		if cfg != nil && cfg.RunMode == config.RunModeSimple {
			writeAutoRoutingRequestError(c, autoRoutingError(http.StatusNotFound, "BILLING_INFO_UNAVAILABLE", "Billing information is not supported in simple mode"), "")
			return apiKey, true
		}
		if !loadAutoRoutingKeyConfig(c, apiKey, routing) {
			return apiKey, true
		}
		return apiKey, false
	}
	if c.Request.Method == http.MethodGet && path == "/v1/usage" {
		return apiKey, false
	}

	input, err := BuildAutoRoutingRequest(c)
	if err != nil {
		writeAutoRoutingFailure(c, err, autoRoutingEndpointForError(c))
		return apiKey, true
	}
	if input.Kind == AutoRoutingModelDirectoryRequest {
		if (cfg == nil || cfg.RunMode != config.RunModeSimple) && !checkAutoDirectoryKeyBudget(c, apiKey) {
			return apiKey, true
		}
		preparation, prepareErr := routing.PrepareRequestKey(c.Request.Context(), apiKey, service.AutoRoutingRequest{
			IsModelDirectory: true,
			ModelID:          input.ModelID,
		})
		if prepareErr != nil {
			writeAutoRoutingFailure(c, prepareErr, "")
			return apiKey, true
		}
		writeAutoModelDirectory(c, preparation, input.ModelID)
		return apiKey, true
	}

	preparation, err := routing.PrepareRequestKey(c.Request.Context(), apiKey, service.AutoRoutingRequest{
		Endpoint:  service.AutoModelEndpoint(input.Endpoint),
		ModelID:   input.ModelID,
		RequestID: input.RequestID,
		Body:      input.Body,
	})
	if err != nil {
		writeAutoRoutingFailure(c, err, input.Endpoint)
		return apiKey, true
	}
	if preparation == nil || preparation.EffectiveKey == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, input.Endpoint)
		return apiKey, true
	}
	if err := BindEffectiveGroupContext(c, apiKey, preparation.EffectiveKey, preparation.Subscription); err != nil {
		writeAutoRoutingFailure(c, err, input.Endpoint)
		return apiKey, true
	}
	c.Set(autoRoutingStateContextKey, &autoRoutingRequestState{
		authenticatedKey: apiKey,
		preparation:      preparation,
		endpoint:         input.Endpoint,
	})
	return preparation.EffectiveKey, false
}

func loadAutoRoutingKeyConfig(c *gin.Context, apiKey *service.APIKey, routing *service.AutoRoutingService) bool {
	if c == nil || c.Request == nil || apiKey == nil || apiKey.User == nil || routing == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return false
	}
	keyConfig, err := routing.GetAPIKeyRouting(c.Request.Context(), apiKey.User.ID, apiKey.ID)
	if err != nil {
		writeAutoRoutingFailure(c, err, "")
		return false
	}
	if keyConfig == nil || keyConfig.Mode != service.AutoRoutingModeAuto {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return false
	}
	c.Set(autoRoutingConfigContextKey, cloneAutoRoutingConfig(keyConfig))
	return true
}

// DispatchAutoRouting claims the prepared execution. Call this immediately
// before the handler sends the request upstream, never from auth before c.Next.
func DispatchAutoRouting(c *gin.Context, apiKey *service.APIKey, routing *service.AutoRoutingService) bool {
	if apiKey == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return false
	}
	if apiKey.RoutingMode == "" || apiKey.RoutingMode == service.AutoRoutingModeFixed {
		return true
	}
	state := getAutoRoutingRequestState(c)
	if apiKey.RoutingMode != service.AutoRoutingModeAuto {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingRequestUnsupported, "")
		return false
	}
	if c == nil || c.Request == nil ||
		state == nil || state.preparation == nil || state.authenticatedKey == nil || routing == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return false
	}
	if state.dispatched {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingExecutionStateConflict, state.endpoint)
		return false
	}
	preparation, err := routing.ClaimDispatch(c.Request.Context(), state.preparation)
	if err != nil {
		writeAutoRoutingFailure(c, err, state.endpoint)
		return false
	}
	if preparation == nil || preparation.EffectiveKey == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, state.endpoint)
		return false
	}
	state.preparation = preparation
	state.dispatched = true
	if err := BindEffectiveGroupContext(c, state.authenticatedKey, preparation.EffectiveKey, preparation.Subscription); err != nil {
		FinishAutoRouting(c, apiKey, routing)
		writeAutoRoutingFailure(c, err, state.endpoint)
		return false
	}
	return true
}

// SetAutoRoutingResultReference records adapter-verified result evidence for
// FinishAutoRouting. Without this evidence, a dispatched request is unknown.
func SetAutoRoutingResultReference(c *gin.Context, resultReference string) bool {
	state := getAutoRoutingRequestState(c)
	if state == nil || !state.dispatched || state.finished || strings.TrimSpace(resultReference) == "" {
		return false
	}
	state.resultReference = strings.TrimSpace(resultReference)
	state.upstreamStarted = true
	return true
}

func MarkAutoRoutingUpstreamStarted(c *gin.Context) {
	state := getAutoRoutingRequestState(c)
	if state != nil && state.dispatched && !state.finished {
		state.upstreamStarted = true
	}
}

// Only admitted, unfinished attempts may reuse their request-local claim during account failover.
func AutoRoutingDispatchStarted(c *gin.Context, apiKey *service.APIKey) bool {
	state := getAutoRoutingRequestState(c)
	if apiKey == nil || apiKey.RoutingMode != service.AutoRoutingModeAuto ||
		apiKey.AutoRouting == nil || apiKey.GroupID == nil || apiKey.Group == nil ||
		apiKey.User == nil || state == nil || !state.dispatched ||
		!state.upstreamStarted || state.finished || state.resultReference != "" ||
		state.preparation == nil || state.preparation.Execution == nil ||
		state.authenticatedKey == nil || state.authenticatedKey.User == nil {
		return false
	}
	execution := state.preparation.Execution
	metadata := apiKey.AutoRouting
	binding, ok := GetEffectiveGroupContext(c)
	return ok && apiKey.ID == state.authenticatedKey.ID &&
		apiKey.User.ID == state.authenticatedKey.User.ID &&
		execution.APIKeyID == apiKey.ID &&
		execution.Status == service.AutoRouteExecutionDispatching &&
		execution.ActualGroupID != nil && *execution.ActualGroupID == *apiKey.GroupID &&
		apiKey.Group.ID == *apiKey.GroupID &&
		binding == (EffectiveGroupContext{
			APIKeyID: apiKey.ID, GroupID: *apiKey.GroupID,
			ProfileID: execution.ProfileID, RoutingRevision: execution.RoutingRevision,
			ExecutionID: execution.ID, AttemptNo: execution.CurrentAttempt, RequestID: execution.RequestID,
		}) &&
		metadata.ProfileID == execution.ProfileID &&
		metadata.RoutingRevision == execution.RoutingRevision &&
		metadata.ExecutionID == execution.ID && metadata.AttemptNo == 1 &&
		metadata.AttemptNo == execution.CurrentAttempt && metadata.RequestID == execution.RequestID
}

// FinishAutoRouting persists a verified completion reference or marks a
// dispatched request unknown after the handler returns.
func FinishAutoRouting(c *gin.Context, apiKey *service.APIKey, routing *service.AutoRoutingService) {
	state := getAutoRoutingRequestState(c)
	if state == nil || state.finished || !state.dispatched || routing == nil || c == nil || c.Request == nil {
		return
	}
	state.finished = true
	finishContext, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), autoRoutingFinishTimeout)
	defer cancel()
	if !state.upstreamStarted {
		if err := routing.ReleaseDispatchClaim(finishContext, state.preparation); err == nil {
			return
		}
	}
	if state.resultReference != "" {
		if err := routing.Complete(finishContext, state.preparation, state.resultReference); err == nil {
			return
		}
	}
	if err := routing.MarkUnknown(finishContext, state.preparation, autoRoutingUnknownEvidence(c)); err != nil && !c.Writer.Written() {
		writeAutoRoutingFailure(c, err, state.endpoint)
	}
}

// AutoKeyConfig returns the validated Auto key configuration stored by
// PrepareAutoRouting for introspection handlers.
func AutoKeyConfig(c *gin.Context) (*service.APIKeyAutoRoutingConfig, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(autoRoutingConfigContextKey)
	if !ok {
		return nil, false
	}
	keyConfig, ok := value.(*service.APIKeyAutoRoutingConfig)
	if !ok || keyConfig == nil {
		return nil, false
	}
	return cloneAutoRoutingConfig(keyConfig), true
}

// ClassifyAutoRoutingRequest recognizes only synchronous, stateless Auto
// generation endpoints and ordinary model-directory requests.
func ClassifyAutoRoutingRequest(c *gin.Context) (AutoRoutingRequestKind, string, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return 0, "", autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Invalid request")
	}

	path := autoRoutingRoutePath(c)
	method := c.Request.Method
	if isCodexManifestRequest(c, path) {
		return 0, "", autoRoutingError(http.StatusNotFound, autoRoutingUnsupportedManifestCode, "Codex model manifests are not supported with Auto routing")
	}

	if method == http.MethodGet {
		switch path {
		case "/v1/models", "/models", "/api/v1/playground/models":
			return AutoRoutingModelDirectoryRequest, autoRoutingDirectoryEndpoint, nil
		case "/v1/models/:model", "/models/:model":
			return AutoRoutingModelDirectoryRequest, autoRoutingDirectoryEndpoint, nil
		}
		if model := strings.TrimSpace(c.Param("model")); model != "" &&
			(path == "/v1/models/"+model || path == "/models/"+model) {
			return AutoRoutingModelDirectoryRequest, autoRoutingDirectoryEndpoint, nil
		}
	}

	if method != http.MethodPost {
		return 0, "", unsupportedAutoEndpoint()
	}

	switch path {
	case "/v1/chat/completions", "/chat/completions", "/api/v1/playground/chat/completions":
		return AutoRoutingGenerationRequest, autoRouteEndpointChat, nil
	case "/v1/responses", "/responses", "/backend-api/codex/responses":
		return AutoRoutingGenerationRequest, autoRouteEndpointResponses, nil
	case "/v1/messages", "/messages", "/antigravity/v1/messages":
		return AutoRoutingGenerationRequest, autoRouteEndpointMessages, nil
	case "/v1/images/generations", "/images/generations", "/api/v1/playground/images/generations":
		return AutoRoutingGenerationRequest, autoRouteEndpointImagesGeneration, nil
	case "/v1/images/edits", "/images/edits", "/api/v1/playground/images/edits":
		return AutoRoutingGenerationRequest, autoRouteEndpointImagesEdit, nil
	default:
		return 0, "", unsupportedAutoEndpoint()
	}
}

// BuildAutoRoutingRequest extracts one unambiguous public model and preserves
// the exact wire body for the execution fingerprint and downstream handlers.
func BuildAutoRoutingRequest(c *gin.Context) (AutoRoutingRequestInput, error) {
	kind, endpoint, err := ClassifyAutoRoutingRequest(c)
	if err != nil {
		return AutoRoutingRequestInput{}, err
	}
	if kind == AutoRoutingModelDirectoryRequest {
		return AutoRoutingRequestInput{
			Kind:      kind,
			Endpoint:  endpoint,
			ModelID:   strings.TrimSpace(c.Param("model")),
			RequestID: autoRoutingBillingRequestID(c),
		}, nil
	}

	requestID := autoRoutingBillingRequestID(c)
	if requestID == "" {
		return AutoRoutingRequestInput{}, autoRoutingError(http.StatusInternalServerError, autoRoutingMissingRequestIDCode, "Auto routing requires a stable request ID")
	}

	rawBody, err := readAndRestoreAutoRequestBody(c.Request)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return AutoRoutingRequestInput{}, autoRoutingError(http.StatusRequestEntityTooLarge, autoRoutingInvalidRequestCode, "Request body is too large")
		}
		return AutoRoutingRequestInput{}, autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Failed to read request body")
	}
	decodedBody, err := decodeAutoRequestBody(c.Request, rawBody)
	if err != nil {
		return AutoRoutingRequestInput{}, autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Invalid or unsupported request body encoding")
	}

	model, err := extractAutoRequestModel(c.FullPath(), endpoint, c.GetHeader("Content-Type"), decodedBody)
	if err != nil {
		return AutoRoutingRequestInput{}, err
	}
	if endpoint == autoRouteEndpointResponses {
		if err := rejectPreviousResponseID(decodedBody, c.GetHeader("Content-Type")); err != nil {
			return AutoRoutingRequestInput{}, err
		}
	}

	return AutoRoutingRequestInput{
		Kind:      kind,
		Endpoint:  endpoint,
		ModelID:   model,
		RequestID: requestID,
		Body:      rawBody,
	}, nil
}

// BindEffectiveGroupContext installs a service-prepared key clone without
// changing the authenticated key or any group object it references.
func BindEffectiveGroupContext(c *gin.Context, authenticatedKey, effectiveKey *service.APIKey, subscription *service.UserSubscription) error {
	if c == nil || c.Request == nil || authenticatedKey == nil || effectiveKey == nil ||
		authenticatedKey == effectiveKey || effectiveKey.AutoRouting == nil ||
		effectiveKey.ID != authenticatedKey.ID ||
		effectiveKey.User == nil || authenticatedKey.User == nil ||
		effectiveKey.User.ID != authenticatedKey.User.ID ||
		effectiveKey.User == authenticatedKey.User ||
		effectiveKey.GroupID == nil || *effectiveKey.GroupID <= 0 ||
		effectiveKey.Group == nil || effectiveKey.Group.ID != *effectiveKey.GroupID ||
		effectiveKey.Group == authenticatedKey.Group ||
		effectiveKey.GroupID == authenticatedKey.GroupID ||
		!service.IsGroupContextValid(effectiveKey.Group) ||
		effectiveKey.AutoRouting.ProfileID <= 0 ||
		effectiveKey.AutoRouting.RoutingRevision <= 0 ||
		effectiveKey.AutoRouting.ExecutionID <= 0 ||
		effectiveKey.AutoRouting.RequestID == "" ||
		effectiveKey.AutoRouting.AttemptNo < 0 ||
		effectiveKey.AutoRouting.AttemptNo > 1 {
		return autoRoutingError(http.StatusInternalServerError, autoRoutingEffectiveGroupInvalidCode, "Auto routing did not bind a valid request-local group")
	}

	metadata := effectiveKey.AutoRouting
	binding := EffectiveGroupContext{
		APIKeyID:        effectiveKey.ID,
		GroupID:         *effectiveKey.GroupID,
		ProfileID:       metadata.ProfileID,
		RoutingRevision: metadata.RoutingRevision,
		ExecutionID:     metadata.ExecutionID,
		AttemptNo:       metadata.AttemptNo,
		RequestID:       metadata.RequestID,
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, effectiveKey.Group)
	ctx = context.WithValue(ctx, ctxkey.UserID, effectiveKey.User.ID)
	ctx = context.WithValue(ctx, effectiveGroupContextKey{}, binding)
	c.Request = c.Request.WithContext(ctx)
	c.Set(string(ContextKeyAPIKey), effectiveKey)
	SetOpsFallbackAPIKey(c, effectiveKey)
	if subscription != nil {
		c.Set(string(ContextKeySubscription), subscription)
	}
	return nil
}

func GetEffectiveGroupContext(c *gin.Context) (EffectiveGroupContext, bool) {
	if c == nil || c.Request == nil {
		return EffectiveGroupContext{}, false
	}
	binding, ok := c.Request.Context().Value(effectiveGroupContextKey{}).(EffectiveGroupContext)
	return binding, ok
}

func getAutoRoutingRequestState(c *gin.Context) *autoRoutingRequestState {
	if c == nil {
		return nil
	}
	value, ok := c.Get(autoRoutingStateContextKey)
	if !ok {
		return nil
	}
	state, _ := value.(*autoRoutingRequestState)
	return state
}

func cloneAutoRoutingConfig(config *service.APIKeyAutoRoutingConfig) *service.APIKeyAutoRoutingConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	cloned.GroupIDs = append([]int64(nil), config.GroupIDs...)
	return &cloned
}

func checkAutoDirectoryKeyBudget(c *gin.Context, apiKey *service.APIKey) bool {
	if apiKey == nil {
		writeAutoRoutingFailure(c, service.ErrAPIKeyNotFound, "")
		return false
	}
	if apiKey.Status == service.StatusAPIKeyExpired || apiKey.IsExpired() {
		writeAutoRoutingRequestError(c, autoRoutingError(http.StatusForbidden, "API_KEY_EXPIRED", "API key has expired"), "")
		return false
	}
	if apiKey.Status == service.StatusAPIKeyQuotaExhausted || apiKey.IsQuotaExhausted() {
		abortWithAPIKeyQuotaError(c)
		return false
	}
	return true
}

func writeAutoModelDirectory(c *gin.Context, preparation *service.AutoRoutingPreparation, requestedModel string) {
	if c == nil || preparation == nil {
		writeAutoRoutingFailure(c, service.ErrAutoRoutingUnavailable, "")
		return
	}
	capabilities := preparation.Capabilities
	if requestedModel != "" && len(capabilities) == 0 {
		writeAutoRoutingRequestError(c, autoRoutingError(http.StatusNotFound, "model_not_found", "The requested model is not available for Auto routing"), "")
		return
	}
	entries := make([]autoModelDirectoryEntry, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		modelID := strings.TrimSpace(capability.ModelID)
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		entries = append(entries, autoModelDirectoryEntry{
			ModelID:            capability.ModelID,
			Platform:           capability.Platform,
			AccountSchedulable: capability.AccountSchedulable,
			ID:                 modelID,
			Object:             "model",
			Created:            1704067200,
			OwnedBy:            capability.Platform,
			Type:               "model",
			DisplayName:        modelID,
			SourcePlatforms:    append([]string(nil), capability.SourcePlatforms...),
			UpstreamModels:     append([]string(nil), capability.UpstreamModels...),
			SupportedEndpoints: append([]service.AutoModelEndpoint(nil), capability.SupportedEndpoints...),
		})
	}
	if requestedModel != "" {
		if len(entries) == 0 || entries[0].ID != requestedModel {
			writeAutoRoutingRequestError(c, autoRoutingError(http.StatusNotFound, "model_not_found", "The requested model is not available for Auto routing"), "")
			return
		}
		c.JSON(http.StatusOK, entries[0])
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, autoModelDirectoryResponse{Object: "list", Data: entries})
	c.Abort()
}

func writeAutoRoutingFailure(c *gin.Context, err error, endpoint string) {
	if c == nil {
		return
	}
	var requestErr *AutoRoutingRequestError
	if errors.As(err, &requestErr) {
		writeAutoRoutingRequestError(c, requestErr, endpoint)
		return
	}
	var applicationErr *infraerrors.ApplicationError
	if errors.As(err, &applicationErr) {
		writeAutoRoutingRequestError(c, autoRoutingError(
			int(applicationErr.Code),
			applicationErr.Reason,
			applicationErr.Message,
		), endpoint)
		return
	}
	writeAutoRoutingRequestError(c, autoRoutingError(
		http.StatusServiceUnavailable,
		"AUTO_ROUTING_UNAVAILABLE",
		"Auto routing is temporarily unavailable",
	), endpoint)
}

func writeAutoRoutingRequestError(c *gin.Context, requestErr *AutoRoutingRequestError, endpoint string) {
	if c == nil || requestErr == nil {
		return
	}
	if c.Writer != nil && c.Writer.Written() {
		c.Abort()
		return
	}
	if endpoint == autoRouteEndpointMessages {
		AnthropicErrorWriter(c, requestErr.Status, requestErr.Message)
		c.Abort()
		return
	}
	if strings.HasPrefix(autoRoutingRoutePath(c), "/api/v1/playground/") {
		AbortWithError(c, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	errorType := "invalid_request_error"
	if requestErr.Status >= http.StatusInternalServerError {
		errorType = "server_error"
	}
	code := requestErr.Code
	if code == "" {
		code = "AUTO_ROUTING_ERROR"
	}
	c.JSON(requestErr.Status, gin.H{
		"error": gin.H{
			"message": requestErr.Message,
			"type":    errorType,
			"code":    code,
		},
	})
	c.Abort()
}

func autoRoutingEndpointForError(c *gin.Context) string {
	switch autoRoutingRoutePath(c) {
	case "/v1/messages", "/messages", "/antigravity/v1/messages":
		return autoRouteEndpointMessages
	default:
		return ""
	}
}

func autoRoutingUnknownEvidence(c *gin.Context) string {
	if c != nil && c.Request != nil {
		switch {
		case errors.Is(c.Request.Context().Err(), context.Canceled):
			return service.AutoRouteEvidenceClientCanceled
		case errors.Is(c.Request.Context().Err(), context.DeadlineExceeded):
			return service.AutoRouteEvidenceUpstreamTimeout
		case c.Writer != nil && c.Writer.Written():
			return service.AutoRouteEvidenceResponseStarted
		}
	}
	return service.AutoRouteEvidenceConnectionInterrupted
}

func extractAutoRequestModel(routePath, endpoint, contentType string, body []byte) (string, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return "", autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "A valid Content-Type is required")
	}
	var model string
	switch {
	case strings.EqualFold(mediaType, "application/json") || strings.HasSuffix(strings.ToLower(mediaType), "+json"):
		model, err = extractStrictJSONModel(body)
	case strings.EqualFold(mediaType, "multipart/form-data") && endpoint == autoRouteEndpointImagesEdit:
		model, err = extractStrictMultipartModel(contentType, body)
	default:
		err = errors.New("unsupported content type")
	}
	if err != nil {
		if errors.Is(err, errAutoModelMissing) {
			return "", autoRoutingError(http.StatusBadRequest, autoRoutingModelRequiredCode, "A model is required for Auto routing")
		}
		if errors.Is(err, errAutoModelAmbiguous) {
			return "", autoRoutingError(http.StatusBadRequest, autoRoutingAmbiguousModelCode, "Request contains ambiguous model fields")
		}
		return "", autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Request body must contain a valid model")
	}

	candidates := requestmodel.FromBodyCandidates(routePath, contentType, body)
	if len(candidates) == 0 || candidates[0] != model {
		return "", autoRoutingError(http.StatusBadRequest, autoRoutingAmbiguousModelCode, "Auto routing could not resolve the same model as the gateway handler")
	}
	for _, candidate := range candidates {
		if candidate != model {
			return "", autoRoutingError(http.StatusBadRequest, autoRoutingAmbiguousModelCode, "Request contains conflicting model fields")
		}
	}
	return model, nil
}

var (
	errAutoModelMissing   = errors.New("auto routing model missing")
	errAutoModelAmbiguous = errors.New("auto routing model ambiguous")
)

type autoJSONField struct {
	name  string
	value json.RawMessage
}

func extractStrictJSONModel(body []byte) (string, error) {
	if !json.Valid(body) {
		return "", errors.New("invalid JSON")
	}
	fields, err := decodeJSONObjectFields(body)
	if err != nil {
		return "", err
	}
	topModel, err := uniqueJSONModel(fields)
	if err != nil {
		return "", err
	}

	var sessionModel string
	sessionFields := matchingJSONFields(fields, "session")
	if len(sessionFields) > 1 {
		return "", errAutoModelAmbiguous
	}
	if len(sessionFields) == 1 {
		if sessionFields[0].name != "session" {
			return "", errAutoModelAmbiguous
		}
		sessionFields, err = decodeJSONObjectFields(sessionFields[0].value)
		if err != nil {
			return "", errors.New("session must be an object")
		}
		sessionModel, err = uniqueJSONModel(sessionFields)
		if err != nil {
			return "", err
		}
	}

	if topModel != "" && sessionModel != "" && topModel != sessionModel {
		return "", errAutoModelAmbiguous
	}
	if topModel != "" {
		return topModel, nil
	}
	if sessionModel != "" {
		return sessionModel, nil
	}
	return "", errAutoModelMissing
}

func decodeJSONObjectFields(body []byte) ([]autoJSONField, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("JSON request body must be an object")
	}

	fields := make([]autoJSONField, 0, 8)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("JSON object key is invalid")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, autoJSONField{name: name, value: value})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return fields, nil
}

func uniqueJSONModel(fields []autoJSONField) (string, error) {
	modelFields := matchingJSONFields(fields, "model")
	if len(modelFields) == 0 {
		return "", nil
	}
	if len(modelFields) != 1 || modelFields[0].name != "model" {
		return "", errAutoModelAmbiguous
	}
	var model string
	if err := json.Unmarshal(modelFields[0].value, &model); err != nil {
		return "", err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errAutoModelMissing
	}
	return model, nil
}

func matchingJSONFields(fields []autoJSONField, name string) []autoJSONField {
	matches := make([]autoJSONField, 0, 1)
	for _, field := range fields {
		if strings.EqualFold(field.name, name) {
			matches = append(matches, field)
		}
	}
	return matches
}

func extractStrictMultipartModel(contentType string, body []byte) (string, error) {
	_, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || strings.TrimSpace(params["boundary"]) == "" {
		return "", errors.New("multipart boundary is required")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var model string
	var session string
	modelCount := 0
	sessionCount := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(part.FormName())
		if !strings.EqualFold(name, "model") && !strings.EqualFold(name, "session") {
			continue
		}
		if name != "model" && name != "session" {
			return "", errAutoModelAmbiguous
		}
		if part.FileName() != "" {
			return "", errors.New("model fields cannot be files")
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return "", err
		}
		switch name {
		case "model":
			modelCount++
			model = strings.TrimSpace(string(value))
		case "session":
			sessionCount++
			session = strings.TrimSpace(string(value))
		}
	}
	if modelCount > 1 || sessionCount > 1 {
		return "", errAutoModelAmbiguous
	}
	if sessionCount == 1 {
		fields, err := decodeJSONObjectFields([]byte(session))
		if err != nil {
			return "", err
		}
		session, err = uniqueJSONModel(fields)
		if err != nil {
			return "", err
		}
	}
	if model != "" && session != "" && model != session {
		return "", errAutoModelAmbiguous
	}
	if model != "" {
		return model, nil
	}
	if session != "" {
		return session, nil
	}
	return "", errAutoModelMissing
}

func rejectPreviousResponseID(body []byte, contentType string) error {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || (!strings.EqualFold(mediaType, "application/json") && !strings.HasSuffix(strings.ToLower(mediaType), "+json")) {
		return autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Responses requests must use JSON")
	}
	fields, err := decodeJSONObjectFields(body)
	if err != nil {
		return autoRoutingError(http.StatusBadRequest, autoRoutingInvalidRequestCode, "Invalid Responses request body")
	}
	if len(matchingJSONFields(fields, "previous_response_id")) > 0 {
		return autoRoutingError(http.StatusBadRequest, autoRoutingStatefulResponseCode, "Auto routing does not support previous_response_id")
	}
	return nil
}

func readAndRestoreAutoRequestBody(request *http.Request) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, nil
	}
	var body []byte
	if preread, ok := request.Body.(*httputil.PrereadBody); ok {
		body = preread.Bytes()
	} else {
		readBody, err := io.ReadAll(request.Body)
		body = readBody
		request.Body = httputil.NewPrereadBody(body)
		request.ContentLength = int64(len(body))
		request.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		if err != nil {
			return body, err
		}
	}
	return append([]byte(nil), body...), nil
}

func decodeAutoRequestBody(request *http.Request, rawBody []byte) ([]byte, error) {
	if request == nil || len(rawBody) == 0 {
		return rawBody, nil
	}
	encoding := strings.TrimSpace(request.Header.Get("Content-Encoding"))
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return rawBody, nil
	}
	clone := request.Clone(request.Context())
	clone.Body = io.NopCloser(bytes.NewReader(rawBody))
	clone.ContentLength = int64(len(rawBody))
	return httputil.ReadRequestBodyWithPrealloc(clone)
}

func autoRoutingBillingRequestID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if requestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(requestID) != "" {
		return "client:" + strings.TrimSpace(requestID)
	}
	if requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
		return "local:" + strings.TrimSpace(requestID)
	}
	return ""
}

func autoRoutingRoutePath(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if routePath := c.FullPath(); routePath != "" {
		return routePath
	}
	return c.Request.URL.Path
}

func isCodexManifestRequest(c *gin.Context, path string) bool {
	if path == "/backend-api/codex/models" || path == "/backend-api/codex/models/:model" {
		return true
	}
	if c == nil || c.Request == nil {
		return false
	}
	_, hasClientVersion := c.Request.URL.Query()["client_version"]
	return hasClientVersion && (path == "/v1/models" || path == "/models" || path == "/v1/models/:model" || path == "/models/:model")
}

func unsupportedAutoEndpoint() error {
	return autoRoutingError(http.StatusNotFound, autoRoutingUnsupportedEndpointCode, "This endpoint is not supported with Auto routing")
}

func autoRoutingError(status int, code, message string) *AutoRoutingRequestError {
	return &AutoRoutingRequestError{Status: status, Code: code, Message: message}
}
