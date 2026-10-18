package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func agentOrchestrationTextBlocks(message AnthropicMessage) []string {
	var texts []string
	for _, block := range parseContentBlocks(message.Content) {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return texts
}

func TestAgentOrchestrationResponsesAnthropicRoundTrip(t *testing.T) {
	request := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"tools": []any{
			map[string]any{
				"type": "namespace",
				"name": "multi_agent_v1",
				"tools": []any{
					map[string]any{"type": "function", "name": "spawn_agent", "parameters": map[string]any{"type": "object"}},
					map[string]any{"type": "function", "name": "wait_agent", "parameters": map[string]any{"type": "object"}},
					map[string]any{"type": "function", "name": "close_agent", "parameters": map[string]any{"type": "object"}},
				},
			},
		},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "Delegate this task and wait for its result."},
			map[string]any{"type": "function_call", "call_id": "call_spawn", "namespace": "multi_agent_v1", "name": "spawn_agent", "arguments": `{"model":"gpt-5.5","task":"Inspect the requested change."}`},
			map[string]any{"type": "function_call_output", "call_id": "call_spawn", "output": `{"agent_id":"gpt_child","status":"running"}`},
			map[string]any{
				"type": "agent_message", "id": "amsg_task", "author": "/root", "recipient": "/root/gpt_child",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Message Type: NEW_TASK\nTask name: /root/gpt_child\nSender: /root\nPayload:\n"},
					map[string]any{"type": "encrypted_content", "encrypted_content": "Inspect the requested change and report evidence."},
				},
			},
			map[string]any{"type": "function_call", "call_id": "call_wait_running", "namespace": "multi_agent_v1", "name": "wait_agent", "arguments": `{"agent_id":"gpt_child"}`},
			map[string]any{
				"type": "agent_message", "id": "amsg_status", "author": "/root/gpt_child", "recipient": "/root",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Message Type: STATUS_UPDATE\nTask name: /root/gpt_child\nSender: /root/gpt_child\nPayload:\n"},
					map[string]any{"type": "encrypted_content", "encrypted_content": "The child is still running; no result is available yet."},
				},
			},
			map[string]any{"type": "function_call_output", "call_id": "call_wait_running", "output": `{"status":"running","result":null}`},
			map[string]any{"type": "function_call", "call_id": "call_wait_done", "namespace": "multi_agent_v1", "name": "wait_agent", "arguments": `{"agent_id":"gpt_child"}`},
			map[string]any{"type": "function_call_output", "call_id": "call_wait_done", "output": `{"status":"completed","result":"The requested change is present."}`},
			map[string]any{
				"type": "agent_message", "id": "amsg_final", "author": "/root/gpt_child", "recipient": "/root",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Message Type: FINAL_ANSWER\nTask name: /root\nSender: /root/gpt_child\nPayload:\n"},
					map[string]any{"type": "encrypted_content", "encrypted_content": "The requested change is present."},
				},
			},
			map[string]any{"type": "function_call", "call_id": "call_close", "namespace": "multi_agent_v1", "name": "close_agent", "arguments": `{"agent_id":"gpt_child"}`},
			map[string]any{"type": "function_call_output", "call_id": "call_close", "output": `{"status":"closed"}`},
		},
	}

	mapping, changed, err := AdaptResponsesClientTools(request)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]ResponsesNamespaceName{
		"multi_agent_v1__spawn_agent": {Namespace: "multi_agent_v1", Name: "spawn_agent"},
		"multi_agent_v1__wait_agent":  {Namespace: "multi_agent_v1", Name: "wait_agent"},
		"multi_agent_v1__close_agent": {Namespace: "multi_agent_v1", Name: "close_agent"},
	}, mapping.NamespaceTools)

	adaptedTools, ok := request["tools"].([]any)
	require.True(t, ok)
	require.Len(t, adaptedTools, 3)
	for index, name := range []string{
		"multi_agent_v1__spawn_agent",
		"multi_agent_v1__wait_agent",
		"multi_agent_v1__close_agent",
	} {
		tool, ok := adaptedTools[index].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "function", tool["type"])
		require.Equal(t, name, tool["name"])
	}

	adaptedInput, ok := request["input"].([]any)
	require.True(t, ok)
	require.Len(t, adaptedInput, 12)
	var adaptedCallNames []string
	for _, index := range []int{1, 4, 7, 10} {
		call, ok := adaptedInput[index].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "function_call", call["type"])
		require.NotContains(t, call, "namespace")
		name, ok := call["name"].(string)
		require.True(t, ok)
		adaptedCallNames = append(adaptedCallNames, name)
	}
	require.Equal(t, []string{
		"multi_agent_v1__spawn_agent",
		"multi_agent_v1__wait_agent",
		"multi_agent_v1__wait_agent",
		"multi_agent_v1__close_agent",
	}, adaptedCallNames)

	adaptedBody, err := json.Marshal(request)
	require.NoError(t, err)
	var responsesRequest ResponsesRequest
	require.NoError(t, json.Unmarshal(adaptedBody, &responsesRequest))
	anthropicRequest, err := ResponsesToAnthropicRequest(&responsesRequest)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Tools, 3)
	require.Equal(t, []string{
		"multi_agent_v1__spawn_agent",
		"multi_agent_v1__wait_agent",
		"multi_agent_v1__close_agent",
	}, []string{
		anthropicRequest.Tools[0].Name,
		anthropicRequest.Tools[1].Name,
		anthropicRequest.Tools[2].Name,
	})

	assertAnthropicPairing(t, anthropicRequest.Messages)
	resultOutputs := map[string]string{
		"call_spawn":        `{"agent_id":"gpt_child","status":"running"}`,
		"call_wait_running": `{"status":"running","result":null}`,
		"call_wait_done":    `{"status":"completed","result":"The requested change is present."}`,
		"call_close":        `{"status":"closed"}`,
	}
	var toolUseCount int
	for index, message := range anthropicRequest.Messages {
		if message.Role != "assistant" {
			continue
		}
		for _, block := range parseContentBlocks(message.Content) {
			if block.Type != "tool_use" {
				continue
			}
			toolUseCount++
			require.Less(t, index+1, len(anthropicRequest.Messages))
			require.Equal(t, "user", anthropicRequest.Messages[index+1].Role)
			results := parseContentBlocks(anthropicRequest.Messages[index+1].Content)
			var paired bool
			for _, result := range results {
				if result.Type != "tool_result" || result.ToolUseID != block.ID {
					continue
				}
				paired = true
				var output string
				require.NoError(t, json.Unmarshal(result.Content, &output))
				require.JSONEq(t, resultOutputs[block.ID], output)
			}
			require.Truef(t, paired, "missing paired tool_result for %s", block.ID)
		}
	}
	require.Len(t, resultOutputs, toolUseCount)

	var sawTask, sawRunningNotice, sawFinalNotice bool
	for _, message := range anthropicRequest.Messages {
		text := agentOrchestrationTextBlocks(message)
		joined := ""
		for _, part := range text {
			joined += part
		}
		if strings.Contains(joined, "Message Type: NEW_TASK") {
			require.Equal(t, "user", message.Role, "the delegated task must remain input context, not assistant output")
			sawTask = true
			require.Contains(t, joined, "Sender: /root")
			require.Contains(t, joined, "Inspect the requested change and report evidence.")
		}
		if strings.Contains(joined, "Message Type: STATUS_UPDATE") {
			sawRunningNotice = true
			require.Equal(t, "user", message.Role)
			require.Contains(t, joined, "The child is still running; no result is available yet.")
			require.True(t, hasToolResult(parseContentBlocks(message.Content), "call_wait_running"))
		}
		if strings.Contains(joined, "Message Type: FINAL_ANSWER") {
			sawFinalNotice = true
			require.Equal(t, "user", message.Role)
			require.Contains(t, joined, "The requested change is present.")
			require.True(t, hasToolResult(parseContentBlocks(message.Content), "call_wait_done"))
		}
	}
	require.True(t, sawTask, "the delegated task envelope and plaintext payload must reach Claude")
	require.True(t, sawRunningNotice, "the running notification must reach Claude as user context")
	require.True(t, sawFinalNotice, "the child final-answer notification must reach Claude as user context")
	anthropicWire, err := json.Marshal(anthropicRequest)
	require.NoError(t, err)
	require.NotContains(t, string(anthropicWire), `"author"`)
	require.NotContains(t, string(anthropicWire), `"recipient"`)
	require.NotContains(t, string(anthropicWire), `"amsg_task"`)

	upstreamResponse := &AnthropicResponse{
		ID:         "msg_parent",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-sonnet-4-20250514",
		StopReason: AnthropicStopReasonPtr("tool_use"),
		Content: []AnthropicContentBlock{{
			Type:  "tool_use",
			ID:    "call_next_wait",
			Name:  "multi_agent_v1__wait_agent",
			Input: json.RawMessage(`{"agent_id":"gpt_child"}`),
		}},
	}
	responsesOutput, err := json.Marshal(AnthropicToResponsesResponse(upstreamResponse))
	require.NoError(t, err)
	restoredOutput, restored, err := RestoreResponsesClientToolPayload(responsesOutput, mapping)
	require.NoError(t, err)
	require.True(t, restored)
	var restoredResponse struct {
		Output []ResponsesOutput `json:"output"`
	}
	require.NoError(t, json.Unmarshal(restoredOutput, &restoredResponse))
	require.Len(t, restoredResponse.Output, 1)
	require.Equal(t, "function_call", restoredResponse.Output[0].Type)
	require.Equal(t, "call_next_wait", restoredResponse.Output[0].CallID)
	require.Equal(t, "multi_agent_v1", restoredResponse.Output[0].Namespace)
	require.Equal(t, "wait_agent", restoredResponse.Output[0].Name)
}
