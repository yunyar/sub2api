package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const playgroundGroupHeader = "X-Playground-Group-ID"
const playgroundImageGenerationMaxCount = 10

type playgroundImageGenerationRequest struct {
	Count json.RawMessage `json:"n"`
	Model string          `json:"model"`
}

func validatePlaygroundImageGenerationCount(c *gin.Context) {
	if !strings.HasPrefix(strings.ToLower(c.ContentType()), "application/json") {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_COUNT", "Image generation requests must use JSON with an integer n between 1 and 10")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Failed to read image generation request")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var request playgroundImageGenerationRequest
	if err := json.Unmarshal(body, &request); err != nil {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Image generation request must be valid JSON")
		return
	}
	if len(request.Count) == 0 || string(request.Count) == "null" {
		return
	}

	var count int
	if err := json.Unmarshal(request.Count, &count); err != nil || count < 1 || count > playgroundImageGenerationMaxCount {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_COUNT", "n must be an integer between 1 and 10")
		return
	}
	if count > 1 && strings.EqualFold(strings.TrimSpace(request.Model), "dall-e-3") {
		middleware.AbortWithError(c, http.StatusBadRequest, "UNSUPPORTED_IMAGE_COUNT", "n greater than 1 is not supported for dall-e-3")
	}
}

func playgroundAPIKeyBridge(apiKeyService *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			middleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required")
			return
		}

		groupID, err := strconv.ParseInt(strings.TrimSpace(c.GetHeader(playgroundGroupHeader)), 10, 64)
		if err != nil || groupID <= 0 {
			middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_GROUP_ID", "A valid playground group is required")
			return
		}

		apiKey, err := apiKeyService.ResolvePlaygroundKey(c.Request.Context(), subject.UserID, groupID)
		if err != nil {
			if errors.Is(err, service.ErrGroupNotAllowed) {
				middleware.AbortWithError(c, http.StatusForbidden, "GROUP_NOT_ALLOWED", "The selected group is not available")
				return
			}
			middleware.AbortWithError(c, http.StatusInternalServerError, "PLAYGROUND_KEY_ERROR", "Failed to prepare playground access")
			return
		}

		c.Request.Header.Del(playgroundGroupHeader)
		c.Request.Header.Del("x-api-key")
		c.Request.Header.Del("x-goog-api-key")
		c.Request.Header.Set("Authorization", "Bearer "+apiKey.Key)
		c.Next()
	}
}

// RegisterPlaygroundRoutes exposes selected gateway operations to authenticated
// panel users without revealing API key credentials to the browser.
func RegisterPlaygroundRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
) {
	playground := v1.Group("/playground")
	playground.Use(middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize))
	playground.Use(middleware.ClientRequestID())
	playground.Use(handler.OpsErrorLoggerMiddleware(opsService))
	playground.Use(handler.InboundEndpointMiddleware())
	playground.Use(gin.HandlerFunc(jwtAuth))
	playground.Use(playgroundAPIKeyBridge(apiKeyService))
	playground.Use(gin.HandlerFunc(apiKeyAuth))
	playground.Use(middleware.GroupModelAllowlist())
	playground.Use(compositeTargetPlatformMiddleware(compositeResolver))
	playground.Use(middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter))

	isOpenAICompatible := func(c *gin.Context) bool {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI, service.PlatformGrok, service.PlatformKimi,
			service.PlatformZhipu, service.PlatformDeepseek, service.PlatformMiniMax,
			service.PlatformOpenCodeGo:
			return true
		default:
			return false
		}
	}

	playground.GET("/models", h.Gateway.Models)
	playground.POST("/chat/completions", func(c *gin.Context) {
		if isOpenAICompatible(c) {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	playground.POST("/images/generations", func(c *gin.Context) {
		validatePlaygroundImageGenerationCount(c)
		if c.IsAborted() {
			return
		}
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.Images(c)
		case service.PlatformGrok:
			h.OpenAIGateway.GrokImages(c)
		default:
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"type":    "not_found_error",
				"message": "Images API is not supported for this platform",
			}})
		}
	})
}
