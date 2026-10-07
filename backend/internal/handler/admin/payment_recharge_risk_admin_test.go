package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPaymentRiskMutationsRequireExplicitConfirmation(t *testing.T) {
	handler := NewPaymentHandler(nil, nil)
	for _, test := range []struct {
		name   string
		body   string
		handle gin.HandlerFunc
	}{
		{"block", `{"order_id":42,"reason":"reviewed"}`, handler.BlockRiskOrderIP},
		{"unblock", `{"ip":"1.1.1.1","reason":"reviewed"}`, handler.UnblockRiskIP},
		{"scan", `{}`, handler.ScanRiskAccounts},
		{"invalid order", `{"order_id":0,"reason":"reviewed","confirm":true}`, handler.BlockRiskOrderIP},
		{"empty reason", `{"order_id":42,"reason":" ","confirm":true}`, handler.BlockRiskOrderIP},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/risk-ips", strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			test.handle(ctx)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestPaymentRiskBlockAndReleaseRequireAuthenticatedActor(t *testing.T) {
	handler := NewPaymentHandler(nil, nil)
	for _, test := range []struct {
		body   string
		handle gin.HandlerFunc
	}{
		{`{"order_id":42,"reason":"reviewed","confirm":true}`, handler.BlockRiskOrderIP},
		{`{"ip":"1.1.1.1","reason":"reviewed","confirm":true}`, handler.UnblockRiskIP},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/risk-ips", strings.NewReader(test.body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		test.handle(ctx)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
}

func TestPaymentRiskListReportsUnavailableService(t *testing.T) {
	handler := NewPaymentHandler(nil, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/risk-ips", nil)
	handler.ListRiskIPs(ctx)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
