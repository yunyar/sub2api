package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentWaitRoundTrip_AnthropicRejectsOpaqueToolOutput(t *testing.T) {
	request := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_wait_running","name":"multi_agent_v1__wait_agent","arguments":"{\"agent_id\":\"gpt_child\"}"},
			{"type":"function_call_output","call_id":"call_wait_running","output":[
				{"type":"input_text","text":"Message Type: STATUS_UPDATE\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}
			]}
		]`),
	}

	_, err := ResponsesToAnthropicRequest(request)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent,
		"opaque wait output must be rejected rather than lost or sent to Claude")
}

func TestAgentWaitRoundTrip_AnthropicPreservesMislabeledPlaintextToolOutput(t *testing.T) {
	const callID = "call_wait_running"
	const envelope = "Message Type: STATUS_UPDATE\nPayload:\n"
	const payload = "Child is still running; no result is available yet."
	request := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_wait_running","name":"multi_agent_v1__wait_agent","arguments":"{\"agent_id\":\"gpt_child\"}"},
			{"type":"function_call_output","call_id":"call_wait_running","output":[
				{"type":"input_text","text":"Message Type: STATUS_UPDATE\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"Child is still running; no result is available yet."}
			]}
		]`),
	}

	anthropicRequest, err := ResponsesToAnthropicRequest(request)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Messages, 2)

	var toolUse, toolResult AnthropicContentBlock
	var foundToolUse, foundToolResult bool
	for _, message := range anthropicRequest.Messages {
		for _, block := range parseContentBlocks(message.Content) {
			switch {
			case block.Type == "tool_use" && block.Name == "multi_agent_v1__wait_agent":
				toolUse = block
				foundToolUse = true
			case block.Type == "tool_result" && block.ToolUseID == callID:
				toolResult = block
				foundToolResult = true
			}
		}
	}
	require.True(t, foundToolUse, "wait_agent call must remain identifiable")
	require.True(t, foundToolResult, "wait_agent output must remain paired with its tool_use")
	require.Equal(t, toolUse.ID, toolResult.ToolUseID)

	var content []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(toolResult.Content, &content))
	require.Len(t, content, 2)
	require.Equal(t, envelope, content[0].Text)
	require.Equal(t, payload, content[1].Text,
		"confirmed plaintext mislabeled as encrypted_content must reach the matching tool_result intact")
}
