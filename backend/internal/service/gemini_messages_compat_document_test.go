//go:build unit

package service

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertClaudeMessagesToGeminiGenerateContentPreservesDocumentAttachments(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Compare the document and image."},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0="}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}},{"type":"text","text":"Report the differences."}]}]}`)
	original := bytes.Clone(body)

	converted, err := convertClaudeMessagesToGeminiGenerateContent(body)

	require.NoError(t, err)
	require.Equal(t, original, body)
	require.Equal(t, int64(4), gjson.GetBytes(converted, "contents.0.parts.#").Int())
	require.Equal(t, "Compare the document and image.", gjson.GetBytes(converted, "contents.0.parts.0.text").String())
	require.Equal(t, "application/pdf", gjson.GetBytes(converted, "contents.0.parts.1.inlineData.mimeType").String())
	require.Equal(t, "JVBERi0=", gjson.GetBytes(converted, "contents.0.parts.1.inlineData.data").String())
	require.False(t, gjson.GetBytes(converted, "contents.0.parts.1.text").Exists())
	require.Equal(t, "image/png", gjson.GetBytes(converted, "contents.0.parts.2.inlineData.mimeType").String())
	require.Equal(t, "AA==", gjson.GetBytes(converted, "contents.0.parts.2.inlineData.data").String())
	require.Equal(t, "Report the differences.", gjson.GetBytes(converted, "contents.0.parts.3.text").String())
}
