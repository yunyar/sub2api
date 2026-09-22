package routes

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type playgroundConversation struct {
	ID           string                     `json:"id"`
	Title        string                     `json:"title"`
	GroupID      int64                      `json:"groupId"`
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
var playgroundHistoryScientificNumber = regexp.MustCompile(`^(-?)([0-9]+)(?:\.([0-9]+))?[eE]([+-]?[0-9]+)$`)

var savePlaygroundHistory = redis.NewScript(`
local now = tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local previous = redis.call('GET', KEYS[2])
local incoming = cjson.decode(ARGV[3])
local expiry = now + 604800000
if previous then
  local stored = cjson.decode(previous)
  if stored.expiresAt <= now then return {410, ''} end
  if stored.revision ~= incoming.revision then return {409, ''} end
  expiry = stored.expiresAt
elseif incoming.revision ~= 0 then
  return {410, ''}
elseif redis.call('ZCARD', KEYS[1]) >= 30 then
  return {429, ''}
end
incoming.revision = incoming.revision + 1
incoming.expiresAt = expiry
local encoded, revisionUpdated = string.gsub(ARGV[3], '"revision"%s*:%s*%-?%d+', '"revision":' .. incoming.revision, 1)
local expiryUpdated
encoded, expiryUpdated = string.gsub(encoded, '"expiresAt"%s*:%s*%-?%d+', '"expiresAt":' .. expiry, 1)
if revisionUpdated ~= 1 or expiryUpdated ~= 1 then return {500, ''} end
redis.call('SET', KEYS[2], encoded, 'PX', math.max(1, expiry - now))
redis.call('ZADD', KEYS[1], expiry, ARGV[2])
redis.call('PEXPIRE', KEYS[1], 604800000)
return {200, encoded}
`)

func RegisterPlaygroundHistoryRoutes(v1 *gin.RouterGroup, jwtAuth middleware.JWTAuthMiddleware, client *redis.Client) {
	history := v1.Group("/playground/conversations", gin.HandlerFunc(jwtAuth), middleware.RequestBodyLimit(256*1024))
	history.Use(func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "Authentication required")
			c.Abort()
			return
		}
		c.Set("playgroundHistoryPrefix", fmt.Sprintf("playground:history:{%d}:", subject.UserID))
		c.Next()
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
			var item playgroundConversation
			if json.Unmarshal(normalizePlaygroundConversationJSON([]byte(raw)), &item) != nil {
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
		raw, err := json.Marshal(item)
		if err != nil {
			response.BadRequest(c, "Invalid conversation")
			return
		}
		prefix := c.GetString("playgroundHistoryPrefix")
		result, err := savePlaygroundHistory.Run(c.Request.Context(), client, []string{prefix + "index", prefix + item.ID}, time.Now().UnixMilli(), item.ID, string(raw)).Slice()
		if err != nil {
			response.InternalError(c, "Failed to save conversation")
			return
		}
		if len(result) != 2 {
			response.InternalError(c, "Invalid storage response")
			return
		}
		code, ok := result[0].(int64)
		if !ok {
			response.InternalError(c, "Invalid storage response")
			return
		}
		if code != http.StatusOK {
			response.Error(c, int(code), "Conversation expired, changed on another device, or history limit reached")
			return
		}
		var saved playgroundConversation
		encoded, valid := result[1].(string)
		if !valid || json.Unmarshal(normalizePlaygroundConversationJSON([]byte(encoded)), &saved) != nil {
			response.InternalError(c, "Invalid storage response")
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
	if len(item.Messages) > 200 || len(item.Title) > 240 || len(item.Model) > 256 || len(item.ImageModel) > 256 || item.Revision < 0 || item.Temperature < 0 || item.Temperature > 2 || (item.Kind != "" && item.Kind != "chat" && item.Kind != "workflow") {
		return false
	}
	for _, message := range item.Messages {
		if (message.Role != "user" && message.Role != "assistant") || len(message.Content) > 32768 || len(message.Model) > 256 || len(message.StepID) > 64 || (message.Kind != "" && message.Kind != "chat" && message.Kind != "image") || len(message.Content) >= 10 && message.Content[:10] == "data:image" {
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
		if !playgroundHistoryID.MatchString(step.ID) || len(step.Prompt) > 16000 {
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
