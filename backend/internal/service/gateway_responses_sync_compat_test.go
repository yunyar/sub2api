//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesSyncCompatSSEFrames(t *testing.T) {
	for _, variant := range []string{"multiline", "data_only"} {
		t.Run(variant, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			stream := anthropicStreamWithMidStreamText()
			if variant == "multiline" {
				stream = strings.ReplaceAll(stream, `,"message":`, ",\ndata: \"message\":")
			} else {
				var lines []string
				for _, line := range strings.Split(stream, "\n") {
					if !strings.HasPrefix(line, "event:") {
						lines = append(lines, line)
					}
				}
				stream = strings.Join(lines, "\n")
			}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			response := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
			result, err := (&GatewayService{}).handleResponsesStreamingResponse(response, requestContext, "claude-sonnet-4-6", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 42, result.Usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), "response.completed")
			require.NotContains(t, recorder.Body.String(), "response.failed")
		})
	}
}

func TestResponsesSyncCompatMalformedFramePreservesPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := anthropicStreamTruncatedBeforeMessageDelta() + "event: content_block_delta\ndata: {invalid-json}\n\n"
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	response := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
	result, err := (&GatewayService{}).handleResponsesStreamingResponse(response, requestContext, "claude-sonnet-4-6", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	require.ErrorContains(t, err, "parse upstream anthropic event")
	require.NotNil(t, result)
	require.True(t, result.Usage.hasObservedTokens())
	require.Contains(t, recorder.Body.String(), "response.failed")
	require.NotContains(t, recorder.Body.String(), "response.completed")
}
