package routes

import (
	"net/http"
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
