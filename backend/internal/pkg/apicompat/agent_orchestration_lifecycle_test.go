package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentOrchestrationLifecycle_ToolHandoffCompletesBeforeChildResult(t *testing.T) {
	const arguments = `{"task":"inspect the next phase"}`

	responsesToAnthropicState := NewResponsesEventToAnthropicState()
	anthropicToResponsesState := NewAnthropicEventToResponsesState()

	input := []*ResponsesStreamEvent{
		{
			Type:     "response.created",
			Response: &ResponsesResponse{ID: "resp_parent", Model: "claude-opus-5-5"},
		},
		{
			Type:        "response.output_item.added",
			OutputIndex: 0,
			Item: &ResponsesOutput{
				Type:   "function_call",
				ID:     "item_spawn",
				CallID: "call_spawn",
				Name:   "spawn_agent",
			},
		},
		{
			Type:        "response.function_call_arguments.delta",
			OutputIndex: 0,
			Delta:       arguments,
		},
		{
			Type:        "response.function_call_arguments.done",
			OutputIndex: 0,
			Arguments:   arguments,
		},
		{
			Type:        "response.output_item.done",
			OutputIndex: 0,
			Item: &ResponsesOutput{
				Type:      "function_call",
				ID:        "item_spawn",
				CallID:    "call_spawn",
				Name:      "spawn_agent",
				Arguments: arguments,
				Status:    "completed",
			},
		},
		{
			Type: "response.completed",
			Response: &ResponsesResponse{
				ID:     "resp_parent",
				Model:  "claude-opus-5-5",
				Status: "completed",
				Output: []ResponsesOutput{{
					Type:      "function_call",
					ID:        "item_spawn",
					CallID:    "call_spawn",
					Name:      "spawn_agent",
					Arguments: arguments,
					Status:    "completed",
				}},
			},
		},
	}

	var anthropicEvents []AnthropicStreamEvent
	var returnedEvents []ResponsesStreamEvent
	for _, event := range input {
		for _, converted := range ResponsesEventToAnthropicEvents(event, responsesToAnthropicState) {
			anthropicEvents = append(anthropicEvents, converted)
			returnedEvents = append(returnedEvents, AnthropicEventToResponsesEvents(&converted, anthropicToResponsesState)...)
		}
	}

	var stopReason string
	messageStops := 0
	for _, event := range anthropicEvents {
		if event.Type == "message_delta" && event.Delta != nil {
			stopReason = event.Delta.StopReason
		}
		if event.Type == "message_stop" {
			messageStops++
		}
	}
	require.Equal(t, "tool_use", stopReason)
	require.Equal(t, 1, messageStops)

	terminalEvents := 0
	var completed *ResponsesStreamEvent
	for i := range returnedEvents {
		switch returnedEvents[i].Type {
		case "response.completed", "response.incomplete", "response.failed":
			terminalEvents++
			completed = &returnedEvents[i]
		}
	}
	require.Equal(t, 1, terminalEvents, "one Anthropic message_stop must produce exactly one Responses terminal")
	require.NotNil(t, completed)
	require.Equal(t, "response.completed", completed.Type)
	require.NotNil(t, completed.Response)
	require.Equal(t, "completed", completed.Response.Status)
	require.Len(t, completed.Response.Output, 1)
	require.Equal(t, "function_call", completed.Response.Output[0].Type)
	require.Equal(t, "spawn_agent", completed.Response.Output[0].Name)
	require.Equal(t, arguments, completed.Response.Output[0].Arguments)
	require.Empty(t, FinalizeResponsesAnthropicStream(responsesToAnthropicState), "a completed parent turn must not be finalized twice")
	require.Empty(t, FinalizeAnthropicResponsesStream(anthropicToResponsesState), "the returned terminal must not be duplicated at EOF")
}

func TestAgentOrchestrationLifecycle_PartialAnthropicEOFIsNotCompleted(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	blockIndex := 0
	events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:    "message_start",
		Message: &AnthropicResponse{ID: "msg_partial", Model: "claude-opus-5-5", Role: "assistant"},
	}, state)
	events = append(events, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:         "content_block_start",
		Index:        &blockIndex,
		ContentBlock: &AnthropicContentBlock{Type: "text"},
	}, state)...)
	events = append(events, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &blockIndex,
		Delta: &AnthropicDelta{Type: "text_delta", Text: "partial answer"},
	}, state)...)

	events = append(events, FinalizeAnthropicResponsesStream(state)...)

	var terminal *ResponsesStreamEvent
	for i := range events {
		switch events[i].Type {
		case "response.completed", "response.incomplete", "response.failed":
			require.Nil(t, terminal, "EOF must not produce multiple terminal events")
			terminal = &events[i]
		}
	}
	require.NotNil(t, terminal, "EOF must be explicitly terminated for the Responses client")
	require.NotEqual(t, "response.completed", terminal.Type, "partial output without an Anthropic stop_reason must not appear successful")
	require.Empty(t, FinalizeAnthropicResponsesStream(state), "EOF finalization must remain idempotent")
}

func TestAgentOrchestrationLifecycle_PreservesResponsesMessagePhaseOnWireRoundTrip(t *testing.T) {
	var input []ResponsesInputItem
	err := json.Unmarshal([]byte(`[
		{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"input_text","text":"Checking the child task."}]},
		{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"input_text","text":"The parent turn is complete."}]}
	]`), &input)
	require.NoError(t, err)
	encodedInput, err := json.Marshal(input)
	require.NoError(t, err)

	var inputWire []struct {
		Phase string `json:"phase"`
	}
	require.NoError(t, json.Unmarshal(encodedInput, &inputWire))
	require.Len(t, inputWire, 2)
	require.Equal(t, []string{"commentary", "final_answer"}, []string{
		inputWire[0].Phase,
		inputWire[1].Phase,
	})

	var response ResponsesResponse
	err = json.Unmarshal([]byte(`{
		"id":"resp_phases",
		"model":"claude-opus-5-5",
		"status":"completed",
		"output":[
			{"type":"message","role":"assistant","phase":"commentary","status":"completed","content":[{"type":"output_text","text":"Checking the child task."}]},
			{"type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"The parent turn is complete."}]}
		]
	}`), &response)
	require.NoError(t, err)
	encodedResponse, err := json.Marshal(response)
	require.NoError(t, err)

	var wire struct {
		Output []map[string]json.RawMessage `json:"output"`
	}
	require.NoError(t, json.Unmarshal(encodedResponse, &wire))
	require.Len(t, wire.Output, 2)

	var phases []string
	for _, item := range wire.Output {
		phaseRaw, exists := item["phase"]
		require.True(t, exists, "assistant message phase was dropped during the Responses wire decode/encode round trip")
		var phase string
		require.NoError(t, json.Unmarshal(phaseRaw, &phase))
		phases = append(phases, phase)
	}
	require.Equal(t, []string{"commentary", "final_answer"}, phases)
}
