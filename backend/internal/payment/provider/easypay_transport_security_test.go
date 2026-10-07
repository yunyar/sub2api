package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEasyPayNeverFollowsCredentialBearingRedirects(t *testing.T) {
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		forwarded.Add(1)
		response.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, target.URL, status)
		}))
		gateway := newTestEasyPay(t, source.URL)
		_, err := gateway.QueryOrder(context.Background(), "order-42")
		source.Close()
		if err == nil {
			t.Fatalf("redirect %d must not authorize payment", status)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatalf("forwarded %d requests to redirect destination", forwarded.Load())
	}
}

func TestEasyPayRejectsOversizedGatewayResponse(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat(" ", maxEasypayResponseSize+1)))
	}))
	defer source.Close()
	gateway := newTestEasyPay(t, source.URL)
	if _, err := gateway.QueryOrder(context.Background(), "order-42"); err == nil ||
		!strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("expected oversized response rejection, got %v", err)
	}
}
