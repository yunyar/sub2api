package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveOpsPlatformPrefersResolvedCompositeTarget(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	ctx := service.WithResolvedTargetPlatform(context.Background(), service.PlatformOpenAI)

	require.Equal(t, service.PlatformOpenAI, resolveOpsPlatform(ctx, apiKey, service.PlatformAnthropic))
}

func TestResolveOpsPlatformDoesNotInferUnresolvedAutoTargetFromProtocol(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		apiKey     *service.APIKey
		fallback   string
		wantResult string
	}{
		{
			name:     "unresolved auto Responses request",
			apiKey:   &service.APIKey{RoutingMode: service.AutoRoutingModeAuto},
			fallback: service.PlatformOpenAI,
		},
		{
			name:     "unresolved auto native request",
			apiKey:   &service.APIKey{RoutingMode: service.AutoRoutingModeAuto},
			fallback: service.PlatformGemini,
		},
		{
			name: "resolved auto Antigravity group",
			apiKey: &service.APIKey{
				RoutingMode: service.AutoRoutingModeAuto,
				Group:       &service.Group{Platform: service.PlatformAntigravity},
			},
			fallback:   service.PlatformOpenAI,
			wantResult: service.PlatformAntigravity,
		},
		{
			name: "fixed Antigravity group",
			apiKey: &service.APIKey{
				RoutingMode: service.AutoRoutingModeFixed,
				Group:       &service.Group{Platform: service.PlatformAntigravity},
			},
			fallback:   service.PlatformOpenAI,
			wantResult: service.PlatformAntigravity,
		},
		{
			name:       "request without known key",
			fallback:   service.PlatformOpenAI,
			wantResult: service.PlatformOpenAI,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.wantResult, resolveOpsPlatform(
				context.Background(), testCase.apiKey, testCase.fallback,
			))
		})
	}
}

func TestOpsErrorLoggerMiddlewareAutoCandidateFailureHasNoResolvedPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, modelID := range []string{"gemini-4-argon", "gpt-6-luna"} {
		t.Run(modelID, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(ctx *gin.Context) {
				middleware2.SetOpsFallbackAPIKey(ctx, &service.APIKey{
					ID:          411,
					UserID:      70,
					RoutingMode: service.AutoRoutingModeAuto,
					User:        &service.User{ID: 70},
				})
				ctx.Set(service.OpsRequestModelKey, modelID)
				ctx.JSON(http.StatusNotFound, gin.H{"error": gin.H{
					"code":    "AUTO_ROUTING_CANDIDATE_UNAVAILABLE",
					"message": "no authorized group can serve this model and endpoint",
					"type":    "invalid_request_error",
				}})
			})

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))

			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Contains(t, recorder.Body.String(), "AUTO_ROUTING_CANDIDATE_UNAVAILABLE")
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			job := <-opsErrorLogQueue
			require.Empty(t, job.entry.Platform)
			require.Nil(t, job.entry.GroupID)
			require.Nil(t, job.entry.AccountID)
			require.Equal(t, modelID, job.entry.Model)
			require.Equal(t, modelID, job.entry.RequestedModel)
			require.NotNil(t, job.entry.UserID)
			require.Equal(t, int64(70), *job.entry.UserID)
			require.NotNil(t, job.entry.APIKeyID)
			require.Equal(t, int64(411), *job.entry.APIKeyID)
		})
	}
}
