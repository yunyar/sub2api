package apicompat

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testAgentMessageFernetToken() string {
	decoded := make([]byte, 73)
	decoded[0] = 0x80
	return base64.RawURLEncoding.EncodeToString(decoded)
}

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

func TestNormalizeAgentMessageContent_RejectsOpaqueDespitePlaintextEnvelope(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\nUse the portable task body."},
		{"type":"encrypted_content","encrypted_content":"` + testAgentMessageFernetToken() + `"}
	]`)

	_, err := NormalizeAgentMessageContent(content)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
}

func TestNormalizeAgentMessageContent_RejectsOpaqueWithHeadersOrPlaceholders(t *testing.T) {
	token := testAgentMessageFernetToken()
	tests := []struct {
		name    string
		content json.RawMessage
	}{
		{
			name: "pure header",
			content: json.RawMessage(`[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"` + token + `"}
			]`),
		},
		{
			name: "placeholder payload",
			content: json.RawMessage(`[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n[placeholder]"},
				{"type":"encrypted_content","encrypted_content":"` + token + `"}
			]`),
		},
		{
			name: "missing message type value",
			content: json.RawMessage(`[
				{"type":"input_text","text":"Message Type:\nPayload:\nThe task is unavailable."},
				{"type":"encrypted_content","encrypted_content":"` + token + `"}
			]`),
		},
		{
			name: "multiple opaque parts without plaintext",
			content: json.RawMessage(`[
				{"type":"input_text","text":"Message Type: STATUS_UPDATE\nPayload:\nPending"},
				{"type":"encrypted_content","encrypted_content":"` + token + `"},
				{"type":"encrypted_content","encrypted_content":"` + token + `"}
			]`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeAgentMessageContent(test.content)
			require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
		})
	}
}

func TestNormalizeAgentMessageContent_RejectsMultipleOpaquePartsDespitePlaintext(t *testing.T) {
	token := testAgentMessageFernetToken()
	content := json.RawMessage(`[
		{"type":"input_text","text":"Message Type: FINAL_ANSWER\nPayload:\nThe child completed the requested task."},
		{"type":"encrypted_content","encrypted_content":"` + token + `"},
		{"type":"encrypted_content","encrypted_content":"` + token + `"},
		{"type":"input_text","text":"Additional explicit context."}
	]`)

	_, err := NormalizeAgentMessageContent(content)
	require.ErrorIs(t, err, ErrOpaqueAgentMessageContent)
}

func TestNormalizeAgentMessageContent_DistinguishesReadableBase64FromBinaryCiphertext(t *testing.T) {
	plainText := strings.Repeat("Readable delegated context. ", 3)
	encodedText := base64.StdEncoding.EncodeToString([]byte(plainText))
	require.False(t, agentMessageLooksOpaqueCiphertext(encodedText))
	require.False(t, agentMessageLooksOpaqueCiphertext(plainText))

	decodedCiphertext := make([]byte, 48)
	for index := range decodedCiphertext {
		decodedCiphertext[index] = byte(index)
	}
	encodedCiphertext := base64.StdEncoding.EncodeToString(decodedCiphertext)
	require.True(t, agentMessageLooksOpaqueCiphertext(encodedCiphertext))

	content := json.RawMessage(`[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"` + encodedText + `"}]`)
	normalized, err := NormalizeAgentMessageContent(content)
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},{"type":"input_text","text":"`+encodedText+`"}]`, string(normalized))
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
