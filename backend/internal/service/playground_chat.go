package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type playgroundChatKey struct{}

var playgroundChatConversationID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type playgroundChatContext struct {
	location       *time.Location
	now            time.Time
	model          string
	conversationID string
}

func WithPlaygroundChat(ctx context.Context, timezone string) context.Context {
	var location *time.Location
	if len(timezone) <= 64 && timezone != "" && timezone != "Local" {
		location, _ = time.LoadLocation(timezone)
	}
	return context.WithValue(ctx, playgroundChatKey{}, playgroundChatContext{location: location, now: time.Now()})
}

func IsPlaygroundChat(ctx context.Context) bool {
	_, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	return ok
}

func WithPlaygroundChatConversation(ctx context.Context, id string) context.Context {
	settings, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	if !ok || !playgroundChatConversationID.MatchString(id) {
		return ctx
	}
	settings.conversationID = id
	return context.WithValue(ctx, playgroundChatKey{}, settings)
}

func PlaygroundChatSessionKey(ctx context.Context, apiKeyID int64) string {
	settings, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	if !ok || settings.conversationID == "" || apiKeyID <= 0 {
		return ""
	}
	seed := fmt.Sprintf("playground:%d:%s:%s", apiKeyID, settings.conversationID, settings.model)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))
}

func WithPlaygroundChatModel(ctx context.Context, model string) context.Context {
	settings, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	if !ok {
		return ctx
	}
	settings.model = model
	return context.WithValue(ctx, playgroundChatKey{}, settings)
}

func PlaygroundSearchFollowupBillingID(ctx context.Context, firstUpstreamID string) string {
	return fmt.Sprintf("playground-search-followup:%x", sha256.Sum256([]byte(resolveUsageBillingRequestID(ctx, firstUpstreamID))))
}

func withPlaygroundLocalTime(ctx context.Context, body []byte, models ...string) []byte {
	settings, ok := ctx.Value(playgroundChatKey{}).(playgroundChatContext)
	if !ok || settings.location == nil {
		return body
	}
	if strings.Contains(strings.ToLower(settings.model), "claude") {
		return body
	}
	for _, model := range models {
		if strings.Contains(strings.ToLower(model), "claude") {
			return body
		}
	}
	messages := gjson.GetBytes(body, "messages").Array()
	if len(messages) == 0 || messages[len(messages)-1].Get("role").String() != "user" {
		return body
	}
	local := settings.now.In(settings.location)
	timeText := "Current local time in the user's browser timezone at the start of this request: " + local.Format("Monday, 2006-01-02 15:04:05 -07:00") + " (" + settings.location.String() + ")."
	timePart, _ := json.Marshal(map[string]string{"type": "text", "text": timeText})
	path := fmt.Sprintf("messages.%d.content", len(messages)-1)
	content := messages[len(messages)-1].Get("content")
	if content.IsArray() {
		updated, err := sjson.SetRawBytes(body, path+".-1", timePart)
		if err == nil {
			return updated
		}
		return body
	}
	if content.Type != gjson.String {
		return body
	}
	parts, _ := json.Marshal([]map[string]string{
		{"type": "text", "text": content.String()},
		{"type": "text", "text": timeText},
	})
	updated, err := sjson.SetRawBytes(body, path, parts)
	if err != nil {
		return body
	}
	return updated
}

func (s *GatewayService) PlaygroundSearchEnabled(ctx context.Context) bool {
	return playgroundSearchEnabled(ctx, s.settingService)
}

func (s *OpenAIGatewayService) PlaygroundSearchEnabled(ctx context.Context) bool {
	return playgroundSearchEnabled(ctx, s.settingService)
}

func playgroundSearchEnabled(ctx context.Context, settings *SettingService) bool {
	return IsPlaygroundChat(ctx) && getWebSearchManager() != nil && settings != nil && settings.IsWebSearchEmulationEnabled(ctx)
}

func SearchPlaygroundWeb(ctx context.Context, account *Account, query string) ([]websearch.SearchResult, error) {
	response, _, err := doWebSearch(ctx, account, query)
	if err != nil || response == nil {
		return nil, err
	}
	results := make([]websearch.SearchResult, 0, 5)
	for _, item := range response.Results {
		link, err := url.Parse(item.URL)
		if err != nil || link.Hostname() == "" || link.User != nil || (link.Scheme != "https" && link.Scheme != "http") || len(item.URL) > 2048 {
			continue
		}
		results = append(results, websearch.SearchResult{
			URL:     item.URL,
			Title:   truncatePlaygroundSearchText(item.Title, 200),
			Snippet: truncatePlaygroundSearchText(item.Snippet, 900),
		})
		if len(results) == 5 {
			break
		}
	}
	return results, nil
}

func truncatePlaygroundSearchText(text string, maxRunes int) string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes])
}
