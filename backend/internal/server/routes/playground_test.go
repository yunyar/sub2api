package routes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	noop := func(c *gin.Context) { c.Next() }

	RegisterPlaygroundRoutes(
		v1,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.JWTAuthMiddleware(noop),
		servermiddleware.APIKeyAuthMiddleware(noop),
		nil,
		nil,
		nil,
		nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024}},
	)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	require.True(t, registered[http.MethodGet+" /api/v1/playground/models"])
	require.True(t, registered[http.MethodPost+" /api/v1/playground/chat/completions"])
	require.True(t, registered[http.MethodPost+" /api/v1/playground/images/generations"])
	require.True(t, registered[http.MethodPost+" /api/v1/playground/images/edits"])
}

func TestValidatePlaygroundImageEditRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	jpeg := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff})
	webp := "data:image/webp;base64," + base64.StdEncoding.EncodeToString([]byte("RIFF0000WEBP"))
	maxSizePNG := make([]byte, playgroundImageEditMaxBytes)
	copy(maxSizePNG, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	maxSizeDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(maxSizePNG)
	oversizedPNG := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, playgroundImageEditMaxBytes-7)...)
	oversized := "data:image/png;base64," + base64.StdEncoding.EncodeToString(oversizedPNG)
	fourImages, err := json.Marshal(map[string]any{"images": []map[string]string{
		{"image_url": png}, {"image_url": jpeg}, {"image_url": webp}, {"image_url": png},
	}})
	require.NoError(t, err)
	fiveImages, err := json.Marshal(map[string]any{"images": []map[string]string{
		{"image_url": png}, {"image_url": png}, {"image_url": png}, {"image_url": png}, {"image_url": png},
	}})
	require.NoError(t, err)

	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "accepts png jpeg and webp", body: `{"images":[{"image_url":"` + png + `"},{"image_url":"` + jpeg + `"},{"image_url":"` + webp + `"}]}`, contentType: "application/json", wantStatus: http.StatusOK},
		{name: "accepts maximum image size", body: `{"images":[{"image_url":"` + maxSizeDataURL + `"}]}`, contentType: "application/json", wantStatus: http.StatusOK},
		{name: "accepts four inputs", body: string(fourImages), contentType: "application/json", wantStatus: http.StatusOK},
		{name: "rejects missing inputs", body: `{"images":[]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects more than four inputs", body: string(fiveImages), contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects remote input", body: `{"images":[{"image_url":"https://example.com/image.png"}]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects svg data url", body: `{"images":[{"image_url":"data:image/svg+xml;base64,PHN2Zz4="}]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects mismatched magic", body: `{"images":[{"image_url":"data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("not png")) + `"}]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects oversized input", body: `{"images":[{"image_url":"` + oversized + `"}]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects remote mask", body: `{"images":[{"image_url":"` + png + `"}],"mask":{"image_url":"https://example.com/mask.png"}}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects duplicate input fields", body: `{"images":[{"image_url":"` + png + `","image_url":"https://example.com/image.png"}]}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejects non JSON", body: "images=local", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/edits", strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", test.contentType)

			validatePlaygroundImageEditRequest(ctx)

			if test.wantStatus == http.StatusOK {
				restored, err := io.ReadAll(ctx.Request.Body)
				require.NoError(t, err)
				require.Equal(t, test.body, string(restored))
				return
			}
			require.True(t, ctx.IsAborted())
			require.Equal(t, test.wantStatus, writer.Code)
		})
	}
}

func TestPlaygroundImageEditEndpointParsesAsOpenAIEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-1","images":[{"image_url":"data:image/png;base64,iVBORw0KGgo="}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/edits", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = request

	parsed, err := (&service.OpenAIGatewayService{}).ParseOpenAIImagesRequest(ctx, body)

	require.NoError(t, err)
	require.True(t, parsed.IsEdits())
	require.Equal(t, "/v1/images/edits", parsed.Endpoint)
	require.Equal(t, []string{"data:image/png;base64,iVBORw0KGgo="}, parsed.InputImageURLs)
}

func TestPlaygroundImageEditHandlerDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-image-1","images":[{"image_url":"data:image/png;base64,iVBORw0KGgo="}]}`

	newContext := func(platform string) (*gin.Context, *httptest.ResponseRecorder) {
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/edits", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			Group: &service.Group{Platform: platform},
		})
		return ctx, writer
	}

	openAIReached := false
	ctx, writer := newContext(service.PlatformOpenAI)
	playgroundImageEditHandler(func(c *gin.Context) {
		openAIReached = true
		require.Equal(t, "/api/v1/playground/images/edits", c.Request.URL.Path)
		restored, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(restored))
		c.Status(http.StatusNoContent)
	})(ctx)
	ctx.Writer.WriteHeaderNow()
	require.True(t, openAIReached)
	require.Equal(t, http.StatusNoContent, writer.Code)

	ctx, writer = newContext(service.PlatformGrok)
	playgroundImageEditHandler(func(*gin.Context) {
		t.Fatal("Grok image edits must not reach the image handler")
	})(ctx)
	require.Equal(t, http.StatusNotFound, writer.Code)
	require.Contains(t, writer.Body.String(), "Image edits are not supported for Grok")

	ctx, writer = newContext(service.PlatformOpenAI)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/edits", strings.NewReader(strings.Replace(body, `"images":`, `"n":11,"images":`, 1)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	playgroundImageEditHandler(func(*gin.Context) {
		t.Fatal("out-of-range image count must not reach the image handler")
	})(ctx)
	require.Equal(t, http.StatusBadRequest, writer.Code)
}

func TestPlaygroundChatCompletionsHandlerUsesOpenAIGatewayPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		platform string
		want     string
	}{
		{platform: service.PlatformOpenAI, want: "openai"},
		{platform: service.PlatformGrok, want: "openai"},
		{platform: service.PlatformCline, want: "openai"},
		{platform: service.PlatformCommandCode, want: "openai"},
		{platform: service.PlatformAnthropic, want: "default"},
	}

	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/chat/completions", nil)
			ctx.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				Group: &service.Group{Platform: test.platform},
			})

			got := ""
			playgroundChatCompletionsHandler(
				func(c *gin.Context) {
					got = "openai"
					c.Status(http.StatusNoContent)
				},
				func(c *gin.Context) {
					got = "default"
					c.Status(http.StatusNoContent)
				},
			)(ctx)

			ctx.Writer.WriteHeaderNow()
			require.Equal(t, test.want, got)
			require.Equal(t, http.StatusNoContent, writer.Code)
		})
	}
}

func TestValidatePlaygroundImageGenerationCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{name: "defaults to one", body: `{"model":"gpt-image-1"}`, contentType: "application/json", status: http.StatusOK},
		{name: "allows maximum", body: `{"n":10}`, contentType: "application/json", status: http.StatusOK},
		{name: "rejects zero", body: `{"n":0}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "rejects concurrent amplification", body: `{"n":11}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "rejects fractional count", body: `{"n":1.5}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "rejects dall e three multiple images", body: `{"model":"dall-e-3","n":2}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "rejects non JSON bypass", body: `n=11`, contentType: "application/x-www-form-urlencoded", status: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/generations", bytes.NewBufferString(test.body))
			ctx.Request.Header.Set("Content-Type", test.contentType)

			validatePlaygroundImageGenerationCount(ctx)

			if test.status == http.StatusOK {
				restored, err := io.ReadAll(ctx.Request.Body)
				require.NoError(t, err)
				require.Equal(t, test.body, string(restored))
				return
			}
			require.True(t, ctx.IsAborted())
			require.Equal(t, test.status, writer.Code)
		})
	}
}
