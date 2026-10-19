package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAutoModelCatalogAdminHandlerEmptyAndInvalidSelections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupRepo := &struct{ service.GroupRepository }{}
	accountRepo := &struct{ service.AccountRepository }{}
	routing := service.NewAutoRoutingService(nil, groupRepo, nil, service.NewAutoModelCatalog(accountRepo))
	router := gin.New()
	router.POST("/catalog", NewAutoRoutingHandler(routing).PreviewModelCatalog)
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"group_ids":[]}`, http.StatusOK},
		{`{"group_ids":[0]}`, http.StatusBadRequest},
		{`{"group_ids":[1,1]}`, http.StatusBadRequest},
		{`{"group_ids":["invalid"]}`, http.StatusBadRequest},
		{`{`, http.StatusBadRequest},
	} {
		t.Run(test.body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/catalog", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code)
			if test.status == http.StatusOK {
				require.JSONEq(t, `{"code":0,"message":"success","data":{"models":[]}}`, recorder.Body.String())
				require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}
