package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingUsageDoesNotRequireGenerationGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/v1/usage", http.StatusNoContent},
		{http.MethodPost, "/v1/usage", http.StatusForbidden},
		{http.MethodPost, "/v1/chat/completions", http.StatusForbidden},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 1, RoutingMode: service.AutoRoutingModeAuto})
			})
			router.Use(RequireGroupAssignment(nil, AnthropicErrorWriter))
			router.Handle(test.method, test.path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			require.Equal(t, test.status, response.Code)
		})
	}
}
