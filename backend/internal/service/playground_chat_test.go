package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPlaygroundChatLocalTime(t *testing.T) {
	body := []byte(`{"model":"alias","messages":[{"role":"system","content":"Static instructions"},{"role":"user","content":"Earlier question"},{"role":"assistant","content":"Earlier answer"},{"role":"user","content":"What time is it?"}]}`)
	ctx := WithPlaygroundChat(context.Background(), "Asia/Shanghai")
	settings, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	require.True(t, ok)
	settings.now = time.Date(2026, time.November, 23, 15, 9, 0, 0, time.UTC)
	ctx = context.WithValue(ctx, playgroundChatKey{}, settings)

	updated := withPlaygroundLocalTime(ctx, body, "gpt-5.5")
	require.Contains(t, gjson.GetBytes(updated, "messages.3.content.1.text").String(), "2026-11-23 23:09:00 +08:00 (Asia/Shanghai)")
	require.Equal(t, "What time is it?", gjson.GetBytes(updated, "messages.3.content.0.text").String())
	settings.now = settings.now.Add(time.Second)
	later := withPlaygroundLocalTime(context.WithValue(ctx, playgroundChatKey{}, settings), body, "gpt-5.5")
	require.NotEqual(t, string(updated), string(later))
	for _, path := range []string{"messages.0", "messages.1", "messages.2"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(updated, path).Raw)
		require.Equal(t, gjson.GetBytes(updated, path).Raw, gjson.GetBytes(later, path).Raw)
	}
	require.Equal(t, body, withPlaygroundLocalTime(ctx, body, "alias", "anthropic/claude-opus-4-8"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChatModel(ctx, "claude-alias"), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(context.Background(), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChat(context.Background(), "Local"), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChat(context.Background(), "Invalid/Zone"), body, "gpt-5.5"))
}

func TestPlaygroundChatSessionKey(t *testing.T) {
	ctx := WithPlaygroundChatConversation(WithPlaygroundChat(context.Background(), ""), "chat-1")
	ctx = WithPlaygroundChatModel(ctx, "gpt")
	key := PlaygroundChatSessionKey(ctx, 42)
	require.Len(t, key, 64)
	require.Equal(t, key, PlaygroundChatSessionKey(ctx, 42))
	require.NotEqual(t, key, PlaygroundChatSessionKey(ctx, 43))
	require.NotEqual(t, key, PlaygroundChatSessionKey(WithPlaygroundChatModel(ctx, "other-model"), 42))
	require.NotEqual(t, key, PlaygroundChatSessionKey(WithPlaygroundChatConversation(ctx, "chat-2"), 42))
	require.Empty(t, PlaygroundChatSessionKey(WithPlaygroundChatConversation(context.Background(), "chat-1"), 42))
	require.Empty(t, PlaygroundChatSessionKey(WithPlaygroundChatConversation(WithPlaygroundChat(context.Background(), ""), "invalid id"), 42))
}

func TestPlaygroundSearchFollowupHasIndependentBillingID(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "playground-turn")
	initial := resolveUsageBillingRequestID(ctx, "initial-upstream")
	followup := PlaygroundSearchFollowupBillingID(ctx, "initial-upstream")
	require.NotEqual(t, initial, followup)
	require.Equal(t, followup, resolveUsageBillingRequestID(ctx, followup))
	require.Equal(t, followup, PlaygroundSearchFollowupBillingID(ctx, "another-upstream"))
}
