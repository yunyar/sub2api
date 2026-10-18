//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	agentMessageIntegrationTaskHeader = "Message Type: NEW_TASK\nTask name: integration\nPayload:\n"
	agentMessageIntegrationPlainTask  = "Compare the image with the attached brief."
	agentMessageIntegrationImage      = "data:image/png;base64,AA=="
	agentMessageIntegrationFile       = "data:application/pdf;base64,JVBERi0="
)

func agentMessageIntegrationResponsesBody(model string, stream bool) []byte {
	return []byte(fmt.Sprintf(`{
		"model":%q,
		"instructions":"Follow the delegated task.",
		"stream":%t,
		"store":true,
		"max_output_tokens":123,
		"input":[{
			"type":"agent_message",
			"id":"amsg_integration",
			"content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: integration\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"Compare the image with the attached brief."},
				{"type":"input_image","image_url":"data:image/png;base64,AA=="},
				{"type":"input_file","filename":"brief.pdf","file_data":"data:application/pdf;base64,JVBERi0="}
			]
		}],
		"tools":[{
			"type":"function",
			"name":"lookup",
			"description":"Look up a task reference.",
			"parameters":{
				"type":"object",
				"properties":{"query":{"type":"string","enum":["alpha","beta"],"default":"alpha"}},
				"required":["query"],
				"additionalProperties":false
			},
			"strict":true
		}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`, model, stream))
}

func agentMessageIntegrationOpaqueBody(model string, stream bool) []byte {
	body := agentMessageIntegrationResponsesBody(model, stream)
	return []byte(strings.Replace(
		string(body),
		`"encrypted_content":"`+agentMessageIntegrationPlainTask+`"`,
		`"encrypted_content":"gAAAAABopaque-agent-message-payload"`,
		1,
	))
}

func agentMessageIntegrationUnconfirmedBody(model string) []byte {
	body := agentMessageIntegrationResponsesBody(model, false)
	unconfirmed, err := sjson.SetBytes(body, "input.0.content.0.text", "Context without the agent task envelope.")
	if err != nil {
		panic(err)
	}
	return unconfirmed
}

func agentMessageIntegrationUnrepresentableFileBody(model string) []byte {
	body := agentMessageIntegrationResponsesBody(model, false)
	return []byte(strings.Replace(
		string(body),
		`"file_data":"`+agentMessageIntegrationFile+`"`,
		`"file_id":"file_unavailable"`,
		1,
	))
}

func agentMessageIntegrationContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func agentMessageIntegrationSSE(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func agentMessageIntegrationOpenAIResponsesSSE() *http.Response {
	return agentMessageIntegrationSSE("data: [DONE]\n\n")
}

func agentMessageIntegrationChatResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_agent_message","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
		)),
	}
}

func agentMessageIntegrationOpenAIOAuthAccount(passthrough bool) *Account {
	return &Account{
		ID:          93101,
		Name:        "agent-message-openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "integration-oauth-token",
			"chatgpt_account_id": "integration-chatgpt-account",
		},
		Extra: map[string]any{
			"openai_passthrough":                        passthrough,
			"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeOff,
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func agentMessageIntegrationChatUserMessage(t *testing.T, body []byte) gjson.Result {
	t.Helper()
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() == "user" {
			return message
		}
	}
	require.FailNow(t, "forwarded chat body has no user message")
	return gjson.Result{}
}

func requireAgentMessageIntegrationChatBody(t *testing.T, body []byte) {
	t.Helper()
	userMessage := agentMessageIntegrationChatUserMessage(t, body)
	require.Equal(t, "text", userMessage.Get("content.0.type").String())
	require.Equal(t, agentMessageIntegrationTaskHeader, userMessage.Get("content.0.text").String())
	require.Equal(t, "text", userMessage.Get("content.1.type").String())
	require.Equal(t, agentMessageIntegrationPlainTask, userMessage.Get("content.1.text").String())
	require.Equal(t, "image_url", userMessage.Get("content.2.type").String())
	require.Equal(t, agentMessageIntegrationImage, userMessage.Get("content.2.image_url.url").String())
	require.Equal(t, "file", userMessage.Get("content.3.type").String())
	require.Equal(t, "brief.pdf", userMessage.Get("content.3.file.filename").String())
	require.Equal(t, agentMessageIntegrationFile, userMessage.Get("content.3.file.file_data").String())
	require.Equal(t, "lookup", gjson.GetBytes(body, "tools.0.function.name").String())
	require.Equal(t, "beta", gjson.GetBytes(body, "tools.0.function.parameters.properties.query.enum.1").String())
	require.Equal(t, "lookup", gjson.GetBytes(body, "tool_choice.function.name").String())
}

func TestOpenAIResponsesAgentMessageIntegration_ForwardOAuthAndPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name        string
		passthrough bool
	}{
		{name: "oauth transform"},
		{name: "oauth passthrough", passthrough: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := agentMessageIntegrationResponsesBody("gpt-5.2", true)
			c, _ := agentMessageIntegrationContext("/v1/responses", body)
			upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationOpenAIResponsesSSE()}
			svc := &OpenAIGatewayService{
				cfg:          &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: false}},
				httpUpstream: upstream,
			}

			result, err := svc.Forward(context.Background(), c, agentMessageIntegrationOpenAIOAuthAccount(test.passthrough), body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "message", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
			require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "input.0.role").String())
			require.Equal(t, agentMessageIntegrationTaskHeader, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
			require.Equal(t, agentMessageIntegrationPlainTask, gjson.GetBytes(upstream.lastBody, "input.0.content.1.text").String())
			require.Equal(t, agentMessageIntegrationImage, gjson.GetBytes(upstream.lastBody, "input.0.content.2.image_url").String())
			require.Equal(t, "brief.pdf", gjson.GetBytes(upstream.lastBody, "input.0.content.3.filename").String())
			require.Equal(t, agentMessageIntegrationFile, gjson.GetBytes(upstream.lastBody, "input.0.content.3.file_data").String())
			require.Equal(t, "beta", gjson.GetBytes(upstream.lastBody, "tools.0.parameters.properties.query.enum.1").String())
			require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "tool_choice.name").String())
		})
	}
}

func TestOpenAIResponsesAgentMessageIntegration_ForwardAPIKeyPreservesOpaqueNativeItem(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationOpaqueBody("gpt-5.4", false)
	c, _ := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_agent_message","object":"response","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}}
	account := &Account{
		ID:          93102,
		Name:        "agent-message-openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-integration", "base_url": "http://upstream.example"},
		Extra:       map[string]any{"openai_passthrough": true},
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "agent_message", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "encrypted_content", gjson.GetBytes(upstream.lastBody, "input.0.content.1.type").String())
	require.Equal(t, "gAAAAABopaque-agent-message-payload", gjson.GetBytes(upstream.lastBody, "input.0.content.1.encrypted_content").String())
}

func TestOpenAIResponsesAgentMessageIntegration_ForwardGrokKeepsNativeTaskAndMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationResponsesBody("grok-4.5", true)
	c, _ := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationSSE(strings.Join([]string{
		`data: {"type":"response.output_text.delta","sequence_number":0,"delta":"ok"}`,
		"",
		`data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_agent_grok","model":"grok-4.5","usage":{"input_tokens":2,"output_tokens":1}}}`,
		"",
	}, "\n"))}
	account := &Account{
		ID:          93103,
		Name:        "agent-message-grok-apikey",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "xai-integration", "base_url": "https://api.x.ai/v1"},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "message", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
	require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "input.0.role").String())
	require.Equal(t, agentMessageIntegrationTaskHeader, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
	require.Equal(t, agentMessageIntegrationPlainTask, gjson.GetBytes(upstream.lastBody, "input.0.content.1.text").String())
	require.Equal(t, agentMessageIntegrationImage, gjson.GetBytes(upstream.lastBody, "input.0.content.2.image_url").String())
	require.Equal(t, agentMessageIntegrationFile, gjson.GetBytes(upstream.lastBody, "input.0.content.3.file_data").String())
	require.Equal(t, "beta", gjson.GetBytes(upstream.lastBody, "tools.0.parameters.properties.query.enum.1").String())
}

func TestOpenAIResponsesAgentMessageIntegration_ForwardResponsesChatFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationResponsesBody("gpt-5.4", false)
	c, _ := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationChatResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	requireAgentMessageIntegrationChatBody(t, upstream.lastBody)
}

func TestOpenAIResponsesAgentMessageIntegration_ForwardAsChatCompletionsResponsesShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationResponsesBody("glm-4.7", false)
	c, _ := agentMessageIntegrationContext("/v1/chat/completions", body)
	upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationChatResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := adaptiveProtocolTestAccount(PlatformZhipu, map[string]any{
		APIProtocolChatCompletions: "http://chat.example",
		APIProtocolAnthropic:       "http://anthropic.example",
	})

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "http://chat.example/v1/chat/completions", upstream.lastReq.URL.String())
	requireAgentMessageIntegrationChatBody(t, upstream.lastBody)
}

func TestOpenAIResponsesAgentMessageIntegration_WebSocketAPIKeyFinalBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_ws_agent_message","model":"gpt-5.4","usage":{"input_tokens":2,"output_tokens":1}}}`)
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, svc, passthroughLifecycleAccount())
	defer server.Close()

	body, err := sjson.SetBytes(agentMessageIntegrationResponsesBody("gpt-5.4", false), "type", "response.create")
	require.NoError(t, err)
	clientConn := dialPassthroughLifecycleClientWithPayload(t, server, string(body))
	defer func() { _ = clientConn.CloseNow() }()

	upstreamBody := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Equal(t, "response.create", gjson.GetBytes(upstreamBody, "type").String())
	require.Equal(t, "message", gjson.GetBytes(upstreamBody, "input.0.type").String())
	require.Equal(t, "user", gjson.GetBytes(upstreamBody, "input.0.role").String())
	require.Equal(t, agentMessageIntegrationTaskHeader, gjson.GetBytes(upstreamBody, "input.0.content.0.text").String())
	require.Equal(t, agentMessageIntegrationPlainTask, gjson.GetBytes(upstreamBody, "input.0.content.1.text").String())
	require.Equal(t, agentMessageIntegrationImage, gjson.GetBytes(upstreamBody, "input.0.content.2.image_url").String())
	require.Equal(t, agentMessageIntegrationFile, gjson.GetBytes(upstreamBody, "input.0.content.3.file_data").String())
	require.Equal(t, "lookup", gjson.GetBytes(upstreamBody, "tools.0.name").String())
	require.Equal(t, "beta", gjson.GetBytes(upstreamBody, "tools.0.parameters.properties.query.enum.1").String())
	require.Equal(t, "lookup", gjson.GetBytes(upstreamBody, "tool_choice.name").String())

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("websocket ingress did not exit")
	}
}

func TestOpenAIResponsesAgentMessageIntegration_AnthropicResponsesFinalBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationResponsesBody("claude-sonnet-4-20250514", false)
	c, _ := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationSSE(responsesBridgeSSEPayload("data_only"))}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID:          93104,
		Name:        "agent-message-anthropic",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "anthropic-integration", "base_url": "https://api.anthropic.com"},
	}

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, agentMessageIntegrationTaskHeader, gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String())
	require.Equal(t, agentMessageIntegrationPlainTask, gjson.GetBytes(upstream.lastBody, "messages.0.content.1.text").String())
	require.Equal(t, "image", gjson.GetBytes(upstream.lastBody, "messages.0.content.2.type").String())
	require.Equal(t, "image/png", gjson.GetBytes(upstream.lastBody, "messages.0.content.2.source.media_type").String())
	require.Equal(t, "AA==", gjson.GetBytes(upstream.lastBody, "messages.0.content.2.source.data").String())
	require.Equal(t, "document", gjson.GetBytes(upstream.lastBody, "messages.0.content.3.type").String())
	require.Equal(t, "application/pdf", gjson.GetBytes(upstream.lastBody, "messages.0.content.3.source.media_type").String())
	require.Equal(t, "JVBERi0=", gjson.GetBytes(upstream.lastBody, "messages.0.content.3.source.data").String())
	require.Equal(t, "beta", gjson.GetBytes(upstream.lastBody, "tools.0.input_schema.properties.query.enum.1").String())
	require.Equal(t, "query", gjson.GetBytes(upstream.lastBody, "tools.0.input_schema.required.0").String())
	require.Equal(t, "tool", gjson.GetBytes(upstream.lastBody, "tool_choice.type").String())
	require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "tool_choice.name").String())
	require.Equal(t, int64(123), gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
}

func TestOpenAIResponsesAgentMessageIntegration_AnthropicRejectsUnrecoverableAgentContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	opaqueBody := agentMessageIntegrationOpaqueBody("claude-sonnet-4-20250514", false)
	tests := []struct {
		name             string
		body             []byte
		wantTaskEnvelope bool
	}{
		{name: "opaque encrypted task", body: opaqueBody, wantTaskEnvelope: true},
		{name: "unconfirmed encrypted task", body: agentMessageIntegrationUnconfirmedBody("claude-sonnet-4-20250514")},
		{name: "unrepresentable file reference", body: agentMessageIntegrationUnrepresentableFileBody("claude-sonnet-4-20250514"), wantTaskEnvelope: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.wantTaskEnvelope {
				require.Contains(t, gjson.GetBytes(test.body, "input.0.content.0.text").String(), "Message Type: NEW_TASK")
			} else {
				require.NotContains(t, gjson.GetBytes(test.body, "input.0.content.0.text").String(), "Message Type: NEW_TASK")
			}
			c, recorder := agentMessageIntegrationContext("/v1/responses", test.body)
			upstream := &httpUpstreamRecorder{resp: agentMessageIntegrationSSE(responsesBridgeSSEPayload("data_only"))}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{
				ID:          93105,
				Name:        "agent-message-anthropic",
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "anthropic-integration", "base_url": "https://api.anthropic.com"},
			}

			result, err := svc.ForwardAsResponses(context.Background(), c, account, test.body, nil)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.code").String())
			require.Nil(t, upstream.lastReq, "invalid agent_message content must be rejected before upstream dispatch")
		})
	}
}

func TestOpenAIResponsesAgentMessageIntegration_AntigravityResponsesFinalBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationResponsesBody("gemini-3.1-pro-high", false)
	c, _ := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: antigravityCompatSuccessResponse()}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)

	result, err := svc.ForwardAsResponses(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, agentMessageIntegrationTaskHeader, gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.0.text").String())
	require.Equal(t, agentMessageIntegrationPlainTask, gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.1.text").String())
	require.Equal(t, "image/png", gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.2.inlineData.mimeType").String())
	require.Equal(t, "AA==", gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.2.inlineData.data").String())
	require.Equal(t, "application/pdf", gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.3.inlineData.mimeType").String())
	require.Equal(t, "JVBERi0=", gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.3.inlineData.data").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "request.contents.0.parts.3.text").Exists())
	require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "request.tools.0.functionDeclarations.0.name").String())
	require.Equal(t, "beta", gjson.GetBytes(upstream.lastBody, "request.tools.0.functionDeclarations.0.parameters.properties.query.enum.1").String())
}

func TestOpenAIResponsesAgentMessageIntegration_AntigravityRejectsOpaqueTaskBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := agentMessageIntegrationOpaqueBody("gemini-3.1-pro-high", false)
	c, recorder := agentMessageIntegrationContext("/v1/responses", body)
	upstream := &httpUpstreamRecorder{resp: antigravityCompatSuccessResponse()}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)

	result, err := svc.ForwardAsResponses(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, upstream.lastReq)
}
