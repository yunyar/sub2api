package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func normalizeOpenAIResponsesAgentMessages(body []byte) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}
	if !json.Valid(body) {
		return body, false, fmt.Errorf("normalize OpenAI Responses agent messages: invalid JSON")
	}

	input := gjson.GetBytes(body, "input")
	if input.IsObject() {
		normalized, changed, err := normalizeOpenAIResponsesAgentMessage([]byte(input.Raw))
		if err != nil || !changed {
			return body, false, err
		}
		updated, err := sjson.SetRawBytes(body, "input", normalized)
		if err != nil {
			return body, false, fmt.Errorf("replace OpenAI Responses agent message: %w", err)
		}
		return updated, true, nil
	}
	if !input.IsArray() {
		return body, false, nil
	}

	inputRaw := []byte(input.Raw)
	var rebuilt bytes.Buffer
	copyFrom := 0
	changed := false
	var normalizeErr error
	input.ForEach(func(_, item gjson.Result) bool {
		normalized, itemChanged, err := normalizeOpenAIResponsesAgentMessage([]byte(item.Raw))
		if err != nil {
			normalizeErr = err
			return false
		}
		if !itemChanged {
			return true
		}

		itemStart := item.Index - input.Index
		itemEnd := itemStart + len(item.Raw)
		if itemStart < copyFrom || itemEnd > len(inputRaw) {
			normalizeErr = fmt.Errorf("normalize OpenAI Responses agent messages: invalid input item offsets")
			return false
		}
		if !changed {
			rebuilt.Grow(len(inputRaw))
			changed = true
		}
		_, _ = rebuilt.Write(inputRaw[copyFrom:itemStart])
		_, _ = rebuilt.Write(normalized)
		copyFrom = itemEnd
		return true
	})
	if normalizeErr != nil {
		return body, false, normalizeErr
	}
	if !changed {
		return body, false, nil
	}
	_, _ = rebuilt.Write(inputRaw[copyFrom:])

	updated, err := sjson.SetRawBytes(body, "input", rebuilt.Bytes())
	if err != nil {
		return body, false, fmt.Errorf("replace OpenAI Responses agent messages: %w", err)
	}
	return updated, true, nil
}

func normalizeOpenAIResponsesAgentMessage(itemRaw []byte) ([]byte, bool, error) {
	itemType := gjson.GetBytes(itemRaw, "type").String()
	if itemType == "function_call_output" || itemType == "custom_tool_call_output" {
		outputRaw := gjson.GetBytes(itemRaw, "output")
		content, changed, err := apicompat.NormalizeAgentFunctionOutputContent(json.RawMessage(outputRaw.Raw))
		if err != nil || !changed {
			return itemRaw, false, nil
		}
		normalized, err := sjson.SetRawBytes(itemRaw, "output", content)
		if err != nil {
			return itemRaw, false, fmt.Errorf("normalize OpenAI agent function output: %w", err)
		}
		return normalized, true, nil
	}
	if itemType != "agent_message" {
		return itemRaw, false, nil
	}

	contentRaw := gjson.GetBytes(itemRaw, "content")
	if !contentRaw.Exists() || contentRaw.Type == gjson.Null {
		return itemRaw, false, nil
	}

	content, err := apicompat.NormalizeAgentMessageContent(json.RawMessage(contentRaw.Raw))
	if err != nil || len(content) == 0 {
		return itemRaw, false, nil
	}
	normalized, err := json.Marshal(struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}{Type: "message", Role: "user", Content: content})
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}
