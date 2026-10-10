package routes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const playgroundGroupHeader = "X-Playground-Group-ID"
const playgroundImageGenerationMaxCount = 10
const playgroundImageEditMaxInputs = 4
const playgroundImageEditMaxBytes = 5 << 20

type playgroundImageGenerationRequest struct {
	Count json.RawMessage `json:"n"`
	Model string          `json:"model"`
}

type playgroundImageEditRequest struct {
	Images []struct {
		ImageURL string `json:"image_url"`
	} `json:"images"`
	Mask *struct {
		ImageURL string `json:"image_url"`
	} `json:"mask"`
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

func validatePlaygroundImageEditRequest(c *gin.Context) {
	if !strings.HasPrefix(strings.ToLower(c.ContentType()), "application/json") {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Image edit requests must use JSON")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Failed to read image edit request")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	if hasDuplicatePlaygroundJSONKeys(body) {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Image edit request must not contain duplicate JSON fields")
		return
	}
	var request playgroundImageEditRequest
	if err := json.Unmarshal(body, &request); err != nil {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "Image edit request must be valid JSON")
		return
	}
	if len(request.Images) == 0 || len(request.Images) > playgroundImageEditMaxInputs {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_INPUT_COUNT", "Image edits require between 1 and 4 input images")
		return
	}
	for _, image := range request.Images {
		if !validPlaygroundImageDataURL(image.ImageURL) {
			middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_INPUT", "Each input image must be a local PNG, JPEG, or WebP data URL no larger than 5 MiB")
			return
		}
	}
	if request.Mask != nil && strings.TrimSpace(request.Mask.ImageURL) != "" && !validPlaygroundImageDataURL(request.Mask.ImageURL) {
		middleware.AbortWithError(c, http.StatusBadRequest, "INVALID_IMAGE_MASK", "The mask must be a local PNG, JPEG, or WebP data URL no larger than 5 MiB")
	}
}

func hasDuplicatePlaygroundJSONKeys(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var readValue func() (bool, error)
	readValue = func() (bool, error) {
		token, err := decoder.Token()
		if err != nil {
			return false, err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return false, nil
		}
		switch delim {
		case '{':
			keys := make(map[string]struct{})
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return false, err
				}
				key, ok := token.(string)
				if !ok {
					return false, errors.New("invalid JSON object key")
				}
				if _, exists := keys[key]; exists {
					return true, nil
				}
				keys[key] = struct{}{}
				duplicate, err := readValue()
				if err != nil || duplicate {
					return duplicate, err
				}
			}
			_, err := decoder.Token()
			return false, err
		case '[':
			for decoder.More() {
				duplicate, err := readValue()
				if err != nil || duplicate {
					return duplicate, err
				}
			}
			_, err := decoder.Token()
			return false, err
		default:
			return false, errors.New("invalid JSON delimiter")
		}
	}

	duplicate, err := readValue()
	if err != nil || duplicate {
		return true
	}
	if _, err := decoder.Token(); err != io.EOF {
		return true
	}
	return false
}

func validPlaygroundImageDataURL(value string) bool {
	value = strings.TrimSpace(value)
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(strings.ToLower(header), "data:") {
		return false
	}

	parts := strings.Split(header[len("data:"):], ";")
	if len(parts) != 2 || !strings.EqualFold(parts[1], "base64") {
		return false
	}
	mimeType := strings.ToLower(strings.TrimSpace(parts[0]))
	var signature []byte
	switch mimeType {
	case "image/png":
		signature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	case "image/jpeg":
		signature = []byte{0xff, 0xd8, 0xff}
	case "image/webp":
		signature = []byte("RIFF")
	default:
		return false
	}
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(playgroundImageEditMaxBytes) {
		return false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > playgroundImageEditMaxBytes || len(decoded) < len(signature) {
		return false
	}
	if mimeType == "image/webp" {
		return len(decoded) >= 12 && string(decoded[:4]) == "RIFF" && string(decoded[8:12]) == "WEBP"
	}
	return bytes.HasPrefix(decoded, signature)
}

func playgroundImageEditHandler(openAIImagesHandler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"type":    "not_found_error",
				"message": "Image edits are not supported for Grok",
			}})
			return
		}

		validatePlaygroundImageGenerationCount(c)
		if c.IsAborted() {
			return
		}
		validatePlaygroundImageEditRequest(c)
		if c.IsAborted() {
			return
		}
		if getGroupPlatform(c) == service.PlatformOpenAI {
			openAIImagesHandler(c)
			return
		}

		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
			"type":    "not_found_error",
			"message": "Images API is not supported for this platform",
		}})
	}
}

func playgroundChatCompletionsHandler(openAIHandler, defaultHandler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if domain.UsesOpenAIGateway(getGroupPlatform(c)) {
			openAIHandler(c)
			return
		}
		defaultHandler(c)
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

	playground.GET("/models", h.Gateway.Models)
	playground.POST("/chat/completions", playgroundChatCompletionsHandler(h.OpenAIGateway.ChatCompletions, h.Gateway.ChatCompletions))
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
	playground.POST("/images/edits", playgroundImageEditHandler(h.OpenAIGateway.Images))
}
