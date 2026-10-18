//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type agentSubrequestHeldOpenBody struct {
	payload   []byte
	sent      bool
	firstRead chan struct{}
	closed    chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
}

func (body *agentSubrequestHeldOpenBody) Read(buffer []byte) (int, error) {
	if !body.sent {
		body.sent = true
		if body.firstRead != nil {
			body.readOnce.Do(func() { close(body.firstRead) })
		}
		return copy(buffer, body.payload), nil
	}
	<-body.closed
	return 0, io.EOF
}

func (body *agentSubrequestHeldOpenBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}

type agentSubrequestDelayedWriteResponseWriter struct {
	gin.ResponseWriter
	delay time.Duration
	once  sync.Once
}

type agentSubrequestFailWriteResponseWriter struct {
	gin.ResponseWriter
}

type agentSubrequestContinuousUsageBody struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func (writer *agentSubrequestDelayedWriteResponseWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { time.Sleep(writer.delay) })
	return writer.ResponseWriter.Write(data)
}

func (writer *agentSubrequestDelayedWriteResponseWriter) WriteString(data string) (int, error) {
	return writer.Write([]byte(data))
}

func (writer *agentSubrequestFailWriteResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (writer *agentSubrequestFailWriteResponseWriter) WriteString(string) (int, error) {
	return 0, errors.New("client disconnected")
}

func (body *agentSubrequestContinuousUsageBody) Read(buffer []byte) (int, error) {
	select {
	case <-body.closed:
		return 0, io.EOF
	case <-time.After(5 * time.Millisecond):
	}
	return copy(buffer, []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\",\"usage\":{\"input_tokens\":9,\"output_tokens\":3,\"total_tokens\":12}}\n\n")), nil
}

func (body *agentSubrequestContinuousUsageBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}

func TestAgentSubrequestHTTPCompletedToolCallDoesNotWaitForUpstreamEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.output_item.added","sequence_number":0,"output_index":0,"item":{"id":"fc_agent_1","type":"function_call","call_id":"call_agent_1","name":"lookup","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","sequence_number":1,"output_index":0,"item_id":"fc_agent_1","call_id":"call_agent_1","delta":"{\"query\":\"beta\"}"}`,
		`data: {"type":"response.function_call_arguments.done","sequence_number":2,"output_index":0,"item_id":"fc_agent_1","call_id":"call_agent_1","name":"lookup","arguments":"{\"query\":\"beta\"}"}`,
		`data: {"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":{"id":"fc_agent_1","type":"function_call","call_id":"call_agent_1","name":"lookup","arguments":"{\"query\":\"beta\"}","status":"completed"}}`,
		`data: {"type":"response.completed","sequence_number":4,"response":{"id":"resp_agent_subrequest","object":"response","created_at":1700000000,"model":"gpt-5.2","status":"completed","output":[{"id":"fc_agent_1","type":"function_call","call_id":"call_agent_1","name":"lookup","arguments":"{\"query\":\"beta\"}","status":"completed"}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`,
		"",
	}, "\n\n")

	for _, test := range []struct {
		name        string
		passthrough bool
	}{
		{name: "native Responses"},
		{name: "passthrough Responses", passthrough: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestBody := agentMessageIntegrationResponsesBody("gpt-5.2", true)
			c, recorder := agentMessageIntegrationContext("/v1/responses", requestBody)
			upstreamBody := &agentSubrequestHeldOpenBody{
				payload:   []byte(upstreamSSE),
				firstRead: make(chan struct{}),
				closed:    make(chan struct{}),
			}
			t.Cleanup(func() { _ = upstreamBody.Close() })
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       upstreamBody,
			}}
			svc := &OpenAIGatewayService{
				cfg:           &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: false}},
				httpUpstream:  upstream,
				toolCorrector: NewCodexToolCorrector(),
			}

			type forwardOutcome struct {
				result *OpenAIForwardResult
				err    error
			}
			outcomeCh := make(chan forwardOutcome, 1)
			go func() {
				result, err := svc.Forward(
					context.Background(),
					c,
					agentMessageIntegrationOpenAIOAuthAccount(test.passthrough),
					requestBody,
				)
				outcomeCh <- forwardOutcome{result: result, err: err}
			}()

			var outcome forwardOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(2 * time.Second):
				_ = upstreamBody.Close()
				select {
				case <-outcomeCh:
				case <-time.After(time.Second):
					t.Fatal("forward remained blocked after releasing the held-open upstream body")
				}
				t.Fatal("completed tool-call response waited for upstream EOF")
			}

			require.NoError(t, outcome.err)
			require.NotNil(t, outcome.result)
			require.NotNil(t, outcome.result.Usage)
			require.Equal(t, 10, outcome.result.Usage.InputTokens)
			require.Equal(t, 3, outcome.result.Usage.OutputTokens)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "message", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
			require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "input.0.role").String())
			require.Equal(t, "Compare the image with the attached brief.", gjson.GetBytes(upstream.lastBody, "input.0.content.1.text").String())
			require.Contains(t, recorder.Body.String(), "response.function_call_arguments.done")
			require.Contains(t, recorder.Body.String(), "response.completed")
			select {
			case <-upstreamBody.closed:
			default:
				t.Fatal("forward returned without closing the upstream response body")
			}

		})
	}
}

func TestAgentSubrequestPassthroughMissingTerminalTimesOutWithUpstreamHeldOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.output_item.added","sequence_number":0,"output_index":0,"item":{"id":"fc_agent_1","type":"function_call","call_id":"call_agent_1","name":"lookup","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.done","sequence_number":1,"output_index":0,"item_id":"fc_agent_1","call_id":"call_agent_1","name":"lookup","arguments":"{\"query\":\"beta\"}","usage":{"input_tokens":6,"output_tokens":2,"total_tokens":8}}`,
		`data: {"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":{"id":"fc_agent_1","type":"function_call","call_id":"call_agent_1","name":"lookup","arguments":"{\"query\":\"beta\"}","status":"completed"}}`,
		"",
	}, "\n\n")

	requestBody := agentMessageIntegrationResponsesBody("gpt-5.2", true)
	c, recorder := agentMessageIntegrationContext("/v1/responses", requestBody)
	upstreamBody := &agentSubrequestHeldOpenBody{
		payload:   []byte(upstreamSSE),
		firstRead: make(chan struct{}),
		closed:    make(chan struct{}),
	}
	t.Cleanup(func() { _ = upstreamBody.Close() })
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       upstreamBody,
	}}
	svc := &OpenAIGatewayService{
		cfg:           &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: false, StreamDataIntervalTimeout: 1}},
		httpUpstream:  upstream,
		toolCorrector: NewCodexToolCorrector(),
	}

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	outcomeCh := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.Forward(
			context.Background(),
			c,
			agentMessageIntegrationOpenAIOAuthAccount(true),
			requestBody,
		)
		outcomeCh <- forwardOutcome{result: result, err: err}
	}()

	select {
	case <-upstreamBody.firstRead:
	case <-time.After(time.Second):
		_ = upstreamBody.Close()
		select {
		case <-outcomeCh:
		case <-time.After(time.Second):
			t.Fatal("forward remained blocked after releasing the held-open upstream body")
		}
		t.Fatal("passthrough did not start reading the upstream SSE body")
	}

	select {
	case outcome := <-outcomeCh:
		require.Error(t, outcome.err, "an SSE stream without a terminal event must not succeed")
		require.NotNil(t, outcome.result, "partial usage must reach the billing caller even when forwarding times out")
		require.Equal(t, 6, outcome.result.Usage.InputTokens)
		require.Equal(t, 2, outcome.result.Usage.OutputTokens)
		require.Len(t, upstream.requests, 1, "a partially sent tool call must not be replayed")
		require.Contains(t, recorder.Body.String(), "response.function_call_arguments.done")
		require.NotContains(t, recorder.Body.String(), "response.completed")
		select {
		case <-upstreamBody.closed:
		default:
			t.Fatal("timeout returned without closing the upstream response body")
		}
	case <-time.After(1500 * time.Millisecond):
		_ = upstreamBody.Close()
		select {
		case <-outcomeCh:
		case <-time.After(time.Second):
			t.Fatal("forward remained blocked after releasing the held-open upstream body")
		}
		t.Fatal("passthrough remained blocked past StreamDataIntervalTimeout without a terminal event")
	}
}

func TestAgentSubrequestPassthroughIdleTimeoutWithQueuedScannerEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, recorder := agentMessageIntegrationContext("/v1/responses", nil)
	c.Writer = &agentSubrequestDelayedWriteResponseWriter{
		ResponseWriter: c.Writer,
		delay:          1200 * time.Millisecond,
	}
	upstreamBody := &agentSubrequestHeldOpenBody{
		payload: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\",\"usage\":{\"input_tokens\":9,\"output_tokens\":3,\"total_tokens\":12}}\n\n"),
		closed:  make(chan struct{}),
	}
	t.Cleanup(func() { _ = upstreamBody.Close() })
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ForceCodexCLI:             false,
			StreamDataIntervalTimeout: 1,
		}},
		toolCorrector: NewCodexToolCorrector(),
	}

	result, err := svc.handleStreamingResponsePassthrough(
		context.Background(),
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       upstreamBody,
		},
		c,
		agentMessageIntegrationOpenAIOAuthAccount(true),
		time.Now(),
		"gpt-5.2",
		"gpt-5.2",
	)

	require.ErrorContains(t, err, "missing terminal event")
	require.NotNil(t, result)
	require.Equal(t, 9, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "response.output_text.delta")
	require.NotContains(t, recorder.Body.String(), "response.completed")
	select {
	case <-upstreamBody.closed:
	default:
		t.Fatal("idle timeout returned without closing the upstream response body")
	}
}

func TestAgentSubrequestPassthroughClientDisconnectDrainIsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := agentMessageIntegrationContext("/v1/responses", nil)
	c.Writer = &agentSubrequestFailWriteResponseWriter{ResponseWriter: c.Writer}
	upstreamBody := &agentSubrequestContinuousUsageBody{closed: make(chan struct{})}
	t.Cleanup(func() { _ = upstreamBody.Close() })
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ForceCodexCLI:             false,
			StreamDataIntervalTimeout: 1,
		}},
		toolCorrector: NewCodexToolCorrector(),
	}
	startedAt := time.Now()

	result, err := svc.handleStreamingResponsePassthrough(
		context.Background(),
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       upstreamBody,
		},
		c,
		agentMessageIntegrationOpenAIOAuthAccount(true),
		startedAt,
		"gpt-5.2",
		"gpt-5.2",
	)

	require.ErrorContains(t, err, "client disconnect")
	require.NotNil(t, result)
	require.Equal(t, 9, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	require.Less(t, time.Since(startedAt), 2*time.Second)
	select {
	case <-upstreamBody.closed:
	default:
		t.Fatal("client-disconnect drain returned without closing the upstream response body")
	}
}
