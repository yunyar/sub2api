package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBindEffectiveGroupContextConcurrentRequestsKeepRoutingStateIsolated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	parentGroupID := int64(900)
	parentGroup := &service.Group{
		ID: parentGroupID, Platform: service.PlatformOpenAI,
		Status: service.StatusActive, Hydrated: true,
	}
	parentUser := &service.User{ID: 77}
	authenticatedKey := &service.APIKey{
		ID: 41, UserID: parentUser.ID, RoutingMode: service.AutoRoutingModeAuto,
		GroupID: &parentGroupID, Group: parentGroup, User: parentUser,
	}
	requests := []struct {
		modelID   string
		groupID   int64
		platform  string
		execution int64
		requestID string
	}{
		{modelID: "agent-openai", groupID: 101, platform: service.PlatformOpenAI, execution: 501, requestID: "agent:openai"},
		{modelID: "agent-anthropic", groupID: 202, platform: service.PlatformAnthropic, execution: 502, requestID: "agent:anthropic"},
	}

	type result struct {
		request int
		ctx     *gin.Context
		key     *service.APIKey
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, len(requests))
	var wait sync.WaitGroup
	for index, request := range requests {
		index, request := index, request
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			httpRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Request = httpRequest.WithContext(context.WithValue(httpRequest.Context(), ctxkey.Model, request.modelID))

			groupID := request.groupID
			effectiveKey := &service.APIKey{
				ID: authenticatedKey.ID, UserID: authenticatedKey.UserID,
				RoutingMode: service.AutoRoutingModeAuto,
				GroupID:     &groupID,
				Group: &service.Group{
					ID: groupID, Platform: request.platform, Status: service.StatusActive, Hydrated: true,
				},
				User: &service.User{ID: authenticatedKey.UserID},
				AutoRouting: &service.AutoRoutingMetadata{
					ProfileID: 3, RoutingRevision: 4, ExecutionID: request.execution,
					AttemptNo: 1, RequestID: request.requestID,
				},
			}
			err := BindEffectiveGroupContext(ctx, authenticatedKey, effectiveKey, nil)
			results <- result{request: index, ctx: ctx, key: effectiveKey, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	seen := make([]bool, len(requests))
	for result := range results {
		request := requests[result.request]
		require.NoError(t, result.err)
		require.Same(t, result.key, func() *service.APIKey {
			key, ok := GetAPIKeyFromContext(result.ctx)
			require.True(t, ok)
			return key
		}())
		require.Equal(t, request.modelID, result.ctx.Request.Context().Value(ctxkey.Model))
		group, ok := result.ctx.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.True(t, ok)
		require.Same(t, result.key.Group, group)
		require.Equal(t, request.groupID, group.ID)
		require.Equal(t, request.platform, group.Platform)
		binding, ok := GetEffectiveGroupContext(result.ctx)
		require.True(t, ok)
		require.Equal(t, request.groupID, binding.GroupID)
		require.Equal(t, request.execution, binding.ExecutionID)
		require.Equal(t, request.requestID, binding.RequestID)
		seen[result.request] = true
	}

	require.Equal(t, []bool{true, true}, seen)
	require.Same(t, parentGroup, authenticatedKey.Group)
	require.Equal(t, parentGroupID, *authenticatedKey.GroupID)
	require.Equal(t, int64(900), authenticatedKey.Group.ID)
	require.Same(t, parentUser, authenticatedKey.User)
}
