//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGrokPlaygroundImagesRequireBalanceReservationBeforeUpstream(t *testing.T) {
	handler, slots, _, upstream := newGrokMediaSlotHandler(t, false, false)
	handler.gatewayService = &service.OpenAIGatewayService{}
	requestContext, recorder := grokMediaSlotContext(context.Background(), true)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/images/generations", strings.NewReader(`{"model":"grok-imagine-image","prompt":"draw three images","n":3}`))
	requestContext.Request.Header.Set("Content-Type", "application/json")

	handler.GrokImages(requestContext)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "billing_unavailable")
	require.Zero(t, upstream.calls)
	require.Equal(t, slots.userAcquired, slots.userReleased)
}
