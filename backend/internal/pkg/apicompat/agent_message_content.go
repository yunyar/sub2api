package apicompat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrOpaqueAgentMessageContent = errors.New("agent_message contains opaque encrypted content")
	ErrUnconfirmedAgentMessage   = errors.New("agent_message encrypted content is not confirmed plaintext")
	ErrUnsupportedAgentMessage   = errors.New("agent_message contains unsupported content")
)

func NormalizeAgentMessageContent(content json.RawMessage) (json.RawMessage, error) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return nil, nil
	}

	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return json.Marshal([]map[string]string{{"type": "input_text", "text": text}})
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(content, &rawParts); err != nil {
		return nil, fmt.Errorf("parse agent_message content: %w", err)
	}
	if len(rawParts) == 0 {
		return nil, nil
	}

	normalized := make([]json.RawMessage, 0, len(rawParts))
	var envelope strings.Builder
	var encryptedParts []string
	encryptedIndexes := make([]int, 0, 1)
	for index, rawPart := range rawParts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(rawPart, &part); err != nil || part == nil {
			return nil, fmt.Errorf("%w: invalid part at index %d", ErrUnsupportedAgentMessage, index)
		}
		var partType string
		if err := json.Unmarshal(part["type"], &partType); err != nil || partType == "" {
			return nil, fmt.Errorf("%w: missing type at index %d", ErrUnsupportedAgentMessage, index)
		}

		switch partType {
		case "input_text", "text":
			var partText string
			if err := json.Unmarshal(part["text"], &partText); err != nil {
				return nil, fmt.Errorf("parse agent_message text part %d: expected string", index)
			}
			_, _ = envelope.WriteString(partText)
			normalizedPart := rawPart
			if partType == "text" {
				part["type"] = json.RawMessage(`"input_text"`)
				var err error
				normalizedPart, err = json.Marshal(part)
				if err != nil {
					return nil, fmt.Errorf("normalize agent_message text part %d: %w", index, err)
				}
			}
			normalized = append(normalized, normalizedPart)
		case "encrypted_content":
			var encrypted string
			if err := json.Unmarshal(part["encrypted_content"], &encrypted); err != nil || encrypted == "" {
				return nil, fmt.Errorf("%w: invalid encrypted part at index %d", ErrUnconfirmedAgentMessage, index)
			}
			if agentMessageLooksOpaqueCiphertext(encrypted) {
				return nil, fmt.Errorf("%w at part %d", ErrOpaqueAgentMessageContent, index)
			}
			encryptedParts = append(encryptedParts, encrypted)
			encryptedIndexes = append(encryptedIndexes, len(normalized))
			normalized = append(normalized, nil)
		case "input_image", "image_url":
			imageURL, metadata, err := agentMessageImagePart(part["image_url"])
			if err != nil {
				return nil, fmt.Errorf("%w: image part %d: %v", ErrUnsupportedAgentMessage, index, err)
			}
			if strings.TrimSpace(imageURL) == "" {
				return nil, fmt.Errorf("%w: image part %d has no image_url", ErrUnsupportedAgentMessage, index)
			}
			if partType == "image_url" {
				part["type"] = json.RawMessage(`"input_image"`)
			}
			imageURLJSON, err := json.Marshal(imageURL)
			if err != nil {
				return nil, fmt.Errorf("normalize agent_message image part %d URL: %w", index, err)
			}
			part["image_url"] = imageURLJSON
			for key, value := range metadata {
				if _, exists := part[key]; !exists {
					part[key] = value
				}
			}
			normalizedPart, err := json.Marshal(part)
			if err != nil {
				return nil, fmt.Errorf("normalize agent_message image part %d: %w", index, err)
			}
			normalized = append(normalized, normalizedPart)
		case "input_file":
			var fileData, fileID string
			_ = json.Unmarshal(part["file_data"], &fileData)
			_ = json.Unmarshal(part["file_id"], &fileID)
			if strings.TrimSpace(fileData) == "" && strings.TrimSpace(fileID) == "" {
				return nil, fmt.Errorf("%w: file part %d has no file_data or file_id", ErrUnsupportedAgentMessage, index)
			}
			normalized = append(normalized, rawPart)
		default:
			return nil, fmt.Errorf("%w: part %d has type %q", ErrUnsupportedAgentMessage, index, partType)
		}
	}

	if len(encryptedParts) > 0 {
		envelopeText := envelope.String()
		if !strings.Contains(envelopeText, "Message Type:") || !strings.Contains(envelopeText, "Payload:") {
			return nil, ErrUnconfirmedAgentMessage
		}
		for index, encrypted := range encryptedParts {
			part, err := json.Marshal(map[string]string{"type": "input_text", "text": encrypted})
			if err != nil {
				return nil, fmt.Errorf("normalize agent_message encrypted part: %w", err)
			}
			normalized[encryptedIndexes[index]] = part
		}
	}

	return json.Marshal(normalized)
}

func agentMessageImagePart(raw json.RawMessage) (string, map[string]json.RawMessage, error) {
	var imageURL string
	if err := json.Unmarshal(raw, &imageURL); err == nil {
		return imageURL, nil, nil
	}
	var imageObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &imageObject); err != nil || imageObject == nil {
		return "", nil, fmt.Errorf("image_url must be a string or object")
	}
	if err := json.Unmarshal(imageObject["url"], &imageURL); err != nil {
		return "", nil, fmt.Errorf("image_url.url must be a string")
	}
	delete(imageObject, "url")
	return imageURL, imageObject, nil
}

func agentMessageImageURL(raw json.RawMessage) string {
	imageURL, _, _ := agentMessageImagePart(raw)
	return imageURL
}

func agentMessageLooksOpaqueCiphertext(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "gAAAA") || strings.HasPrefix(value, "anthropic-thinking-v1:") {
		return true
	}
	if len(value) < 40 || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawStdEncoding,
		base64.RawURLEncoding,
		base64.StdEncoding,
		base64.URLEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) >= 24 {
			return true
		}
	}
	return false
}
