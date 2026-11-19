package routes

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type playgroundConversation struct {
	ID           string                     `json:"id"`
	Title        string                     `json:"title"`
	GroupID      int64                      `json:"groupId"`
	Routing      *playgroundHistoryRouting  `json:"routing,omitempty"`
	Model        string                     `json:"model"`
	ImageModel   string                     `json:"imageModel,omitempty"`
	Kind         string                     `json:"kind,omitempty"`
	Workflow     *playgroundWorkflow        `json:"workflow,omitempty"`
	SystemPrompt string                     `json:"systemPrompt"`
	Temperature  float64                    `json:"temperature"`
	Messages     []playgroundHistoryMessage `json:"messages"`
	Revision     int64                      `json:"revision"`
	ExpiresAt    int64                      `json:"expiresAt"`
}

type playgroundHistoryRouting struct {
	Mode          string `json:"mode,omitempty"`
	AutoProfileID *int64 `json:"autoProfileId,omitempty"`
}

type playgroundHistoryMessage struct {
	ID      int64  `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Model   string `json:"model,omitempty"`
	Kind    string `json:"kind,omitempty"`
	StepID  string `json:"stepId,omitempty"`
}

type playgroundWorkflow struct {
	Steps       []playgroundWorkflowStep `json:"steps"`
	CurrentStep int                      `json:"currentStep"`
}

type playgroundWorkflowStep struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

var playgroundHistoryID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
var playgroundHistoryImageData = regexp.MustCompile(`(?i)data:image/[a-z0-9.+-]+;base64,`)
var playgroundHistoryScientificNumber = regexp.MustCompile(`^(-?)([0-9]+)(?:\.([0-9]+))?[eE]([+-]?[0-9]+)$`)

const (
	playgroundHistoryRetention = 30 * 24 * time.Hour
	playgroundHistoryMaxBody   = 4 * 1024 * 1024
	playgroundHistoryMaxText   = 1024 * 1024
	playgroundDefaultQuotaMB   = 50
	playgroundMaxQuotaMB       = 120
)

var savePlaygroundHistory = redis.NewScript(`
local now = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local previous = redis.call('GET', KEYS[2])
local revision = tonumber(ARGV[3])
local expiry = now + tonumber(ARGV[6])
if previous then
  local currentRevision, currentExpiry = string.match(previous, '^PG1 (%d+) (%d+) ')
  if not currentRevision then
    local stored = cjson.decode(previous)
    currentRevision, currentExpiry = stored.revision, stored.expiresAt
  end
  expiry = tonumber(currentExpiry)
  if expiry <= now then return {410, ''} end
  if tonumber(currentRevision) ~= revision then return {409, ''} end
elseif revision ~= 0 then
  return {410, ''}
elseif redis.call('ZCARD', KEYS[1]) >= 30 then
  return {429, ''}
end
local quotaMB = tonumber(redis.call('GET', KEYS[3])) or tonumber(ARGV[7])
local size = tonumber(ARGV[5])
local prefix = string.sub(KEYS[1], 1, -6)
for _, id in ipairs(redis.call('ZRANGE', KEYS[1], 0, -1)) do
  if id ~= ARGV[2] then
    local key = prefix .. id
    local header = redis.call('GETRANGE', key, 0, 63)
    local rawSize = string.match(header, '^PG1 %d+ %d+ (%d+)\n')
    size = size + (tonumber(rawSize) or redis.call('STRLEN', key))
  end
end
if size > quotaMB * 1048576 then return {413, ''} end
local encoded = 'PG1 ' .. (revision + 1) .. ' ' .. expiry .. ' ' .. ARGV[5] .. '\n' .. ARGV[4]
redis.call('SET', KEYS[2], encoded, 'PX', math.max(1, expiry - now))
redis.call('ZADD', KEYS[1], expiry, ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[6])
return {200, encoded}
`)

func playgroundHistoryPrefix(userID int64) string {
	return fmt.Sprintf("playground:history:{%d}:", userID)
}

func playgroundHistoryQuotaKey(userID int64) string {
	return playgroundHistoryPrefix(userID) + ":quota"
}

func readPlaygroundHistoryQuota(c *gin.Context, client *redis.Client, userID int64) {
	quota, err := client.Get(c.Request.Context(), playgroundHistoryQuotaKey(userID)).Int()
	if err == redis.Nil {
		quota = playgroundDefaultQuotaMB
	} else if err != nil {
		response.InternalError(c, "Failed to load playground storage quota")
		return
	}
	response.Success(c, gin.H{"limitMB": quota, "defaultMB": playgroundDefaultQuotaMB})
}

func registerPlaygroundHistoryAdminRoutes(admin *gin.RouterGroup, client *redis.Client) {
	quota := admin.Group("/users/:id/playground-storage")
	quota.Use(func(c *gin.Context) {
		if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
			response.Error(c, http.StatusForbidden, "Full administrator access required")
			c.Abort()
			return
		}
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID < 1 {
			response.BadRequest(c, "Invalid user ID")
			c.Abort()
			return
		}
		c.Set("playgroundQuotaUserID", userID)
	})
	quota.GET("", func(c *gin.Context) {
		readPlaygroundHistoryQuota(c, client, c.GetInt64("playgroundQuotaUserID"))
	})
	quota.PUT("", middleware.RequestBodyLimit(1024), func(c *gin.Context) {
		var payload struct {
			LimitMB *int `json:"limitMB"`
		}
		if c.ShouldBindJSON(&payload) != nil || payload.LimitMB == nil || *payload.LimitMB < 0 || *payload.LimitMB > playgroundMaxQuotaMB {
			response.BadRequest(c, fmt.Sprintf("Storage quota must be between 0 and %d MiB", playgroundMaxQuotaMB))
			return
		}
		userID := c.GetInt64("playgroundQuotaUserID")
		key := playgroundHistoryQuotaKey(userID)
		ctx := c.Request.Context()
		var err error
		if *payload.LimitMB == 0 {
			err = client.Del(ctx, key).Err()
		} else {
			err = client.Set(ctx, key, *payload.LimitMB, 0).Err()
		}
		if err != nil {
			response.InternalError(c, "Failed to update playground storage quota")
			return
		}
		readPlaygroundHistoryQuota(c, client, userID)
	})
}

func storePlaygroundConversation(ctx context.Context, client *redis.Client, prefix string, item playgroundConversation) (int, playgroundConversation, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return 0, item, err
	}
	if len(raw) > playgroundHistoryMaxBody {
		return http.StatusRequestEntityTooLarge, item, nil
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return 0, item, err
	}
	if err := writer.Close(); err != nil {
		return 0, item, err
	}
	result, err := savePlaygroundHistory.Run(ctx, client,
		[]string{prefix + "index", prefix + item.ID, prefix + ":quota"},
		time.Now().UnixMilli(), item.ID, item.Revision, compressed.Bytes(), len(raw),
		playgroundHistoryRetention.Milliseconds(), playgroundDefaultQuotaMB,
	).Slice()
	if err != nil {
		return 0, item, err
	}
	if len(result) != 2 {
		return 0, item, fmt.Errorf("invalid playground storage result")
	}
	code, ok := result[0].(int64)
	if !ok {
		return 0, item, fmt.Errorf("invalid playground storage status")
	}
	if code == http.StatusOK {
		stored, ok := result[1].(string)
		if !ok {
			return 0, item, fmt.Errorf("invalid stored playground conversation")
		}
		item, err = decodePlaygroundConversation(stored)
	}
	return int(code), item, err
}

func decodePlaygroundConversation(stored string) (playgroundConversation, error) {
	var item playgroundConversation
	if !strings.HasPrefix(stored, "PG1 ") {
		err := json.Unmarshal(normalizePlaygroundConversationJSON([]byte(stored)), &item)
		return item, err
	}
	header, compressed, found := strings.Cut(stored, "\n")
	if !found {
		return item, fmt.Errorf("missing playground storage header")
	}
	var revision, expiry int64
	var size int
	if _, err := fmt.Sscanf(header, "PG1 %d %d %d", &revision, &expiry, &size); err != nil || size < 1 || size > playgroundHistoryMaxBody {
		return item, fmt.Errorf("invalid playground storage header")
	}
	reader, err := gzip.NewReader(strings.NewReader(compressed))
	if err != nil {
		return item, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, int64(size)+1))
	if err != nil || len(raw) != size {
		return item, fmt.Errorf("invalid compressed playground conversation")
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return item, err
	}
	item.Revision, item.ExpiresAt = revision, expiry
	return item, nil
}

func RegisterPlaygroundHistoryRoutes(v1 *gin.RouterGroup, jwtAuth middleware.JWTAuthMiddleware, client *redis.Client) {
	history := v1.Group("/playground/conversations", gin.HandlerFunc(jwtAuth), middleware.RequestBodyLimit(playgroundHistoryMaxBody))
	history.Use(func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "Authentication required")
			c.Abort()
			return
		}
		c.Set("playgroundHistoryPrefix", playgroundHistoryPrefix(subject.UserID))
		c.Next()
	})
	history.GET("/quota", func(c *gin.Context) {
		subject, _ := middleware.GetAuthSubjectFromContext(c)
		readPlaygroundHistoryQuota(c, client, subject.UserID)
	})
	history.GET("", func(c *gin.Context) {
		prefix := c.GetString("playgroundHistoryPrefix")
		ids, err := client.ZRangeArgs(c.Request.Context(), redis.ZRangeArgs{Key: prefix + "index", Start: fmt.Sprintf("(%d", time.Now().UnixMilli()), Stop: "+inf", ByScore: true, Count: 30}).Result()
		if err != nil {
			response.InternalError(c, "Failed to load conversations")
			return
		}
		items := make([]playgroundConversation, 0, len(ids))
		for _, id := range ids {
			raw, err := client.Get(c.Request.Context(), prefix+id).Result()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				response.InternalError(c, "Failed to load conversations")
				return
			}
			item, err := decodePlaygroundConversation(raw)
			if err != nil {
				response.InternalError(c, "Invalid conversation data")
				return
			}
			if item.ExpiresAt > time.Now().UnixMilli() {
				items = append(items, item)
			}
		}
		response.Success(c, items)
	})
	history.PUT("/:id", func(c *gin.Context) {
		var item playgroundConversation
		if c.Param("id") == "index" || !playgroundHistoryID.MatchString(c.Param("id")) || c.ShouldBindJSON(&item) != nil || !validPlaygroundConversation(&item) {
			response.BadRequest(c, "Invalid conversation (maximum 200 messages)")
			return
		}
		item.ID = c.Param("id")
		code, saved, err := storePlaygroundConversation(c.Request.Context(), client, c.GetString("playgroundHistoryPrefix"), item)
		if err != nil {
			response.InternalError(c, "Failed to save conversation")
			return
		}
		if code != http.StatusOK {
			if code == http.StatusRequestEntityTooLarge {
				response.Error(c, code, "Playground history storage quota exceeded")
			} else {
				response.Error(c, code, "Conversation expired, changed on another device, or history limit reached")
			}
			return
		}
		response.Success(c, saved)
	})
	history.DELETE("/:id", func(c *gin.Context) {
		if c.Param("id") == "index" || !playgroundHistoryID.MatchString(c.Param("id")) {
			response.BadRequest(c, "Invalid conversation ID")
			return
		}
		prefix := c.GetString("playgroundHistoryPrefix")
		pipe := client.TxPipeline()
		pipe.Del(c.Request.Context(), prefix+c.Param("id"))
		pipe.ZRem(c.Request.Context(), prefix+"index", c.Param("id"))
		if _, err := pipe.Exec(c.Request.Context()); err != nil {
			response.InternalError(c, "Failed to delete conversation")
			return
		}
		response.Success(c, gin.H{})
	})
}

func normalizePlaygroundConversationJSON(raw []byte) []byte {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	messages, exists := value["messages"]
	if !exists {
		return raw
	}
	if isEmptyJSONObject(messages) {
		value["messages"] = json.RawMessage("[]")
	} else {
		normalizedMessages, valid := normalizeLegacyPlaygroundMessageIDs(messages)
		if !valid {
			return raw
		}
		value["messages"] = normalizedMessages
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return normalized
}

func isEmptyJSONObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && len(value) == 0
}

func normalizeLegacyPlaygroundMessageIDs(raw json.RawMessage) (json.RawMessage, bool) {
	var messages []map[string]json.RawMessage
	if json.Unmarshal(raw, &messages) != nil {
		return nil, false
	}
	changed := false
	for _, message := range messages {
		id, ok := normalizeLegacyIntegralJSONNumber(message["id"])
		if !ok {
			continue
		}
		message["id"] = id
		changed = true
	}
	if !changed {
		return raw, true
	}
	normalized, err := json.Marshal(messages)
	if err != nil {
		return nil, false
	}
	return normalized, true
}

func normalizeLegacyIntegralJSONNumber(raw json.RawMessage) (json.RawMessage, bool) {
	value := string(raw)
	matches := playgroundHistoryScientificNumber.FindStringSubmatch(value)
	if matches == nil {
		return nil, false
	}
	exponent, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return nil, false
	}
	coefficient := new(big.Int)
	if _, ok := coefficient.SetString(matches[2]+matches[3], 10); !ok {
		return nil, false
	}
	if matches[1] == "-" {
		coefficient.Neg(coefficient)
	}
	if coefficient.Sign() == 0 {
		return json.RawMessage("0"), true
	}
	if exponent > 18+int64(len(matches[3])) || exponent < -int64(len(matches[2])+len(matches[3])) {
		return nil, false
	}
	scale := exponent - int64(len(matches[3]))
	if scale >= 0 {
		coefficient.Mul(coefficient, new(big.Int).Exp(big.NewInt(10), big.NewInt(scale), nil))
	} else {
		divisorScale := -scale
		if divisorScale > int64(len(matches[2])+len(matches[3])) {
			return nil, false
		}
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(divisorScale), nil)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(coefficient, divisor, remainder)
		if remainder.Sign() != 0 {
			return nil, false
		}
		coefficient = quotient
	}
	if !coefficient.IsInt64() {
		return nil, false
	}
	return json.RawMessage(coefficient.String()), true
}

func validPlaygroundConversation(item *playgroundConversation) bool {
	if len(item.Messages) > 200 || len(item.Title) > 240 || len(item.Model) > 256 || len(item.ImageModel) > 256 || len(item.SystemPrompt) > playgroundHistoryMaxText || item.Revision < 0 || item.Revision > 1<<31 || item.Temperature < 0 || item.Temperature > 2 || (item.Kind != "" && item.Kind != "chat" && item.Kind != "workflow") {
		return false
	}
	for _, message := range item.Messages {
		if (message.Role != "user" && message.Role != "assistant") || len(message.Content) > playgroundHistoryMaxText || len(message.Model) > 256 || len(message.StepID) > 64 || (message.Kind != "" && message.Kind != "chat" && message.Kind != "image") || playgroundHistoryImageData.MatchString(message.Content) {
			return false
		}
	}
	if item.Kind != "workflow" {
		return item.Workflow == nil && len(item.Messages) > 0
	}
	if item.Workflow == nil || len(item.Workflow.Steps) == 0 || len(item.Workflow.Steps) > 50 || item.Workflow.CurrentStep < 0 || item.Workflow.CurrentStep >= len(item.Workflow.Steps) {
		return false
	}
	stepIDs := make(map[string]struct{}, len(item.Workflow.Steps))
	for _, step := range item.Workflow.Steps {
		if !playgroundHistoryID.MatchString(step.ID) || len(step.Prompt) > playgroundHistoryMaxText {
			return false
		}
		if _, exists := stepIDs[step.ID]; exists {
			return false
		}
		stepIDs[step.ID] = struct{}{}
	}
	for _, message := range item.Messages {
		if message.StepID == "" {
			return false
		}
		if _, exists := stepIDs[message.StepID]; !exists {
			return false
		}
	}
	return true
}
