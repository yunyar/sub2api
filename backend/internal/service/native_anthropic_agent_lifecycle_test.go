//go:build unit

package service

import (
	"encoding/json"
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

type nativeAnthropicResponsesLifecycleOutcome struct {
	result *OpenAIForwardResult
	err    error
}

type nativeAnthropicResponsesLifecycleEvent struct {
	Type     string `json:"type"`
	Response struct {
		Status string `json:"status"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

func nativeAnthropicResponsesPartialEOFStream() string {
	return strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10,"output_tokens":1}}}`,
		"",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial answer"}}`,
		"",
		`data: {"type":"message_delta","usage":{"output_tokens":5}}`,
		"",
		"",
	}, "\n")
}

func nativeAnthropicResponsesCompleteStream() string {
	return strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_complete","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"complete answer"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
		"",
	}, "\n")
}

func nativeAnthropicResponsesTerminalEvents(body string) []nativeAnthropicResponsesLifecycleEvent {
	var events []nativeAnthropicResponsesLifecycleEvent
	for _, line := range strings.Split(body, "\n") {
		payload, ok := extractOpenAISSEDataLine(line)
		if !ok {
			continue
		}
		var event nativeAnthropicResponsesLifecycleEvent
		if json.Unmarshal([]byte(payload), &event) == nil {
			switch event.Type {
			case "response.completed", "response.incomplete", "response.failed":
				events = append(events, event)
			}
		}
	}
	return events
}

func TestResponsesStreamingFromNativeAnthropic_DataOnlyMultilineSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newNativeAnthropicHangTestService(2)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	stream := strings.Join([]string{
		"data: {",
		`data: "type":"message_start",`,
		`data: "message":{"id":"msg_multiline","type":"message","role":"assistant",`,
		`data: "content":[],"model":"glm-4.7","usage":{"input_tokens":11}}`,
		"data: }",
		"",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"multiline works"}}`,
		"",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(stream)),
		Header:     http.Header{},
	}

	result, err := svc.handleResponsesStreamingFromNativeAnthropic(
		resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), "multiline works")

	terminals := nativeAnthropicResponsesTerminalEvents(rec.Body.String())
	require.Len(t, terminals, 1)
	require.Equal(t, "response.completed", terminals[0].Type)
	require.Equal(t, "completed", terminals[0].Response.Status)
}

func TestResponsesStreamingFromNativeAnthropic_PartialEOFFailsAndReturnsPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newNativeAnthropicHangTestService(2)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(nativeAnthropicResponsesPartialEOFStream())),
		Header:     http.Header{},
	}

	result, err := svc.handleResponsesStreamingFromNativeAnthropic(
		resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)

	terminals := nativeAnthropicResponsesTerminalEvents(rec.Body.String())
	require.Len(t, terminals, 1)
	require.Equal(t, "response.failed", terminals[0].Type)
	require.Equal(t, "failed", terminals[0].Response.Status)
	require.Equal(t, 10, terminals[0].Response.Usage.InputTokens)
	require.Equal(t, 5, terminals[0].Response.Usage.OutputTokens)
	require.NotContains(t, rec.Body.String(), "response.completed")
}

func TestResponsesBufferedFromNativeAnthropic_PartialEOFReturns502AndPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newNativeAnthropicHangTestService(2)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(nativeAnthropicResponsesPartialEOFStream())),
		Header:     http.Header{},
	}

	result, err := svc.handleResponsesBufferedFromNativeAnthropic(
		resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream stream failed before completion")
}

func TestResponsesStreamingFromNativeAnthropic_MessageStopReturnsWithConnectionOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newNativeAnthropicHangTestService(1)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp, pipeReader, pipeWriter := newHangingUpstreamResponse()
	defer func() {
		_ = pipeWriter.Close()
		_ = pipeReader.Close()
	}()

	outcomeCh := make(chan nativeAnthropicResponsesLifecycleOutcome, 1)
	go func() {
		result, err := svc.handleResponsesStreamingFromNativeAnthropic(
			resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(),
			apicompat.ResponsesClientToolMapping{},
		)
		outcomeCh <- nativeAnthropicResponsesLifecycleOutcome{result: result, err: err}
	}()

	_, writeErr := io.WriteString(pipeWriter, nativeAnthropicResponsesCompleteStream())
	require.NoError(t, writeErr)

	select {
	case outcome := <-outcomeCh:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, 5, outcome.result.Usage.OutputTokens)
		terminals := nativeAnthropicResponsesTerminalEvents(rec.Body.String())
		require.Len(t, terminals, 1)
		require.Equal(t, "response.completed", terminals[0].Type)
	case <-time.After(300 * time.Millisecond):
		_ = pipeWriter.Close()
		select {
		case <-outcomeCh:
		case <-time.After(2 * time.Second):
			t.Fatal("handler remained blocked after upstream body was released")
		}
		t.Fatal("handler waited for EOF after receiving message_stop")
	}
}
