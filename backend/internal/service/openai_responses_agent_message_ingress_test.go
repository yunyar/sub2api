package service

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesAgentMessageIngressNormalizesAcrossTransports(t *testing.T) {
	tests := []struct {
		name      string
		normalize func([]byte) ([]byte, bool, error)
	}{
		{
			name:      "HTTP native Responses",
			normalize: normalizeOpenAIResponsesAgentMessages,
		},
		{
			name: "WebSocket API key",
			normalize: func(body []byte) ([]byte, bool, error) {
				return normalizeOpenAIResponsesWebSocketCompatibilityBody(body, &Account{
					Platform: PlatformOpenAI,
					Type:     AccountTypeAPIKey,
				}, false)
			},
		},
		{
			name: "WebSocket OAuth",
			normalize: func(body []byte) ([]byte, bool, error) {
				return normalizeOpenAIResponsesWebSocketCompatibilityBody(body, &Account{
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
				}, false)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-luna","input":[{"type":"agent_message","id":"amsg_external","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"Compare these references."},{"type":"input_image","image_url":"data:image/png;base64,AA=="},{"type":"input_file","filename":"notes.txt","file_data":"data:text/plain;base64,bm90ZXM="}]}],"tools":[{"type":"function","name":"report","parameters":{"type":"object","properties":{"value":{"type":"integer","const":9007199254740993}}}}]}`)
			original := bytes.Clone(body)

			normalized, changed, err := test.normalize(body)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, original, body)
			require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
			require.Equal(t, "user", gjson.GetBytes(normalized, "input.0.role").String())
			require.False(t, gjson.GetBytes(normalized, "input.0.id").Exists())
			require.Equal(t, "Compare these references.", gjson.GetBytes(normalized, "input.0.content.1.text").String())
			require.Equal(t, "input_image", gjson.GetBytes(normalized, "input.0.content.2.type").String())
			require.Equal(t, "notes.txt", gjson.GetBytes(normalized, "input.0.content.3.filename").String())
			require.Equal(t, "report", gjson.GetBytes(normalized, "tools.0.name").String())
			require.Contains(t, string(normalized), "9007199254740993")

			repeated, repeatedChanged, err := test.normalize(normalized)
			require.NoError(t, err)
			require.False(t, repeatedChanged)
			require.Equal(t, normalized, repeated)
		})
	}
}

func TestResponsesAgentMessageIngressKeepsNativeInputAuthoritative(t *testing.T) {
	body := []byte(`{"model":"gpt-6-luna","input":[{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"Use the current task."}]}],"messages":[{"role":"user","content":"stale task"}],"commands":[]}`)

	legacyBody, changed, err := normalizeOpenAIResponsesLegacyIngress(body)
	require.NoError(t, err)
	require.True(t, changed)
	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(legacyBody)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "messages").Exists())
	require.False(t, gjson.GetBytes(normalized, "commands").Exists())
	require.Equal(t, int64(1), gjson.GetBytes(normalized, "input.#").Int())
	require.Equal(t, "Use the current task.", gjson.GetBytes(normalized, "input.0.content.1.text").String())
	require.NotContains(t, string(normalized), "stale task")
}
