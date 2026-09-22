package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundHistoryIsolationExpiryAndConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	router := gin.New()
	auth := middleware.JWTAuthMiddleware(func(ctx *gin.Context) {
		userID, _ := strconv.ParseInt(ctx.GetHeader("X-Test-User"), 10, 64)
		if userID > 0 {
			ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		ctx.Next()
	})
	RegisterPlaygroundHistoryRoutes(router.Group("/api/v1"), auth, client)
	request := func(method, path, user string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, "/api/v1/playground/conversations"+path, bytes.NewReader(encoded))
		req.Header.Set("X-Test-User", user)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	conversation := playgroundConversation{Title: "Private", Temperature: 0.7, Messages: []playgroundHistoryMessage{{ID: 1, Role: "user", Content: "hello"}}}
	require.Equal(t, http.StatusUnauthorized, request("GET", "", "", nil).Code)
	require.Equal(t, http.StatusOK, request("PUT", "/example", "1", conversation).Code)
	var saved struct {
		Data playgroundConversation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(request("GET", "", "2", nil).Body.Bytes(), &struct {
		Data []playgroundConversation `json:"data"`
	}{}))
	require.NotContains(t, request("GET", "", "2", nil).Body.String(), "Private")
	require.Equal(t, http.StatusOK, request("DELETE", "/example", "2", nil).Code)
	require.Contains(t, request("GET", "", "1", nil).Body.String(), "Private")
	require.Equal(t, http.StatusConflict, request("PUT", "/example", "1", conversation).Code)
	conversation.Revision = 1
	response := request("PUT", "/example", "1", conversation)
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	expiry := saved.Data.ExpiresAt
	require.InDelta(t, time.Now().Add(7*24*time.Hour).UnixMilli(), expiry, 2000)
	conversation.Revision = 2
	require.NoError(t, json.Unmarshal(request("PUT", "/example", "1", conversation).Body.Bytes(), &saved))
	require.Equal(t, expiry, saved.Data.ExpiresAt)
	server.FastForward(7*24*time.Hour + time.Second)
	require.NotContains(t, request("GET", "", "1", nil).Body.String(), "Private")
	require.Equal(t, http.StatusGone, request("PUT", "/example", "1", saved.Data).Code)
	conversation.Revision = 0
	conversation.Messages[0].Content = strings.Repeat("x", 256*1024)
	require.NotEqual(t, http.StatusOK, request("PUT", "/oversized", "1", conversation).Code)
}

func TestPlaygroundWorkflowHistoryValidation(t *testing.T) {
	valid := playgroundConversation{Kind: "workflow", Model: "chat", ImageModel: "image", Temperature: 0.7, Messages: []playgroundHistoryMessage{{ID: 1, Role: "assistant", Content: "result", Kind: "chat", StepID: "step-1"}}, Workflow: &playgroundWorkflow{Steps: []playgroundWorkflowStep{{ID: "step-1", Prompt: "first"}}, CurrentStep: 0}}
	require.True(t, validPlaygroundConversation(&valid))
	valid.Messages = nil
	require.True(t, validPlaygroundConversation(&valid))
	valid.Workflow.Steps = append(valid.Workflow.Steps, playgroundWorkflowStep{ID: "step-2", Prompt: ""})
	require.True(t, validPlaygroundConversation(&valid))
	valid.Workflow.Steps[1].ID = "step-1"
	require.False(t, validPlaygroundConversation(&valid))
	valid.Workflow.Steps[1].ID = "step-2"
	valid.Messages = []playgroundHistoryMessage{{ID: 1, Role: "assistant", Content: "result", Kind: "chat", StepID: "step-1"}}
	valid.Messages[0].Content = "data:image/png;base64,abc"
	require.False(t, validPlaygroundConversation(&valid))
	valid.Messages[0].Content = "result"
	valid.Workflow.CurrentStep = 2
	require.False(t, validPlaygroundConversation(&valid))
	valid.Workflow.CurrentStep = 0
	valid.Messages[0].StepID = "missing"
	require.False(t, validPlaygroundConversation(&valid))
	valid.Messages[0].StepID = "step-1"
	valid.Workflow.Steps = make([]playgroundWorkflowStep, 51)
	require.False(t, validPlaygroundConversation(&valid))
}

func TestPlaygroundWorkflowHistoryPreservesEmptyMessagesAsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	router := gin.New()
	auth := middleware.JWTAuthMiddleware(func(ctx *gin.Context) {
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		ctx.Next()
	})
	RegisterPlaygroundHistoryRoutes(router.Group("/api/v1"), auth, client)
	conversation := playgroundConversation{
		Kind: "workflow", Model: "chat", ImageModel: "image", Temperature: 0.7,
		Messages: []playgroundHistoryMessage{},
		Workflow: &playgroundWorkflow{Steps: []playgroundWorkflowStep{{ID: "step-1", Prompt: "first"}}},
	}
	body, err := json.Marshal(conversation)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/playground/conversations/workflow-1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)

	raw, err := client.Get(t.Context(), "playground:history:{1}:workflow-1").Result()
	require.NoError(t, err)
	require.Contains(t, raw, `"messages":[]`)
	var stored playgroundConversation
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.NotNil(t, stored.Messages)
	require.Empty(t, stored.Messages)
}

func TestPlaygroundHistoryNormalizesLegacyEmptyMessageObject(t *testing.T) {
	legacy := []byte(`{"id":"workflow-1","messages":{},"workflow":{"steps":[{"id":"step-1","prompt":"first"}],"currentStep":0}}`)
	var conversation playgroundConversation
	require.NoError(t, json.Unmarshal(normalizePlaygroundConversationJSON(legacy), &conversation))
	require.NotNil(t, conversation.Messages)
	require.Empty(t, conversation.Messages)
}

func TestPlaygroundHistoryNormalizesLegacyScientificMessageID(t *testing.T) {
	legacy := []byte(`{"id":"workflow-1","messages":[{"id":1.790037855279e+14,"role":"assistant","content":"result","stepId":"step-1"}],"workflow":{"steps":[{"id":"step-1","prompt":"first"}],"currentStep":0}}`)
	var conversation playgroundConversation
	require.NoError(t, json.Unmarshal(normalizePlaygroundConversationJSON(legacy), &conversation))
	require.Equal(t, int64(179003785527900), conversation.Messages[0].ID)
}

func TestNormalizeLegacyIntegralJSONNumber(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		value string
		ok    bool
	}{
		{name: "preserves precision above float64", raw: `9.007199254740993e+15`, value: `9007199254740993`, ok: true},
		{name: "fractional mantissa within int64", raw: `0.9e+19`, value: `9000000000000000000`, ok: true},
		{name: "int64 maximum", raw: `9.223372036854775807e+18`, value: `9223372036854775807`, ok: true},
		{name: "int64 minimum", raw: `-9.223372036854775808e+18`, value: `-9223372036854775808`, ok: true},
		{name: "fractional", raw: `179003785527900.1`, ok: false},
		{name: "fractional scientific", raw: `1.5e+0`, ok: false},
		{name: "overflow", raw: `9.223372036854776e+18`, ok: false},
		{name: "minimum exponent", raw: `1e-9223372036854775808`, ok: false},
		{name: "maximum exponent", raw: `1e+9223372036854775807`, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, ok := normalizeLegacyIntegralJSONNumber(json.RawMessage(test.raw))
			require.Equal(t, test.ok, ok)
			if test.ok {
				require.Equal(t, json.RawMessage(test.value), id)
			}
		})
	}
}

func TestPlaygroundHistoryLoadsLegacyScientificMessageID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	router := gin.New()
	auth := middleware.JWTAuthMiddleware(func(ctx *gin.Context) {
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		ctx.Next()
	})
	RegisterPlaygroundHistoryRoutes(router.Group("/api/v1"), auth, client)

	prefix := "playground:history:{1}:"
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	legacy := `{"id":"workflow-1","revision":1,"expiresAt":` + strconv.FormatInt(expiresAt, 10) + `,"messages":[{"id":1.790037855279e+14,"role":"assistant","content":"result","stepId":"step-1"}],"kind":"workflow","workflow":{"steps":[{"id":"step-1","prompt":"first"}],"currentStep":0}}`
	require.NoError(t, client.Set(t.Context(), prefix+"workflow-1", legacy, time.Hour).Err())
	require.NoError(t, client.ZAdd(t.Context(), prefix+"index", redis.Z{Score: float64(expiresAt), Member: "workflow-1"}).Err())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/playground/conversations", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Data []playgroundConversation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, int64(179003785527900), body.Data[0].Messages[0].ID)
}

func TestPlaygroundWorkflowHistoryWithRealRedis(t *testing.T) {
	address := os.Getenv("PLAYGROUND_HISTORY_REDIS_ADDR")
	if address == "" {
		t.Skip("set PLAYGROUND_HISTORY_REDIS_ADDR to run against Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(t.Context()).Err())

	keyPrefix := "playground:history:{real-redis-test}:"
	require.NoError(t, client.Del(t.Context(), keyPrefix+"index", keyPrefix+"workflow-1").Err())
	t.Cleanup(func() { _ = client.Del(t.Context(), keyPrefix+"index", keyPrefix+"workflow-1").Err() })
	now := time.Now().UnixMilli()
	conversation := playgroundConversation{
		ID: "workflow-1", Title: `Prompt says {"revision":999,"expiresAt":1}`, Kind: "workflow", Model: "chat", ImageModel: "image", Temperature: 0.7,
		Messages: []playgroundHistoryMessage{},
		Workflow: &playgroundWorkflow{Steps: []playgroundWorkflowStep{{ID: "step-1", Prompt: "first"}}},
	}
	raw, err := json.Marshal(conversation)
	require.NoError(t, err)
	result, err := savePlaygroundHistory.Run(t.Context(), client, []string{keyPrefix + "index", keyPrefix + conversation.ID}, now, conversation.ID, string(raw)).Slice()
	require.NoError(t, err)
	require.Equal(t, int64(http.StatusOK), result[0])

	storedRaw, err := client.Get(t.Context(), keyPrefix+conversation.ID).Result()
	require.NoError(t, err)
	var stored playgroundConversation
	require.NoError(t, json.Unmarshal([]byte(storedRaw), &stored))
	require.Equal(t, conversation.Title, stored.Title)
	require.NotNil(t, stored.Messages)
	require.Empty(t, stored.Messages)
	require.Equal(t, int64(1), stored.Revision)
	require.Greater(t, stored.ExpiresAt, now)
}

func TestPlaygroundHistoryLegacyCJSONWithRealRedis(t *testing.T) {
	address := os.Getenv("PLAYGROUND_HISTORY_REDIS_ADDR")
	if address == "" {
		t.Skip("set PLAYGROUND_HISTORY_REDIS_ADDR to run against Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(t.Context()).Err())

	keyPrefix := "playground:history:{real-redis-legacy-test}:"
	require.NoError(t, client.Del(t.Context(), keyPrefix+"index", keyPrefix+"workflow-1").Err())
	t.Cleanup(func() { _ = client.Del(t.Context(), keyPrefix+"index", keyPrefix+"workflow-1").Err() })
	now := time.Now().UnixMilli()
	expiresAt := now + 604800000
	conversation := playgroundConversation{
		ID: "workflow-1", Revision: 1, ExpiresAt: expiresAt, Kind: "workflow", Temperature: 0.7,
		Messages: []playgroundHistoryMessage{{ID: 179003785527900, Role: "assistant", Content: "result", StepID: "step-1"}},
		Workflow: &playgroundWorkflow{Steps: []playgroundWorkflowStep{{ID: "step-1", Prompt: "first"}}},
	}
	raw, err := json.Marshal(conversation)
	require.NoError(t, err)
	legacy, err := redis.NewScript(`return cjson.encode(cjson.decode(ARGV[1]))`).Run(t.Context(), client, nil, string(raw)).Text()
	require.NoError(t, err)
	require.Contains(t, legacy, `"id":1.790037855279e+14`)
	require.NoError(t, client.Set(t.Context(), keyPrefix+conversation.ID, legacy, time.Until(time.UnixMilli(expiresAt))).Err())
	require.NoError(t, client.ZAdd(t.Context(), keyPrefix+"index", redis.Z{Score: float64(expiresAt), Member: conversation.ID}).Err())

	var loaded playgroundConversation
	require.NoError(t, json.Unmarshal(normalizePlaygroundConversationJSON([]byte(legacy)), &loaded))
	require.Equal(t, conversation.Messages[0].ID, loaded.Messages[0].ID)
	updated, err := json.Marshal(loaded)
	require.NoError(t, err)
	result, err := savePlaygroundHistory.Run(t.Context(), client, []string{keyPrefix + "index", keyPrefix + conversation.ID}, now, conversation.ID, string(updated)).Slice()
	require.NoError(t, err)
	require.Equal(t, int64(http.StatusOK), result[0])
	storedRaw, err := client.Get(t.Context(), keyPrefix+conversation.ID).Result()
	require.NoError(t, err)
	var stored playgroundConversation
	require.NoError(t, json.Unmarshal([]byte(storedRaw), &stored))
	require.Equal(t, conversation.Messages[0].ID, stored.Messages[0].ID)
	require.Equal(t, int64(2), stored.Revision)
	require.Equal(t, expiresAt, stored.ExpiresAt)
}
