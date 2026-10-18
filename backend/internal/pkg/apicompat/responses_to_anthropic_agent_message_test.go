package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesToAnthropicRequest_AgentMessageBecomesUserMessage(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Parent context."}]},
			{"type":"agent_message","id":"amsg_1","author":"/root","recipient":"/root/gpt_task","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/gpt_task\nSender: /root\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"Implement the requested change."}
			]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I will inspect the repository."}]}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)

	require.Equal(t, "user", out.Messages[0].Role)
	var userBlocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[0].Content, &userBlocks))
	require.Len(t, userBlocks, 3)
	require.Equal(t, "Parent context.", userBlocks[0].Text)
	require.Equal(t, "Message Type: NEW_TASK\nTask name: /root/gpt_task\nSender: /root\nPayload:\n", userBlocks[1].Text)
	require.Equal(t, "Implement the requested change.", userBlocks[2].Text)

	require.Equal(t, "assistant", out.Messages[1].Role)
	var assistantBlocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[1].Content, &assistantBlocks))
	require.Len(t, assistantBlocks, 1)
	require.Equal(t, "I will inspect the repository.", assistantBlocks[0].Text)
}

func TestResponsesToAnthropicRequest_AgentMessagePreservesImageWithoutText(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"agent_message","author":"/root","recipient":"/root/child","content":[]},
			{"type":"agent_message","author":"/root","recipient":"/root/child","content":[
				{"type":"input_image","image_url":"data:image/png;base64,AA=="}
			]}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 1)
	require.Equal(t, "user", out.Messages[0].Role)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[0].Content, &blocks))
	require.Len(t, blocks, 1)
	require.Equal(t, "image", blocks[0].Type)
	require.Equal(t, "image/png", blocks[0].Source.MediaType)
	require.Equal(t, "AA==", blocks[0].Source.Data)
}

func TestResponsesToAnthropicRequest_AgentMessagePreservesTextAndMediaOrder(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-4-20250514",
		Input: json.RawMessage(`[
			{"type":"agent_message","author":"/root","recipient":"/root/child","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
				{"type":"input_image","image_url":{"url":"data:image/png;base64,AA=="}},
				{"type":"encrypted_content","encrypted_content":"Read the image."},
				{"type":"input_file","filename":"brief.pdf","file_data":"data:application/pdf;base64,JVBERi0="},
				{"type":"input_text","text":" End task."}
			]}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 1)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[0].Content, &blocks))
	require.Len(t, blocks, 5)
	require.Equal(t, "text", blocks[0].Type)
	require.Equal(t, "Message Type: NEW_TASK\nPayload:\n", blocks[0].Text)
	require.Equal(t, "image", blocks[1].Type)
	require.Equal(t, "image/png", blocks[1].Source.MediaType)
	require.Equal(t, "text", blocks[2].Type)
	require.Equal(t, "Read the image.", blocks[2].Text)
	require.Equal(t, "document", blocks[3].Type)
	require.Equal(t, "application/pdf", blocks[3].Source.MediaType)
	require.Equal(t, "JVBERi0=", blocks[3].Source.Data)
	require.Equal(t, "text", blocks[4].Type)
	require.Equal(t, " End task.", blocks[4].Text)
}

func TestResponsesToAnthropicRequest_AgentMessageRejectsUnrecoverableContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{
			name: "opaque ciphertext",
			input: `[{"type":"agent_message","content":[
				{"type":"input_text","text":"Message Type: NEW_TASK\nPayload:\n"},
				{"type":"encrypted_content","encrypted_content":"gAAAAABopaque-payload"}
			]}]`,
			want: ErrOpaqueAgentMessageContent,
		},
		{
			name: "unclassified encrypted content",
			input: `[{"type":"agent_message","content":[
				{"type":"encrypted_content","encrypted_content":"Do the task."}
			]}]`,
			want: ErrUnconfirmedAgentMessage,
		},
		{
			name: "file id without file data",
			input: `[{"type":"agent_message","content":[
				{"type":"input_text","text":"Read this file."},
				{"type":"input_file","file_id":"file_123"}
			]}]`,
			want: ErrUnsupportedAgentMessage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResponsesToAnthropicRequest(&ResponsesRequest{
				Model: "claude-sonnet-4-20250514",
				Input: json.RawMessage(test.input),
			})
			require.ErrorIs(t, err, test.want)
		})
	}
}
