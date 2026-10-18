package service

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationResponsesAgentMessageAuditsTaskAsUser(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"earlier request"}]},
			{"type":"agent_message","author":"/root","recipient":"/root/task","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/task\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"Review the attached document."}
			]}
		]
	}`)
	original := bytes.Clone(body)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, original, body)
	require.Contains(t, input.Text, "Message Type: NEW_TASK")
	require.Contains(t, input.Text, "Review the attached document.")
	require.NotContains(t, input.Text, "earlier request")

	keywordText := extractContentModerationKeywordText(ContentModerationProtocolOpenAIResponses, body)
	require.Contains(t, keywordText, "Message Type: NEW_TASK")
	require.Contains(t, keywordText, "Review the attached document.")
	require.NotContains(t, keywordText, "earlier request")
}

func TestContentModerationResponsesOpaqueAgentMessageAuditsVisibleEnvelope(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"agent_message","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-agent-payload"}
			]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "Message Type: NEW_TASK")
	require.NotContains(t, input.Text, "gAAAAABopaque-agent-payload")
}

func TestContentModerationResponsesDoesNotTreatGenericEncryptedContentAsText(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"reasoning","encrypted_content":"GENERIC_ENCRYPTED_SECRET"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}
