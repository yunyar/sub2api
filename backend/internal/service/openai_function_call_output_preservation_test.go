package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyCodexOAuthTransformPreservesFunctionCallOutputPayload(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{
			name:   "plain text",
			output: `"plain result"`,
		},
		{
			name:   "text and encrypted content items",
			output: `[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"opaque-ciphertext"}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-luna","input":[{"type":"function_call","call_id":"fc_task","name":"spawn_agent","arguments":"{}"},{"type":"function_call_output","call_id":"fc_task","output":` + test.output + `}]}`)
			var request map[string]any
			require.NoError(t, json.Unmarshal(body, &request))

			result := applyCodexOAuthTransformWithOptions(request, codexOAuthTransformOptions{
				IsCodexCLI:          true,
				PreserveToolCallIDs: true,
			})
			require.NoError(t, result.Error)

			input, ok := request["input"].([]any)
			require.True(t, ok)
			require.Len(t, input, 2)
			outputItem, ok := input[1].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "function_call_output", outputItem["type"])
			require.Equal(t, "fc_task", outputItem["call_id"])

			actualOutput, err := json.Marshal(outputItem["output"])
			require.NoError(t, err)
			require.JSONEq(t, test.output, string(actualOutput))
		})
	}
}
