package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAgentMessageContent_PlainTextAndMediaKeepOrder(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
		{"type":"input_image","image_url":"data:image/png;base64,AA=="},
		{"type":"encrypted_content","encrypted_content":"Inspect this image."},
		{"type":"input_file","filename":"notes.txt","file_data":"data:text/plain;base64,SGk="},
		{"type":"text","text":"Finish."}
	]`)

	normalized, err := NormalizeAgentMessageContent(content)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
		{"type":"input_image","image_url":"data:image/png;base64,AA=="},
		{"type":"input_text","text":"Inspect this image."},
		{"type":"input_file","filename":"notes.txt","file_data":"data:text/plain;base64,SGk="},
		{"type":"input_text","text":"Finish."}
	]`, string(normalized))
}

func TestNormalizeAgentMessageContent_ImageURLObjects(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		want     string
		imageURL string
	}{
		{
			name:     "input_image object",
			content:  `[{"type":"input_image","image_url":{"url":"data:image/png;base64,AA==","detail":"high","format":"png"},"metadata":{"source":"task"}}]`,
			want:     `[{"type":"input_image","image_url":"data:image/png;base64,AA==","detail":"high","format":"png","metadata":{"source":"task"}}]`,
			imageURL: "data:image/png;base64,AA==",
		},
		{
			name:     "image_url alias object",
			content:  `[{"type":"image_url","image_url":{"url":"https://example.test/image.png","detail":"low","quality":"preview"},"trace":"task-image"}]`,
			want:     `[{"type":"input_image","image_url":"https://example.test/image.png","detail":"low","quality":"preview","trace":"task-image"}]`,
			imageURL: "https://example.test/image.png",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeAgentMessageContent(json.RawMessage(test.content))
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(normalized))

			var parts []ResponsesContentPart
			require.NoError(t, json.Unmarshal(normalized, &parts))
			require.Len(t, parts, 1)
			require.Equal(t, test.imageURL, parts[0].ImageURL)
		})
	}
}

func TestNormalizeAgentMessageContent_OpaqueCiphertextIsIdentifiable(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
		{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}
	]`)

	_, err := NormalizeAgentMessageContent(content)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
}

func TestNormalizeAgentMessageContent_UnconfirmedEncryptedTextErrors(t *testing.T) {
	content := json.RawMessage(`[{"type":"encrypted_content","encrypted_content":"plain-looking but unclassified"}]`)

	_, err := NormalizeAgentMessageContent(content)
	require.ErrorIs(t, err, ErrUnconfirmedAgentMessage)
}

func TestNormalizeAgentMessageContent_UnsupportedPartsError(t *testing.T) {
	for _, partType := range []string{"input_audio", "input_video", "future_part"} {
		t.Run(partType, func(t *testing.T) {
			content := json.RawMessage(`[{"type":"` + partType + `","value":"keep"}]`)

			_, err := NormalizeAgentMessageContent(content)
			require.ErrorIs(t, err, ErrUnsupportedAgentMessage)
		})
	}
}

func TestNormalizeAgentMessageContent_StringAndEmptyContent(t *testing.T) {
	normalized, err := NormalizeAgentMessageContent(json.RawMessage(`"plain task"`))
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"input_text","text":"plain task"}]`, string(normalized))

	normalized, err = NormalizeAgentMessageContent(json.RawMessage(`[]`))
	require.NoError(t, err)
	require.Empty(t, normalized)

	normalized, err = NormalizeAgentMessageContent(nil)
	require.NoError(t, err)
	require.Empty(t, normalized)
}
