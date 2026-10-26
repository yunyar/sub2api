package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialPricingDocumentRestrictsRedirectsAndResponseSize(t *testing.T) {
	var targetContacted bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetContacted = true
		_, _ = writer.Write([]byte("unexpected target"))
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/prices":
			_, _ = writer.Write([]byte("official prices"))
		case "/same-origin":
			http.Redirect(writer, request, "/prices", http.StatusFound)
		case "/other-origin":
			http.Redirect(writer, request, target.URL, http.StatusFound)
		case "/large":
			_, _ = writer.Write([]byte(strings.Repeat("x", (8<<20)+1)))
		default:
			writer.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	client := &pricingRemoteClient{httpClient: server.Client()}
	body, err := client.FetchOfficialPricingDocument(context.Background(), server.URL+"/same-origin")
	require.NoError(t, err)
	require.Equal(t, "official prices", string(body))
	_, err = client.FetchOfficialPricingDocument(context.Background(), server.URL+"/other-origin")
	require.ErrorContains(t, err, "outside source origin")
	require.False(t, targetContacted)
	_, err = client.FetchOfficialPricingDocument(context.Background(), server.URL+"/large")
	require.ErrorContains(t, err, "exceeds 8 MiB")
	_, err = client.FetchOfficialPricingDocument(context.Background(), server.URL+"/forbidden")
	require.ErrorContains(t, err, "HTTP 403")
	_, err = client.FetchOfficialPricingDocument(context.Background(), "http://example.com/prices")
	require.ErrorContains(t, err, "HTTPS")
}
