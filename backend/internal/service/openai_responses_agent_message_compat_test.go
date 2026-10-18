package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesAgentMessages_PlainTaskWithMedia(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6.1-sol",
		"request_id":"req_native_1",
		"tools":[{"type":"function","name":"keep"}],
		"metadata":{"large":9007199254740993},
		"input":[
			{"type":"message","role":"user","content":"prior"},
			{"type":"agent_message","id":"amsg_1","author":"/root","recipient":"/root/gpt_task","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/gpt_task\nSender: /root\nPayload:\n"},
				{"type":"input_image","image_url":"data:image/png;base64,AA=="},
				{"type":"encrypted_content","encrypted_content":"Inspect the image and report its dimensions."}
			]},
			{"type":"reasoning","id":"rs_keep","encrypted_content":"gAAAAopaque","summary":[]}
		]
	}`)
	original := bytes.Clone(body)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, original, body, "helper must not mutate caller-owned bytes")

	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(normalized, "model").String())
	require.Equal(t, "req_native_1", gjson.GetBytes(normalized, "request_id").String())
	require.Contains(t, string(normalized), `"large":9007199254740993`)
	require.Equal(t, "function", gjson.GetBytes(normalized, "tools.0.type").String())
	require.Equal(t, "reasoning", gjson.GetBytes(normalized, "input.2.type").String())
	require.Equal(t, "gAAAAopaque", gjson.GetBytes(normalized, "input.2.encrypted_content").String())

	agentMessage := gjson.GetBytes(normalized, "input.1")
	require.Equal(t, "message", agentMessage.Get("type").String())
	require.Equal(t, "user", agentMessage.Get("role").String())
	require.False(t, agentMessage.Get("id").Exists())
	require.Equal(t, 3, len(agentMessage.Get("content").Array()))
	require.Equal(t, "input_text", agentMessage.Get("content.0.type").String())
	require.Equal(t, "Message Type: NEW_TASK\nTask name: /root/gpt_task\nSender: /root\nPayload:\n", agentMessage.Get("content.0.text").String())
	require.Equal(t, "input_image", agentMessage.Get("content.1.type").String())
	require.Equal(t, "data:image/png;base64,AA==", agentMessage.Get("content.1.image_url").String())
	require.Equal(t, "input_text", agentMessage.Get("content.2.type").String())
	require.Equal(t, "Inspect the image and report its dimensions.", agentMessage.Get("content.2.text").String())
}

func TestNormalizeOpenAIResponsesAgentMessages_PreservesUncertainItems(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{
			name: "encrypted content without task envelope",
			item: `{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"plain-looking but unclassified"}]}`,
		},
		{
			name: "recognized opaque fernet content",
			item: `{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}]}`,
		},
		{
			name: "recognized opaque anthropic content",
			item: `{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"anthropic-thinking-v1:opaque"}]}`,
		},
		{
			name: "empty content",
			item: `{"type":"agent_message","content":[]}`,
		},
		{
			name: "missing content",
			item: `{"type":"agent_message","id":"amsg_empty"}`,
		},
		{
			name: "unsupported content part",
			item: `{"type":"agent_message","content":[{"type":"future_part","value":"keep"}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte("{ \"model\" : \"keep\", \"input\" : [" + test.item + "] }")
			original := bytes.Clone(body)

			normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, original, normalized)
			require.Equal(t, original, body)
		})
	}
}

func TestNormalizeOpenAIResponsesAgentMessages_NoAgentMessageReturnsOriginalBytes(t *testing.T) {
	body := []byte(" { \"model\" : \"keep\", \"input\" : [{\"type\":\"message\",\"role\":\"user\",\"content\":\"hi\"}], \"value\":9007199254740993 } ")

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, normalized)
}

func TestNormalizeOpenAIResponsesAgentMessages_ConvertsSingleInputObject(t *testing.T) {
	body := []byte(`{"model":"keep","input":{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\nDone."}]}}`)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.type").String())
	require.Equal(t, "user", gjson.GetBytes(normalized, "input.role").String())
	require.Equal(t, "Message Type: FINAL_ANSWER\nPayload:\nDone.", gjson.GetBytes(normalized, "input.content.0.text").String())
}

func TestNormalizeOpenAIResponsesAgentMessages_PreservesMediaOnlyMessage(t *testing.T) {
	body := []byte(`{"input":[{"type":"agent_message","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
	require.Equal(t, "input_image", gjson.GetBytes(normalized, "input.0.content.0.type").String())
	require.Equal(t, "data:image/png;base64,AA==", gjson.GetBytes(normalized, "input.0.content.0.image_url").String())
}

func TestNormalizeOpenAIResponsesAgentMessages_InvalidJSONReturnsErrorAndOriginal(t *testing.T) {
	body := []byte(`{"input":[`)
	original := bytes.Clone(body)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.Error(t, err)
	require.False(t, changed)
	require.Equal(t, original, normalized)
	require.Equal(t, original, body)
}

func TestNormalizeOpenAIResponsesAgentMessages_PreservesLongBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyz0123456789"))
	body := []byte(`{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"` + encoded + `"}]}]}`)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, normalized)
}

func TestNormalizeOpenAIResponsesAgentMessages_OutputIsValidJSON(t *testing.T) {
	body := []byte(`{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"Do the task."}]}]}`)

	normalized, changed, err := normalizeOpenAIResponsesAgentMessages(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, json.Valid(normalized))
}
