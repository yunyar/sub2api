package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClassifyAutoRoutingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		method     string
		path       string
		wantKind   AutoRoutingRequestKind
		wantTarget string
		wantCode   string
	}{
		{name: "chat generation", method: http.MethodPost, path: "/v1/chat/completions", wantKind: AutoRoutingGenerationRequest, wantTarget: autoRouteEndpointChat},
		{name: "model directory", method: http.MethodGet, path: "/v1/models", wantKind: AutoRoutingModelDirectoryRequest, wantTarget: autoRoutingDirectoryEndpoint},
		{name: "native google models", method: http.MethodGet, path: "/v1beta/models", wantCode: autoRoutingUnsupportedEndpointCode},
		{name: "codex manifest", method: http.MethodGet, path: "/backend-api/codex/models", wantKind: AutoRoutingModelDirectoryRequest, wantTarget: autoRoutingDirectoryEndpoint},
		{name: "codex client manifest", method: http.MethodGet, path: "/models?client_version=0.1", wantKind: AutoRoutingModelDirectoryRequest, wantTarget: autoRoutingDirectoryEndpoint},
		{name: "codex v1 client manifest", method: http.MethodGet, path: "/v1/models?client_version=0.1", wantKind: AutoRoutingModelDirectoryRequest, wantTarget: autoRoutingDirectoryEndpoint},
		{name: "codex model detail", method: http.MethodGet, path: "/v1/models/gpt-6-luna?client_version=0.1", wantKind: AutoRoutingModelDirectoryRequest, wantTarget: autoRoutingDirectoryEndpoint},
		{name: "codex post is not a manifest", method: http.MethodPost, path: "/models?client_version=0.1", wantCode: autoRoutingUnsupportedEndpointCode},
		{name: "embeddings", method: http.MethodPost, path: "/v1/embeddings", wantCode: autoRoutingUnsupportedEndpointCode},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(test.method, test.path, nil)

			kind, target, err := ClassifyAutoRoutingRequest(ctx)
			if test.wantCode != "" {
				var requestErr *AutoRoutingRequestError
				if !errors.As(err, &requestErr) {
					t.Fatalf("ClassifyAutoRoutingRequest() error = %v, want AutoRoutingRequestError", err)
				}
				if requestErr.Code != test.wantCode {
					t.Fatalf("error code = %q, want %q", requestErr.Code, test.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ClassifyAutoRoutingRequest() error = %v", err)
			}
			if kind != test.wantKind || target != test.wantTarget {
				t.Fatalf("classification = (%v, %q), want (%v, %q)", kind, target, test.wantKind, test.wantTarget)
			}
		})
	}
}

func TestIsCodexManifestRequestOnlyMatchesCollectionGET(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/v1/models?client_version=0.1", true},
		{http.MethodGet, "/models?client_version=0.1", true},
		{http.MethodGet, "/v1/models?client_version=", false},
		{http.MethodGet, "/backend-api/codex/models", true},
		{http.MethodGet, "/v1/models/gpt-6-luna?client_version=0.1", false},
		{http.MethodGet, "/backend-api/codex/models/gpt-6-luna", false},
		{http.MethodPost, "/models?client_version=0.1", false},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(test.method, test.path, nil)
		if got := isCodexManifestRequest(ctx, ctx.Request.URL.Path); got != test.want {
			t.Fatalf("isCodexManifestRequest(%s %s) = %t, want %t", test.method, test.path, got, test.want)
		}
	}
}

func TestWriteAutoCodexModelsManifestRevalidatesFinalDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capability := func(modelID string) service.AutoModelCapability {
		return service.AutoModelCapability{
			ModelID: modelID, Platform: service.PlatformOpenAI,
			SupportedEndpoints: []service.AutoModelEndpoint{service.AutoModelEndpointResponses},
		}
	}
	first := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(first)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.1", nil)
	writeAutoCodexModelsManifest(ctx, &service.AutoRoutingPreparation{
		Capabilities: []service.AutoModelCapability{capability("first-model")},
	}, "")
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), `"slug":"first-model"`)
	firstETag := first.Header().Get("ETag")
	require.NotEmpty(t, firstETag)
	require.Equal(t, "private, no-cache", first.Header().Get("Cache-Control"))
	require.Contains(t, first.Header().Values("Vary"), "Authorization")
	require.Contains(t, first.Header().Values("Vary"), "X-Api-Key")

	unchanged := httptest.NewRecorder()
	unchangedContext, _ := gin.CreateTestContext(unchanged)
	unchangedContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.1", nil)
	unchangedContext.Request.Header.Set("If-None-Match", firstETag)
	writeAutoCodexModelsManifest(unchangedContext, &service.AutoRoutingPreparation{
		Capabilities: []service.AutoModelCapability{capability("first-model")},
	}, "")
	require.Equal(t, http.StatusNotModified, unchanged.Code)
	require.Empty(t, unchanged.Body.String())
	require.Equal(t, firstETag, unchanged.Header().Get("ETag"))

	changed := httptest.NewRecorder()
	changedContext, _ := gin.CreateTestContext(changed)
	changedContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.1", nil)
	changedContext.Request.Header.Set("If-None-Match", firstETag)
	writeAutoCodexModelsManifest(changedContext, &service.AutoRoutingPreparation{
		Capabilities: []service.AutoModelCapability{capability("second-model")},
	}, "")
	require.Equal(t, http.StatusOK, changed.Code)
	require.Contains(t, changed.Body.String(), `"slug":"second-model"`)
	require.NotEqual(t, firstETag, changed.Header().Get("ETag"))

	empty := httptest.NewRecorder()
	emptyContext, _ := gin.CreateTestContext(empty)
	emptyContext.Request = httptest.NewRequest(http.MethodGet, "/backend-api/codex/models", nil)
	writeAutoCodexModelsManifest(emptyContext, &service.AutoRoutingPreparation{}, "")
	require.Equal(t, http.StatusOK, empty.Code)
	require.JSONEq(t, `{"models":[]}`, empty.Body.String())
}

func TestWriteAutoModelDirectoryKeepsModelDetailShapeForCodexQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models/model?client_version=0.1", nil)
	writeAutoModelDirectory(ctx, &service.AutoRoutingPreparation{
		Capabilities: []service.AutoModelCapability{{ModelID: "model", Platform: service.PlatformOpenAI}},
	}, "model")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"model_id":"model","platform":"openai","account_schedulable":false,"id":"model","object":"model","created":1704067200,"owned_by":"openai","type":"model","display_name":"model"}`, recorder.Body.String())
}

func TestPrepareAutoRoutingSkipsFixedAndLegacyKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{service.AutoRoutingModeFixed, ""} {
		t.Run("mode="+mode, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			key := &service.APIKey{ID: 9, RoutingMode: mode}

			got, handled := PrepareAutoRouting(ctx, key, nil, nil)
			if handled || got != key {
				t.Fatalf("PrepareAutoRouting() = (%p, %t), want (%p, false)", got, handled, key)
			}
			if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
				t.Fatalf("fixed/legacy key wrote response: status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestPrepareAutoRoutingFailsClosedWhenAutoRoutingUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	key := &service.APIKey{
		ID:          9,
		RoutingMode: service.AutoRoutingModeAuto,
		User:        &service.User{ID: 7},
	}

	got, handled := PrepareAutoRouting(ctx, key, nil, nil)
	if !handled || got != key {
		t.Fatalf("PrepareAutoRouting() = (%p, %t), want original key and handled", got, handled)
	}
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "AUTO_ROUTING_UNAVAILABLE") {
		t.Fatalf("response = %d %q, want Auto routing unavailable", recorder.Code, recorder.Body.String())
	}
}

func TestPrepareAutoRoutingRecordsModelBeforeCandidateResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, modelID := range []string{"gemini-4-argon-high", "gpt-6-luna"} {
		t.Run(modelID, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"` + modelID + `","input":"hello"}`)
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			ctx.Request = request.WithContext(context.WithValue(request.Context(), ctxkey.RequestID, "local-id"))
			key := &service.APIKey{
				ID: 9, UserID: 7, RoutingMode: service.AutoRoutingModeAuto,
				User: &service.User{ID: 7},
			}

			got, handled := PrepareAutoRouting(ctx, key, &service.AutoRoutingService{}, nil)

			if !handled || got != key || recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("unavailable routing = (%p, %t, %d), want original key, handled, 503", got, handled, recorder.Code)
			}
			if model := ctx.GetString(service.OpsRequestModelKey); model != modelID {
				t.Fatalf("Ops model = %q, want requested model even when routing fails", model)
			}
			if model, _ := ctx.Request.Context().Value(ctxkey.Model).(string); model != modelID {
				t.Fatalf("request context model = %q, want requested model", model)
			}
			restored, err := io.ReadAll(ctx.Request.Body)
			if err != nil || !bytes.Equal(restored, body) {
				t.Fatalf("restored body = %q, error = %v", restored, err)
			}
		})
	}
}

func TestDispatchAutoRoutingRejectsRepeatedDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	key := &service.APIKey{ID: 9, RoutingMode: service.AutoRoutingModeAuto}
	ctx.Set(autoRoutingStateContextKey, &autoRoutingRequestState{
		authenticatedKey: key,
		preparation:      &service.AutoRoutingPreparation{},
		endpoint:         autoRouteEndpointChat,
		dispatched:       true,
	})

	if DispatchAutoRouting(ctx, key, &service.AutoRoutingService{}) {
		t.Fatal("DispatchAutoRouting() succeeded for repeated dispatch")
	}
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "AUTO_ROUTING_EXECUTION_STATE_CONFLICT") {
		t.Fatalf("response = %d %q, want dispatch conflict", recorder.Code, recorder.Body.String())
	}
}

func TestAutoRoutingDispatchStartedRequiresAdmittedMatchingExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		change func(*gin.Context, *autoRoutingRequestState, *service.APIKey)
		want   bool
	}{
		{name: "account failover reuses admitted claim", want: true},
		{name: "missing preparation", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) { state.preparation = nil }},
		{name: "unclaimed", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) { state.dispatched = false }},
		{name: "admission failed", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) { state.upstreamStarted = false }},
		{name: "finished", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) { state.finished = true }},
		{name: "result already received", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) {
			state.resultReference = "response-1"
		}},
		{name: "different key", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) { key.ID++ }},
		{name: "different user", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) { key.User.ID++ }},
		{name: "different group", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) {
			groupID := int64(20)
			key.GroupID = &groupID
		}},
		{name: "different execution", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) { key.AutoRouting.ExecutionID++ }},
		{name: "different revision", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) {
			key.AutoRouting.RoutingRevision++
		}},
		{name: "different profile", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) { key.AutoRouting.ProfileID++ }},
		{name: "different request", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) {
			key.AutoRouting.RequestID = "other"
		}},
		{name: "different attempt", change: func(_ *gin.Context, _ *autoRoutingRequestState, key *service.APIKey) { key.AutoRouting.AttemptNo++ }},
		{name: "terminal execution", change: func(_ *gin.Context, state *autoRoutingRequestState, _ *service.APIKey) {
			state.preparation.Execution.Status = service.AutoRouteExecutionCompleted
		}},
		{name: "lost request binding", change: func(ctx *gin.Context, _ *autoRoutingRequestState, _ *service.APIKey) {
			ctx.Request = ctx.Request.WithContext(context.Background())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(2)
			key := &service.APIKey{
				ID: 9, UserID: 7, User: &service.User{ID: 7},
				RoutingMode: service.AutoRoutingModeAuto, GroupID: &groupID,
				Group: &service.Group{ID: 2},
				AutoRouting: &service.AutoRoutingMetadata{
					ProfileID: 3, RoutingRevision: 4, ExecutionID: 5, AttemptNo: 1, RequestID: "client:request",
				},
			}
			state := &autoRoutingRequestState{
				dispatched: true, upstreamStarted: true,
				authenticatedKey: &service.APIKey{ID: 9, User: &service.User{ID: 7}},
				preparation: &service.AutoRoutingPreparation{Execution: &service.AutoRoutingExecution{
					ID: 5, APIKeyID: 9, ProfileID: 3, RoutingRevision: 4,
					ActualGroupID: &groupID, CurrentAttempt: 1, RequestID: "client:request",
					Status: service.AutoRouteExecutionDispatching,
				}},
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), effectiveGroupContextKey{}, EffectiveGroupContext{
				APIKeyID: 9, GroupID: 2, ProfileID: 3, RoutingRevision: 4, ExecutionID: 5, AttemptNo: 1, RequestID: "client:request",
			}))
			ctx.Set(autoRoutingStateContextKey, state)
			if test.change != nil {
				test.change(ctx, state, key)
			}
			if got := AutoRoutingDispatchStarted(ctx, key); got != test.want {
				t.Fatalf("AutoRoutingDispatchStarted() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWriteAutoRoutingRequestErrorDoesNotAppendAfterResponseCommitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		path     string
		endpoint string
	}{
		{name: "openai", path: "/v1/chat/completions"},
		{name: "messages", path: "/v1/messages", endpoint: autoRouteEndpointMessages},
		{name: "playground", path: "/api/v1/playground/chat/completions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, test.path, nil)
			_, _ = ctx.Writer.Write([]byte("partial stream"))

			writeAutoRoutingRequestError(ctx, autoRoutingError(http.StatusConflict, "CONFLICT", "conflict"), test.endpoint)

			if got := recorder.Body.String(); got != "partial stream" {
				t.Fatalf("body = %q, want existing stream unchanged", got)
			}
			if !ctx.IsAborted() {
				t.Fatal("context was not aborted after the committed response")
			}
		})
	}
}

func TestExtractStrictJSONModelRejectsAmbiguousFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate top-level model", body: `{"model":"first","model":"second"}`},
		{name: "case variant top-level model", body: `{"model":"first","MODEL":"first"}`},
		{name: "conflicting session model", body: `{"model":"first","session":{"model":"second"}}`},
		{name: "duplicate nested session model", body: `{"session":{"model":"first","model":"first"}}`},
		{name: "case variant nested session model", body: `{"session":{"model":"first","MODEL":"first"}}`},
		{name: "duplicate session objects", body: `{"session":{"model":"first"},"session":{"model":"first"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := extractStrictJSONModel([]byte(test.body))
			if !errors.Is(err, errAutoModelAmbiguous) {
				t.Fatalf("extractStrictJSONModel() error = %v, want ambiguity", err)
			}
		})
	}
}

func TestBuildAutoRoutingRequestRestoresWireBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-6-luna","input":"hello"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.ClientRequestID, " stable-id "))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	input, err := BuildAutoRoutingRequest(ctx)
	if err != nil {
		t.Fatalf("BuildAutoRoutingRequest() error = %v", err)
	}
	if input.ModelID != "gpt-6-luna" || input.RequestID != "client:stable-id" {
		t.Fatalf("input = %+v", input)
	}
	if !bytes.Equal(input.Body, body) {
		t.Fatalf("wire body = %q, want %q", input.Body, body)
	}
	restored, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if !bytes.Equal(restored, body) {
		t.Fatalf("restored body = %q, want %q", restored, body)
	}
}

func TestBuildAutoRoutingRequestUsesLocalBillingIDPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-6-luna"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.RequestID, "local-id"))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	input, err := BuildAutoRoutingRequest(ctx)
	if err != nil {
		t.Fatalf("BuildAutoRoutingRequest() error = %v", err)
	}
	if input.RequestID != "local:local-id" {
		t.Fatalf("RequestID = %q, want local:local-id", input.RequestID)
	}
}

func TestBuildAutoRoutingRequestPreservesBodyLimitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-6-luna"}`))
	request.Body = http.MaxBytesReader(recorder, request.Body, 4)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.ClientRequestID, "request-id"))
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	_, err := BuildAutoRoutingRequest(ctx)
	var requestErr *AutoRoutingRequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("BuildAutoRoutingRequest() error = %v, want AutoRoutingRequestError", err)
	}
	if requestErr.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("error status = %d, want %d", requestErr.Status, http.StatusRequestEntityTooLarge)
	}
}

func TestBuildAutoRoutingRequestRejectsResponsesPreviousResponseID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-6-luna","previous_response_id":"resp_123"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.ClientRequestID, "request-id"))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	_, err := BuildAutoRoutingRequest(ctx)
	var requestErr *AutoRoutingRequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("BuildAutoRoutingRequest() error = %v, want AutoRoutingRequestError", err)
	}
	if requestErr.Code != autoRoutingStatefulResponseCode {
		t.Fatalf("error code = %q, want %q", requestErr.Code, autoRoutingStatefulResponseCode)
	}
}

func TestBindEffectiveGroupContextDoesNotMutateAuthenticatedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticatedGroupID := int64(1)
	effectiveGroupID := int64(2)
	authenticatedGroup := &service.Group{ID: authenticatedGroupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
	effectiveGroup := &service.Group{ID: effectiveGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true}
	authenticatedUser := &service.User{ID: 7, GroupRates: map[int64]float64{1: 1}}
	effectiveUser := *authenticatedUser
	effectiveUser.GroupRates = map[int64]float64{1: 1, 2: 2}
	authenticatedKey := &service.APIKey{
		ID:      9,
		UserID:  authenticatedUser.ID,
		User:    authenticatedUser,
		Group:   authenticatedGroup,
		GroupID: &authenticatedGroupID,
	}
	effectiveKey := *authenticatedKey
	effectiveKey.User = &effectiveUser
	effectiveKey.Group = effectiveGroup
	effectiveKey.GroupID = &effectiveGroupID
	effectiveKey.AutoRouting = &service.AutoRoutingMetadata{
		ProfileID:       3,
		RoutingRevision: 4,
		ExecutionID:     5,
		AttemptNo:       1,
		RequestID:       "client:request-id",
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	if err := BindEffectiveGroupContext(ctx, authenticatedKey, &effectiveKey, nil); err != nil {
		t.Fatalf("BindEffectiveGroupContext() error = %v", err)
	}
	if authenticatedKey.Group != authenticatedGroup || *authenticatedKey.GroupID != authenticatedGroupID {
		t.Fatal("binding changed the authenticated key's fixed group")
	}
	if authenticatedUser.GroupRates[2] != 0 {
		t.Fatal("binding changed the authenticated user's group-rate map")
	}
	group, ok := ctx.Request.Context().Value(ctxkey.Group).(*service.Group)
	if !ok || group != effectiveGroup {
		t.Fatal("request context does not contain the effective group")
	}
	binding, ok := GetEffectiveGroupContext(ctx)
	if !ok || binding.GroupID != effectiveGroupID || binding.ExecutionID != 5 || binding.AttemptNo != 1 {
		t.Fatalf("effective group binding = %+v, present=%t", binding, ok)
	}
	boundKey, ok := GetAPIKeyFromContext(ctx)
	if !ok || boundKey != &effectiveKey {
		t.Fatal("Gin context does not contain the request-local effective key")
	}
}

func TestBindEffectiveGroupContextRejectsSharedGroupPointer(t *testing.T) {
	groupID := int64(1)
	group := &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
	user := &service.User{ID: 7}
	authenticatedKey := &service.APIKey{ID: 9, User: user, Group: group, GroupID: &groupID}
	effectiveKey := *authenticatedKey
	effectiveKey.AutoRouting = &service.AutoRoutingMetadata{
		ProfileID:       3,
		RoutingRevision: 4,
		ExecutionID:     5,
		RequestID:       "client:request-id",
		AttemptNo:       1,
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	if err := BindEffectiveGroupContext(ctx, authenticatedKey, &effectiveKey, nil); err == nil {
		t.Fatal("expected shared group pointer to be rejected")
	}
}
