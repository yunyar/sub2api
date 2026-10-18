//go:build unit

package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func responsesBridgeSSEPayload(format string) string {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_compat","model":"gemini-test","role":"assistant","content":[],"usage":{"input_tokens":12}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	}
	eventTypes := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	var payload strings.Builder
	for index, event := range events {
		switch format {
		case "data_only":
			payload.WriteString("data: " + event + "\n\n")
		case "metadata":
			payload.WriteString("event: " + eventTypes[index] + "\nid: compat\n: heartbeat\nretry: 1000\ndata: " + event + "\n\n")
		case "multiline":
			event = strings.Replace(event, ",", ",\ndata: ", 1)
			payload.WriteString("event: " + eventTypes[index] + "\ndata: " + event + "\n\n")
		case "event_type":
			event = strings.Replace(event, `"type":"`+eventTypes[index]+`",`, "", 1)
			event = strings.Replace(event, `"type":"`+eventTypes[index]+`"`, "", 1)
			payload.WriteString("event: " + eventTypes[index] + "\ndata: " + event + "\n\n")
		case "crlf":
			payload.WriteString("event: " + eventTypes[index] + "\r\ndata: " + event + "\r\n\r\n")
		default:
			payload.WriteString("event: " + eventTypes[index] + "\ndata: " + event + "\n\n")
		}
	}
	return payload.String()
}

func runResponsesBridgeSSETest(
	service *GatewayService,
	resp *http.Response,
	stream bool,
) (*ForwardResult, *httptest.ResponseRecorder, error) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	var result *ForwardResult
	var err error
	if stream {
		result, err = service.handleResponsesStreamingResponse(resp, ginContext, "gemini-test", "gemini-test", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	} else {
		result, err = service.handleResponsesBufferedStreamingResponse(resp, ginContext, "gemini-test", "gemini-test", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	}
	return result, recorder, err
}

func TestResponsesBridgeAcceptsCompleteSSEFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, format := range []string{"standard", "data_only", "metadata", "multiline", "event_type", "crlf"} {
		for _, stream := range []bool{false, true} {
			mode := "buffered"
			if stream {
				mode = "stream"
			}
			t.Run(format+"/"+mode, func(t *testing.T) {
				resp := &http.Response{Body: io.NopCloser(strings.NewReader(responsesBridgeSSEPayload(format)))}
				result, recorder, err := runResponsesBridgeSSETest(&GatewayService{}, resp, stream)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 12, result.Usage.InputTokens)
				require.Equal(t, 7, result.Usage.OutputTokens)
				require.Contains(t, recorder.Body.String(), "hello")
				if stream {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed\n"))
				} else {
					require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
				}
			})
		}
	}
}

func TestForwardAsResponsesAntigravityAPIKeyUsesMessagesBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		mode := "buffered"
		if stream {
			mode = "stream"
		}
		t.Run(mode, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			body := `{"model":"gemini-test","input":"hello","stream":` + strconv.FormatBool(stream) + `}`
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(responsesBridgeSSEPayload("data_only"))),
			}}
			service := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{
				ID: 146, Platform: PlatformAntigravity, Type: AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key": "fixture-key", "base_url": "https://antigravity.example.test",
					"model_mapping": map[string]any{"gemini-test": "gemini-test-high"},
				},
			}
			result, err := service.ForwardAsResponses(ginContext.Request.Context(), ginContext, account, []byte(body), nil)
			require.NoError(t, err)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "/antigravity/v1/messages", upstream.lastReq.URL.Path)
			require.Contains(t, string(upstream.lastBody), `"model":"gemini-test-high"`)
			require.Contains(t, string(upstream.lastBody), `"stream":true`)
			require.Equal(t, "gemini-test", result.Model)
			require.Equal(t, "gemini-test-high", result.UpstreamModel)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), "hello")
		})
	}
}

func TestResponsesBridgeReturnsAtMessageStopBeforeUpstreamEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		mode := "buffered"
		if stream {
			mode = "stream"
		}
		t.Run(mode, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			body := struct {
				io.Reader
				io.Closer
			}{Reader: io.MultiReader(strings.NewReader(responsesBridgeSSEPayload("standard")), reader), Closer: reader}
			resp := &http.Response{Body: body}
			done := make(chan struct{})
			var result *ForwardResult
			var err error
			go func() {
				result, _, err = runResponsesBridgeSSETest(&GatewayService{}, resp, stream)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				_ = writer.Close()
				<-done
				t.Fatal("message_stop must finish the response without waiting for upstream EOF")
			}
			require.NoError(t, err)
			require.Equal(t, 7, result.Usage.OutputTokens)
			writeResult := make(chan error, 1)
			go func() {
				_, writeErr := writer.Write([]byte(": heartbeat\n\n"))
				writeResult <- writeErr
			}()
			select {
			case writeErr := <-writeResult:
				require.ErrorIs(t, writeErr, io.ErrClosedPipe)
			case <-time.After(time.Second):
				_ = reader.Close()
				<-writeResult
				t.Fatal("finishing the response must close the upstream body")
			}
		})
	}
}

func TestResponsesBridgeRejectsUpstreamErrorEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"No available accounts\"}}\n\n",
		))}
		_, recorder, err := runResponsesBridgeSSETest(&GatewayService{}, resp, stream)
		require.ErrorContains(t, err, "No available accounts")
		require.NotContains(t, recorder.Body.String(), "response.completed")
		if stream {
			require.Contains(t, recorder.Body.String(), "response.failed")
		} else {
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		}
	}
}

func TestResponsesBridgeReportsInterruptedStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		reader, writer := io.Pipe()
		go func() {
			_, _ = io.WriteString(writer, `event: message_start
data: {"type":"message_start","message":{"id":"msg_partial","content":[],"usage":{"input_tokens":12}}}

`)
			_ = writer.CloseWithError(io.ErrUnexpectedEOF)
		}()
		resp := &http.Response{Body: reader}
		result, recorder, err := runResponsesBridgeSSETest(&GatewayService{}, resp, stream)
		require.Error(t, err)
		require.NotContains(t, recorder.Body.String(), "response.completed")
		if stream {
			require.NotNil(t, result)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Contains(t, recorder.Body.String(), "response.failed")
		} else {
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		}
	}
}

func TestResponsesBridgeRejectsEOFWithoutTerminalEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, payload := range []string{
			"",
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_partial\",\"content\":[],\"usage\":{\"input_tokens\":12}}}\n\n",
		} {
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
			result, recorder, err := runResponsesBridgeSSETest(&GatewayService{}, resp, stream)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			require.NotContains(t, recorder.Body.String(), "response.completed")
			if stream {
				require.NotNil(t, result)
				require.Contains(t, recorder.Body.String(), "response.failed")
			} else {
				require.Equal(t, http.StatusBadGateway, recorder.Code)
			}
		}
	}
}

func TestResponsesBridgeTimesOutIdleUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		reader, writer := io.Pipe()
		defer writer.Close()
		service := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}}}
		result, recorder, err := runResponsesBridgeSSETest(service, &http.Response{Body: reader}, stream)
		require.ErrorIs(t, err, errAnthropicNativeStreamIdle)
		require.NotContains(t, recorder.Body.String(), "response.completed")
		_, writeErr := writer.Write([]byte("data: ignored\n\n"))
		require.True(t, errors.Is(writeErr, io.ErrClosedPipe))
		if stream {
			require.NotNil(t, result)
			require.Contains(t, recorder.Body.String(), "response.failed")
		} else {
			require.Equal(t, http.StatusBadGateway, recorder.Code)
		}
	}
}
