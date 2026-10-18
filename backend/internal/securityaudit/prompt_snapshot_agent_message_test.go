package securityaudit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptSnapshotAgentMessageIncludesPlainTaskAndPrioritizesIt(t *testing.T) {
	body := []byte(`{
		"instructions":"system instructions",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"earlier user request"}]},
			{"type":"agent_message","author":"/root","recipient":"/root/task","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/task\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"Inspect the image and report its dimensions."}
			]}
		]
	}`)
	original := bytes.Clone(body)

	snapshot, err := ExtractPromptSnapshot(Request{Protocol: "openai_responses", Body: body})
	require.NoError(t, err)
	require.Equal(t, original, body)
	require.Contains(t, snapshot.FullPrompt, "Message Type: NEW_TASK")
	require.Contains(t, snapshot.FullPrompt, "Inspect the image and report its dimensions.")
	require.Contains(t, snapshot.FullPrompt, "earlier user request")
	require.Less(
		t,
		strings.Index(snapshot.ScanText, "Inspect the image and report its dimensions."),
		strings.Index(snapshot.ScanText, "earlier user request"),
	)

	blocking, err := ExtractBlockingPromptSnapshot(Request{Protocol: "openai_responses", Body: body}, true)
	require.NoError(t, err)
	require.Contains(t, blocking.FullPrompt, "Inspect the image and report its dimensions.")
}

func TestPromptSnapshotWebsocketNestedAgentMessageIncludesPlainTask(t *testing.T) {
	body := []byte(`{
		"type":"response.create",
		"response":{
			"input":[
				{"type":"agent_message","content":[
					{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
					{"type":"encrypted_content","encrypted_content":"Summarize the attached report."}
				]}
			]
		}
	}`)
	original := bytes.Clone(body)

	snapshot, err := ExtractPromptSnapshot(Request{Protocol: "responses_websocket", Body: body})
	require.NoError(t, err)
	require.Equal(t, original, body)
	require.Contains(t, snapshot.FullPrompt, "Summarize the attached report.")
	require.Contains(t, snapshot.FullPrompt, "Message Type: NEW_TASK")
}

func TestPromptSnapshotOpaqueAgentMessageKeepsVisibleEnvelopeOnly(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"agent_message","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-agent-payload"}
			]}
		]
	}`)

	snapshot, err := ExtractPromptSnapshot(Request{Protocol: "openai_responses", Body: body})
	require.NoError(t, err)
	require.Contains(t, snapshot.FullPrompt, "Message Type: NEW_TASK")
	require.NotContains(t, snapshot.FullPrompt, "gAAAAABopaque-agent-payload")
}

func TestPromptSnapshotDoesNotTreatGenericEncryptedContentAsText(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":"visible request"},
			{"type":"reasoning","encrypted_content":"GENERIC_ENCRYPTED_SECRET"}
		]
	}`)

	snapshot, err := ExtractPromptSnapshot(Request{Protocol: "openai_responses", Body: body})
	require.NoError(t, err)
	require.Contains(t, snapshot.FullPrompt, "visible request")
	require.NotContains(t, snapshot.FullPrompt, "GENERIC_ENCRYPTED_SECRET")
}
