package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentOrchestrationCompletionAudit_FlattenedNamespaceDescription(t *testing.T) {
	const namespaceDescription = "Spawn child agents, wait until each reports completed, then consolidate results before responding."
	const spawnSchema = `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}`
	const waitSchema = `{"type":"object","properties":{"agent_id":{"type":"string"}},"required":["agent_id"],"additionalProperties":false}`
	const closeSchema = `{"type":"object","properties":{"agent_id":{"type":"string"}},"required":["agent_id"],"additionalProperties":false}`
	const spawnDescription = "Start a child task."
	const waitDescription = "Poll a child task until it completes."

	request := map[string]any{
		"parallel_tool_calls": false,
		"tool_choice": map[string]any{
			"type":      "function",
			"name":      "wait_agent",
			"namespace": "multi_agent_v1",
		},
		"tools": []any{
			map[string]any{
				"type":        "namespace",
				"name":        "multi_agent_v1",
				"description": namespaceDescription,
				"tools": []any{
					map[string]any{"type": "function", "name": "spawn_agent", "description": spawnDescription, "parameters": json.RawMessage(spawnSchema)},
					map[string]any{"type": "function", "name": "wait_agent", "description": waitDescription, "parameters": json.RawMessage(waitSchema)},
					map[string]any{"type": "function", "name": "close_agent", "parameters": json.RawMessage(closeSchema)},
				},
			},
		},
	}

	mapping, changed, err := AdaptResponsesClientTools(request)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, map[string]ResponsesNamespaceName{
		"multi_agent_v1__spawn_agent": {Namespace: "multi_agent_v1", Name: "spawn_agent"},
		"multi_agent_v1__wait_agent":  {Namespace: "multi_agent_v1", Name: "wait_agent"},
		"multi_agent_v1__close_agent": {Namespace: "multi_agent_v1", Name: "close_agent"},
	}, mapping.NamespaceTools)

	tools, ok := request["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 3)
	wantSchemas := map[string]string{
		"multi_agent_v1__spawn_agent": spawnSchema,
		"multi_agent_v1__wait_agent":  waitSchema,
		"multi_agent_v1__close_agent": closeSchema,
	}
	wantDescriptions := map[string]string{
		"multi_agent_v1__spawn_agent": namespaceDescription + "\n\n" + spawnDescription,
		"multi_agent_v1__wait_agent":  namespaceDescription + "\n\n" + waitDescription,
		"multi_agent_v1__close_agent": namespaceDescription,
	}
	var missingDescriptions []string
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		require.True(t, ok)
		name, ok := tool["name"].(string)
		require.True(t, ok)
		require.Equal(t, "function", tool["type"])
		require.JSONEq(t, wantSchemas[name], string(mustJSON(t, tool["parameters"])))
		require.Equal(t, wantDescriptions[name], tool["description"])
		description, _ := tool["description"].(string)
		if description == "" || !strings.Contains(description, namespaceDescription) {
			missingDescriptions = append(missingDescriptions, name)
		}
	}
	require.Empty(t, missingDescriptions, "namespace orchestration guidance must survive flattening on every child tool")

	choice, ok := request["tool_choice"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "multi_agent_v1__wait_agent", choice["name"])
	require.NotContains(t, choice, "namespace")
	require.Equal(t, false, request["parallel_tool_calls"])
}

func TestAgentOrchestrationCompletionAudit_FlattenedChildrenAndEmptyDescription(t *testing.T) {
	const namespaceDescription = "Wait for all child results before consolidating."
	const childDescription = "Return the child status."
	const schema = `{"type":"object","properties":{},"additionalProperties":false}`

	tests := []struct {
		name             string
		field            string
		namespaceDesc    string
		childDesc        string
		wantDescription  string
		wantDescription2 bool
	}{
		{
			name:            "children inherits namespace and child description",
			field:           "children",
			namespaceDesc:   namespaceDescription,
			childDesc:       childDescription,
			wantDescription: namespaceDescription + "\n\n" + childDescription,
		},
		{
			name:            "empty namespace description preserves child description",
			field:           "tools",
			childDesc:       childDescription,
			wantDescription: childDescription,
		},
		{
			name:             "empty descriptions stay absent",
			field:            "children",
			wantDescription2: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			child := map[string]any{"type": "function", "name": "wait_agent", "parameters": json.RawMessage(schema)}
			if test.childDesc != "" {
				child["description"] = test.childDesc
			}
			namespace := map[string]any{"type": "namespace", "name": "multi_agent_v1", test.field: []any{child}}
			if test.namespaceDesc != "" {
				namespace["description"] = test.namespaceDesc
			}
			request := map[string]any{"tools": []any{namespace}}

			_, changed, err := AdaptResponsesClientTools(request)
			require.NoError(t, err)
			require.True(t, changed)
			tools, ok := request["tools"].([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			flattened, ok := tools[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "multi_agent_v1__wait_agent", flattened["name"])
			require.JSONEq(t, schema, string(mustJSON(t, flattened["parameters"])))
			if test.wantDescription2 {
				require.NotContains(t, flattened, "description")
				return
			}
			require.Equal(t, test.wantDescription, flattened["description"])
		})
	}
}

func TestAgentOrchestrationCompletionAudit_AnthropicBridgeKeepsDescriptions(t *testing.T) {
	const namespaceDescription = "Wait for every requested child result before the parent answer."
	const childDescription = "Wait for one child."
	const schema = `{"type":"object","properties":{"agent_id":{"type":"string"}},"required":["agent_id"],"additionalProperties":false}`

	request := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"tools": []any{map[string]any{
			"type": "namespace", "name": "multi_agent_v1", "description": namespaceDescription,
			"children": []any{map[string]any{
				"type": "function", "name": "wait_agent", "description": childDescription,
				"parameters": json.RawMessage(schema),
			}},
		}},
		"input": []any{map[string]any{"type": "message", "role": "user", "content": "wait"}},
	}

	_, changed, err := AdaptResponsesClientTools(request)
	require.NoError(t, err)
	require.True(t, changed)
	body, err := json.Marshal(request)
	require.NoError(t, err)
	var responsesRequest ResponsesRequest
	require.NoError(t, json.Unmarshal(body, &responsesRequest))
	anthropicRequest, err := ResponsesToAnthropicRequest(&responsesRequest)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Tools, 1)
	require.Equal(t, "multi_agent_v1__wait_agent", anthropicRequest.Tools[0].Name)
	require.Equal(t, namespaceDescription+"\n\n"+childDescription, anthropicRequest.Tools[0].Description)
	require.JSONEq(t, schema, string(anthropicRequest.Tools[0].InputSchema))
}

func TestAgentOrchestrationCompletionAudit_AnthropicBridgePreservesChildOnlyDescription(t *testing.T) {
	const childDescription = "Wait for one child."

	requestBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"input": "wait",
		"tools": []any{map[string]any{
			"type": "namespace", "name": "multi_agent_v1",
			"children": []any{map[string]any{"type": "function", "name": "wait_agent", "description": childDescription}},
		}},
	}

	_, changed, err := AdaptResponsesClientTools(requestBody)
	require.True(t, changed)
	require.NoError(t, err)
	body, err := json.Marshal(requestBody)
	require.NoError(t, err)
	var request ResponsesRequest
	require.NoError(t, json.Unmarshal(body, &request))
	anthropicRequest, err := ResponsesToAnthropicRequest(&request)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Tools, 1)
	require.Equal(t, childDescription, anthropicRequest.Tools[0].Description)
}

func TestAgentOrchestrationCompletionAudit_ChatBridgeKeepsDescriptions(t *testing.T) {
	const namespaceDescription = "Wait for all requested child results before consolidating."
	const childDescription = "Wait for one child."
	const schema = `{"type":"object","properties":{"agent_id":{"type":"string"}},"required":["agent_id"],"additionalProperties":false}`

	for _, childrenField := range []string{"tools", "children"} {
		t.Run(childrenField, func(t *testing.T) {
			child := ResponsesTool{
				Type:        "function",
				Name:        "wait_agent",
				Description: childDescription,
				Parameters:  json.RawMessage(schema),
			}
			namespace := ResponsesTool{
				Type:        "namespace",
				Name:        "multi_agent_v1",
				Description: namespaceDescription,
			}
			if childrenField == "tools" {
				namespace.Tools = []ResponsesTool{child}
			} else {
				namespace.Children = []ResponsesTool{child}
			}
			parallel := false
			request := &ResponsesRequest{Model: "gpt-5.5", Input: json.RawMessage(`"wait"`), Tools: []ResponsesTool{namespace}, ParallelToolCalls: &parallel}

			chatRequest, err := ResponsesToChatCompletionsRequest(request)
			require.NoError(t, err)
			require.Len(t, chatRequest.Tools, 1)
			require.Equal(t, "multi_agent_v1__wait_agent", chatRequest.Tools[0].Function.Name)
			require.Equal(t, namespaceDescription+"\n\n"+childDescription, chatRequest.Tools[0].Function.Description)
			require.JSONEq(t, schema, string(chatRequest.Tools[0].Function.Parameters))
			require.NotNil(t, chatRequest.ParallelToolCalls)
			require.False(t, *chatRequest.ParallelToolCalls)
		})
	}
}

func TestAgentOrchestrationCompletionAudit_ChatBridgePreservesChildOnlyDescription(t *testing.T) {
	const childDescription = "Wait for one child."

	request := &ResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`"wait"`),
		Tools: []ResponsesTool{{
			Type:  "namespace",
			Name:  "multi_agent_v1",
			Tools: []ResponsesTool{{Type: "function", Name: "wait_agent", Description: childDescription}},
		}},
	}

	chatRequest, err := ResponsesToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Len(t, chatRequest.Tools, 1)
	require.Equal(t, childDescription, chatRequest.Tools[0].Function.Description)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
