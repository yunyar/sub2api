package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesToChatCompletionsRequestPreservesMixedEncryptedFunctionOutput(t *testing.T) {
	const encryptedOutput = `[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"opaque-ciphertext"}]`
	req := &ResponsesRequest{
		Model: "gpt-6-luna",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"fc_task","name":"spawn_agent","arguments":"{}"},
			{"type":"function_call_output","call_id":"fc_task","output":` + encryptedOutput + `}
		]`),
	}
	chatRequest, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, chatRequest.Messages, 2)
	require.Equal(t, "assistant", chatRequest.Messages[0].Role)
	require.Equal(t, "tool", chatRequest.Messages[1].Role)
	require.Equal(t, "fc_task", chatRequest.Messages[1].ToolCallID)
	var outputText string
	require.NoError(t, json.Unmarshal(chatRequest.Messages[1].Content, &outputText))
	require.JSONEq(t, encryptedOutput, outputText)
}
