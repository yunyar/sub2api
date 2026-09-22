package routes

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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
