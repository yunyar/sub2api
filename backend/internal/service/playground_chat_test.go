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
	body := []byte(`{"model":"alias","messages":[{"role":"user","content":"What time is it?"}]}`)
	ctx := WithPlaygroundChat(context.Background(), "Asia/Shanghai")
	settings := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	settings.now = time.Date(2026, time.November, 23, 15, 9, 0, 0, time.UTC)
	ctx = context.WithValue(ctx, playgroundChatKey{}, settings)

	updated := withPlaygroundLocalTime(ctx, body, "gpt-5.5")
	require.Contains(t, gjson.GetBytes(updated, "messages.0.content").String(), "2026-11-23 23:09:00 +08:00 (Asia/Shanghai)")
	require.Equal(t, "What time is it?", gjson.GetBytes(updated, "messages.1.content").String())
	require.Equal(t, body, withPlaygroundLocalTime(ctx, body, "alias", "anthropic/claude-opus-4-8"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChatModel(ctx, "claude-alias"), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(context.Background(), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChat(context.Background(), "Local"), body, "gpt-5.5"))
	require.Equal(t, body, withPlaygroundLocalTime(WithPlaygroundChat(context.Background(), "Invalid/Zone"), body, "gpt-5.5"))
}

func TestPlaygroundSearchFollowupHasIndependentBillingID(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "playground-turn")
	initial := resolveUsageBillingRequestID(ctx, "initial-upstream")
	followup := PlaygroundSearchFollowupBillingID(ctx, "initial-upstream")
	require.NotEqual(t, initial, followup)
	require.Equal(t, followup, resolveUsageBillingRequestID(ctx, followup))
	require.Equal(t, followup, PlaygroundSearchFollowupBillingID(ctx, "another-upstream"))
}
