package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesToChatCompletionsRequestRejectsOpaqueAgentFunctionOutput(t *testing.T) {
	const encryptedOutput = `[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}]`
	req := &ResponsesRequest{
		Model: "gpt-6-luna",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"fc_task","name":"spawn_agent","arguments":"{}"},
			{"type":"function_call_output","call_id":"fc_task","output":` + encryptedOutput + `}
		]`),
	}
	_, err := ResponsesToChatCompletionsRequest(req)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
}

func TestResponsesAgentFunctionOutputRestoresPortablePlaintext(t *testing.T) {
	const taskOutput = `[{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"The child found the root cause."}]`
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"fc_wait","name":"wait_agent","arguments":"{}"},
			{"type":"function_call_output","call_id":"fc_wait","output":` + taskOutput + `}
		]`),
	}
	anthropicRequest, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Messages, 2)
	var calls []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anthropicRequest.Messages[0].Content, &calls))
	require.Len(t, calls, 1)
	var results []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anthropicRequest.Messages[1].Content, &results))
	require.Len(t, results, 1)
	require.Equal(t, "tool_result", results[0].Type)
	require.NotEmpty(t, calls[0].ID)
	require.Equal(t, calls[0].ID, results[0].ToolUseID)
	var content []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(results[0].Content, &content))
	require.Len(t, content, 2)
	require.Equal(t, "Message Type: FINAL_ANSWER\nPayload:\n", content[0].Text)
	require.Equal(t, "The child found the root cause.", content[1].Text)

	chatRequest, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chatRequest.Messages, 2)
	require.Equal(t, "tool", chatRequest.Messages[1].Role)
	require.Equal(t, "fc_wait", chatRequest.Messages[1].ToolCallID)
	var outputText string
	require.NoError(t, json.Unmarshal(chatRequest.Messages[1].Content, &outputText))
	require.JSONEq(t, `[{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\n"},{"type":"input_text","text":"The child found the root cause."}]`, outputText)
}

func TestResponsesAgentFunctionOutputRejectsRealCiphertext(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"fc_wait","name":"wait_agent","arguments":"{}"},
			{"type":"function_call_output","call_id":"fc_wait","output":[
				{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}
			]}
		]`),
	}
	_, err := ResponsesToAnthropicRequest(req)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
	_, err = ResponsesToChatCompletionsRequest(req)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
}

func TestNormalizeAgentFunctionOutputContentPreservesOrdinaryOutputs(t *testing.T) {
	for _, output := range []string{
		`"The child finished."`,
		`{"status":"completed","results":[{"type":"encrypted_content","encrypted_content":"application data"}]}`,
		`[{"type":"input_text", "text":"ordinary output"},{"type":"input_image","image_url":"data:image/png;base64,AA=="}]`,
	} {
		normalized, changed, err := NormalizeAgentFunctionOutputContent(json.RawMessage(output))
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, output, string(normalized))
	}
}

func TestResponsesAgentFunctionOutputPreservesPortableMediaOrder(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"fc_wait","name":"wait_agent","arguments":"{}"},
			{"type":"function_call_output","call_id":"fc_wait","output":[
				{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\n"},
				{"type":"input_image","image_url":"data:image/png;base64,AA=="},
				{"type":"encrypted_content","encrypted_content":"The child attached its report."},
				{"type":"input_file","filename":"report.pdf","file_data":"data:application/pdf;base64,JVBERi0="}
			]}
		]`),
	}
	anthropicRequest, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Messages, 2)
	var results []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anthropicRequest.Messages[1].Content, &results))
	require.Len(t, results, 1)
	var content []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(results[0].Content, &content))
	require.Len(t, content, 4)
	require.Equal(t, "Message Type: FINAL_ANSWER\nPayload:\n", content[0].Text)
	require.Equal(t, "image", content[1].Type)
	require.Equal(t, "The child attached its report.", content[2].Text)
	require.Equal(t, "document", content[3].Type)
	require.Equal(t, "application/pdf", content[3].Source.MediaType)
}
