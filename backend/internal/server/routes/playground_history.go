package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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
}

var playgroundHistoryID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

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
local encoded = cjson.encode(incoming)
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
		ids, err := client.ZRangeByScore(c.Request.Context(), prefix+"index", &redis.ZRangeBy{Min: fmt.Sprintf("(%d", time.Now().UnixMilli()), Max: "+inf", Count: 30}).Result()
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
			if json.Unmarshal([]byte(raw), &item) != nil {
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
		if c.Param("id") == "index" || !playgroundHistoryID.MatchString(c.Param("id")) || c.ShouldBindJSON(&item) != nil || len(item.Messages) == 0 || len(item.Messages) > 200 || len(item.Title) > 240 || len(item.Model) > 256 || item.Revision < 0 || item.Temperature < 0 || item.Temperature > 2 {
			response.BadRequest(c, "Invalid conversation (maximum 200 messages)")
			return
		}
		for _, message := range item.Messages {
			if message.Role != "user" && message.Role != "assistant" {
				response.BadRequest(c, "Invalid message role")
				return
			}
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
		if !valid || json.Unmarshal([]byte(encoded), &saved) != nil {
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
