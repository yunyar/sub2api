package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const bloomSecurityProfiles = `{"cold_start":{"easy":2,"medium":3,"hard":5},"text":{"easy":1,"medium":4,"hard":5},"image":{"easy":1,"medium":2,"hard":7},"mixed":{"easy":2,"medium":3,"hard":5}}`

type bloomSecuritySettingsFixture struct {
	service.SettingRepository
	value  string
	err    error
	reads  atomic.Int64
	writes int
}

func (store *bloomSecuritySettingsFixture) GetValue(_ context.Context, key string) (string, error) {
	store.reads.Add(1)
	if key != bloomPlannerKey {
		return "", service.ErrSettingNotFound
	}
	return store.value, store.err
}

func (store *bloomSecuritySettingsFixture) Set(_ context.Context, key, value string) error {
	if key != bloomPlannerKey {
		return errors.New("unexpected setting")
	}
	if store.err != nil {
		return store.err
	}
	store.value = value
	store.writes++
	return nil
}

type bloomSecurityKeysFixture struct {
	key         *service.APIKey
	err         error
	calls       atomic.Int64
	requestedID atomic.Int64
}

func (keys *bloomSecurityKeysFixture) GetByID(_ context.Context, keyID int64) (*service.APIKey, error) {
	keys.calls.Add(1)
	keys.requestedID.Store(keyID)
	return keys.key, keys.err
}

type bloomSecurityModelFixture struct {
	calls   atomic.Int64
	status  int
	body    string
	err     error
	request *http.Request
	payload string
}

func (model *bloomSecurityModelFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	model.calls.Add(1)
	model.request = req
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	model.payload = string(payload)
	if model.err != nil {
		return nil, model.err
	}
	return &http.Response{StatusCode: model.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(model.body)), Request: req}, nil
}

func bloomSecurityModelResponse(t *testing.T, content string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"content": content}}},
	})
	require.NoError(t, err)
	return string(payload)
}

func bloomSecuritySaveSettings(t *testing.T, store *bloomSecuritySettingsFixture, settings bloomPlannerSettings) {
	t.Helper()
	payload, err := json.Marshal(settings)
	require.NoError(t, err)
	store.value = string(payload)
}

func newBloomSecurityPlanner(t *testing.T) (*bloomPlanner, *bloomSecuritySettingsFixture, *bloomSecurityKeysFixture, *bloomSecurityModelFixture) {
	t.Helper()
	var profiles map[bloomrules.Modality]bloomSplit
	require.NoError(t, json.Unmarshal([]byte(bloomSecurityProfiles), &profiles))
	store := &bloomSecuritySettingsFixture{}
	bloomSecuritySaveSettings(t, store, bloomPlannerSettings{
		Enabled: true, KeyID: 8, AdminID: 9, Model: "private-planner-model", Profiles: profiles,
	})
	keys := &bloomSecurityKeysFixture{key: &service.APIKey{
		ID: 8, UserID: 9, Key: "sk-private-planner-credential", Status: service.StatusActive,
		User: &service.User{ID: 9, Role: service.RoleAdmin, Status: service.StatusActive, Email: "private-admin@example.test"},
	}}
	model := &bloomSecurityModelFixture{status: http.StatusOK, body: bloomSecurityModelResponse(t, bloomSecurityProfiles)}
	planner := newBloomPlanner(store, keys, 12345)
	planner.client.Transport = model
	return planner, store, keys, model
}

func TestBloomPlannerSecurityUsesSavedProfilesWithoutModelCalls(t *testing.T) {
	t.Parallel()
	planner, store, keys, model := newBloomSecurityPlanner(t)
	planner.client = nil
	saved := store.value
	expected := map[bloomrules.Modality]bloomSplit{
		bloomrules.ColdStart: {Easy: 2, Medium: 3, Hard: 5},
		bloomrules.Text:      {Easy: 1, Medium: 4, Hard: 5},
		bloomrules.Image:     {Easy: 1, Medium: 2, Hard: 7},
		bloomrules.Mixed:     {Easy: 2, Medium: 3, Hard: 5},
	}
	for modality, split := range expected {
		require.Equal(t, split, planner.split(context.Background(), modality))
		for _, budget := range []int64{0, 1, 999_999, 10_000_000} {
			total := split.reward(budget, bloomrules.Weekly)
			for _, tier := range []bloomrules.Tier{bloomrules.Easy, bloomrules.Medium, bloomrules.Hard} {
				total += 7 * split.reward(budget, tier)
			}
			require.LessOrEqual(t, total, budget)
		}
	}
	require.Zero(t, model.calls.Load())
	require.EqualValues(t, 4, keys.calls.Load())
	require.EqualValues(t, 8, keys.requestedID.Load())
	require.Equal(t, saved, store.value)
	require.Zero(t, store.writes)
	keys.key.User.Role = service.RoleUser
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
	require.EqualValues(t, 5, keys.calls.Load())
	require.Zero(t, model.calls.Load())
}

func TestBloomPlannerSecurityRejectsUnavailableKeysAndProfiles(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expired := now.Add(-time.Minute)
	cases := []struct {
		name   string
		mutate func(*bloomPlannerSettings, *bloomSecurityKeysFixture)
	}{
		{"disabled planner", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) { settings.Enabled = false }},
		{"legacy unverified owner", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) { settings.AdminID = 0 }},
		{"invalid key id", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) { settings.KeyID = 0 }},
		{"key lookup failure", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.err = errors.New("private lookup failure")
		}},
		{"deleted key", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key = nil }},
		{"wrong key id", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.ID = 80 }},
		{"different owner", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.UserID = 10 }},
		{"different loaded owner", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.User.ID = 10 }},
		{"missing administrator", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.User = nil }},
		{"demoted administrator", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.User.Role = service.RoleUser }},
		{"disabled administrator", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.User.Status = service.StatusDisabled
		}},
		{"deleted administrator", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.User.DeletedAt = &expired }},
		{"disabled key", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.Status = service.StatusAPIKeyDisabled
		}},
		{"expired key status", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.Status = service.StatusAPIKeyExpired
		}},
		{"exhausted key status", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.Status = service.StatusAPIKeyQuotaExhausted
		}},
		{"unknown key status", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.Status = "unknown" }},
		{"runtime expiration", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.ExpiresAt = &expired }},
		{"runtime quota exhaustion", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.Quota, keys.key.QuotaUsed = 5, 5
		}},
		{"empty credential", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) { keys.key.Key = " " }},
		{"five hour limit", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.RateLimit5h, keys.key.Usage5h, keys.key.Window5hStart = 5, 5, &now
		}},
		{"daily limit", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.RateLimit1d, keys.key.Usage1d, keys.key.Window1dStart = 5, 5, &now
		}},
		{"weekly limit", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			keys.key.RateLimit7d, keys.key.Usage7d, keys.key.Window7dStart = 5, 5, &now
		}},
		{"deleted group", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			groupID := int64(3)
			keys.key.GroupID = &groupID
		}},
		{"disabled group", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			groupID := int64(3)
			keys.key.GroupID, keys.key.Group = &groupID, &service.Group{ID: groupID, Status: service.StatusDisabled}
		}},
		{"mismatched group", func(_ *bloomPlannerSettings, keys *bloomSecurityKeysFixture) {
			groupID := int64(3)
			keys.key.GroupID, keys.key.Group = &groupID, &service.Group{ID: 4, Status: service.StatusActive}
		}},
		{"missing profiles", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) { settings.Profiles = nil }},
		{"missing profile", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) {
			delete(settings.Profiles, bloomrules.Image)
		}},
		{"invalid profile", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) {
			settings.Profiles[bloomrules.Text] = bloomSplit{Easy: 2, Medium: 3, Hard: 99}
		}},
		{"unknown profile", func(settings *bloomPlannerSettings, _ *bloomSecurityKeysFixture) {
			delete(settings.Profiles, bloomrules.Image)
			settings.Profiles[bloomrules.Modality("unknown")] = defaultBloomSplit
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			planner, store, keys, model := newBloomSecurityPlanner(t)
			settings, err := planner.current(context.Background())
			require.NoError(t, err)
			test.mutate(&settings, keys)
			bloomSecuritySaveSettings(t, store, settings)
			require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
			require.Zero(t, model.calls.Load())
			require.Zero(t, store.writes)
		})
	}
}

func TestBloomPlannerSecurityAllowsExpiredRateWindows(t *testing.T) {
	t.Parallel()
	past := time.Now().Add(-8 * 24 * time.Hour)
	for _, windowStart := range []*time.Time{nil, &past} {
		planner, _, keys, model := newBloomSecurityPlanner(t)
		keys.key.RateLimit5h, keys.key.Usage5h, keys.key.Window5hStart = 5, 5, windowStart
		keys.key.RateLimit1d, keys.key.Usage1d, keys.key.Window1dStart = 5, 5, windowStart
		keys.key.RateLimit7d, keys.key.Usage7d, keys.key.Window7dStart = 5, 5, windowStart
		groupID := int64(3)
		keys.key.GroupID, keys.key.Group = &groupID, &service.Group{ID: groupID, Status: service.StatusActive}
		require.Equal(t, bloomSplit{Easy: 1, Medium: 4, Hard: 5}, planner.split(context.Background(), bloomrules.Text))
		require.Zero(t, model.calls.Load())
	}
}

func TestBloomPlannerSecuritySettingsFailuresUseDefaults(t *testing.T) {
	t.Parallel()
	for _, settingError := range []error{service.ErrSettingNotFound, errors.New("private settings failure")} {
		planner, store, _, model := newBloomSecurityPlanner(t)
		store.err = settingError
		require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
		require.Zero(t, model.calls.Load())
	}
	planner, store, _, model := newBloomSecurityPlanner(t)
	store.value = "invalid json"
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
	planner.settings = nil
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
	planner.settings = store
	planner.keys = nil
	store.value = `{"enabled":true,"key_id":8,"admin_id":9,"profiles":` + bloomSecurityProfiles + `}`
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Modality("unknown")))
	require.Zero(t, model.calls.Load())
	var absent *bloomPlanner
	require.Equal(t, defaultBloomSplit, absent.split(context.Background(), bloomrules.Text))
}

func bloomSecurityContext(method, path, body string, userID int64, role string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	if userID > 0 {
		ginContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		ginContext.Set(string(middleware.ContextKeyUserRole), role)
	}
	return ginContext, recorder
}

func TestBloomPlannerSecurityAdminConfiguration(t *testing.T) {
	t.Parallel()
	planner, store, keys, model := newBloomSecurityPlanner(t)
	handler := &BloomHandler{planner: planner}
	body := `{"enabled":true,"key_id":8,"model":"private-planner-model","admin_id":10,"key":"injected-secret","profiles":{"text":{"easy":9}}}`
	ginContext, recorder := bloomSecurityContext(http.MethodPut, "/api/v1/admin/settings/bloom-planner", body, 9, service.RoleAdmin)
	handler.UpdatePlanner(ginContext)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.EqualValues(t, 1, model.calls.Load())
	require.Equal(t, 1, store.writes)
	require.Equal(t, "Bearer sk-private-planner-credential", model.request.Header.Get("Authorization"))
	require.Equal(t, "127.0.0.1:12345", model.request.URL.Host)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(model.payload), &payload))
	require.Len(t, payload, 4)
	require.Equal(t, "private-planner-model", payload["model"])
	for _, secret := range []string{keys.key.Key, keys.key.User.Email, "user_id", "admin_id"} {
		require.NotContains(t, model.payload, secret)
	}
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(store.value), &stored))
	require.Len(t, stored, 5)
	require.JSONEq(t, "8", string(stored["key_id"]))
	require.JSONEq(t, "9", string(stored["admin_id"]))
	require.JSONEq(t, bloomSecurityProfiles, string(stored["profiles"]))
	for _, secret := range []string{keys.key.Key, keys.key.User.Email, "injected-secret", `"key":`} {
		require.NotContains(t, store.value, secret)
		require.NotContains(t, recorder.Body.String(), secret)
	}
	ginContext, recorder = bloomSecurityContext(http.MethodGet, "/api/v1/admin/settings/bloom-planner", "", 9, service.RoleAdmin)
	handler.Planner(ginContext)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), keys.key.Key)
	require.Equal(t, bloomSplit{Easy: 1, Medium: 4, Hard: 5}, planner.split(context.Background(), bloomrules.Text))
	require.EqualValues(t, 1, model.calls.Load())

	keys.key.User.Role = service.RoleUser
	saved := store.value
	ginContext, recorder = bloomSecurityContext(http.MethodPut, "/api/v1/admin/settings/bloom-planner", body, 9, service.RoleAdmin)
	handler.UpdatePlanner(ginContext)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, saved, store.value)
	require.Equal(t, 1, store.writes)
	require.EqualValues(t, 1, model.calls.Load())
	ginContext, recorder = bloomSecurityContext(http.MethodPut, "/api/v1/admin/settings/bloom-planner", `{"enabled":false}`, 9, service.RoleAdmin)
	handler.UpdatePlanner(ginContext)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, store.value, "admin_id")
	require.Equal(t, defaultBloomSplit, planner.split(context.Background(), bloomrules.Text))
	require.EqualValues(t, 1, model.calls.Load())
}

func TestBloomPlannerSecurityGenerationFailuresPreserveConfiguration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  int
		body    string
		content string
		err     error
	}{
		{name: "key rejected", status: http.StatusUnauthorized},
		{name: "billing rejected", status: http.StatusForbidden},
		{name: "rate limit", status: http.StatusTooManyRequests},
		{name: "model unavailable", status: http.StatusServiceUnavailable},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "invalid envelope", body: "private upstream failure"},
		{name: "empty choices", body: `{"choices":[]}`},
		{name: "incomplete profiles", content: `{"text":{"easy":2,"medium":3,"hard":5}}`},
		{name: "invalid profile json", content: "not json"},
		{name: "invalid rewards", content: strings.ReplaceAll(bloomSecurityProfiles, `"hard":5`, `"hard":99`)},
		{name: "oversized response", body: strings.Repeat(" ", 64<<10) + "{}"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			planner, store, keys, model := newBloomSecurityPlanner(t)
			if test.status != 0 {
				model.status = test.status
			}
			if test.body != "" {
				model.body = test.body
			}
			if test.content != "" {
				model.body = bloomSecurityModelResponse(t, test.content)
			}
			model.err = test.err
			saved := store.value
			ginContext, recorder := bloomSecurityContext(http.MethodPut, "/api/v1/admin/settings/bloom-planner",
				`{"enabled":true,"key_id":8,"model":"private-planner-model"}`, 9, service.RoleAdmin)
			(&BloomHandler{planner: planner}).UpdatePlanner(ginContext)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.Equal(t, saved, store.value)
			require.Zero(t, store.writes)
			require.EqualValues(t, 1, model.calls.Load())
			require.NotContains(t, recorder.Body.String(), keys.key.Key)
			require.NotContains(t, recorder.Body.String(), "private upstream")
		})
	}
	planner, _, _, model := newBloomSecurityPlanner(t)
	model.body = bloomSecurityModelResponse(t, `{"text":{"easy":2,"medium":3,"hard":5}}`)
	settings, err := planner.current(context.Background())
	require.NoError(t, err)
	require.Error(t, planner.generate(context.Background(), settings.AdminID, &settings))
	require.JSONEq(t, bloomSecurityProfiles, func() string {
		payload, err := json.Marshal(settings.Profiles)
		require.NoError(t, err)
		return string(payload)
	}())
}

func TestBloomPlannerSecurityRejectsUserAccessToConfiguration(t *testing.T) {
	t.Parallel()
	for _, identity := range []struct {
		userID int64
		role   string
		status int
	}{{0, "", http.StatusUnauthorized}, {42, service.RoleUser, http.StatusForbidden}, {42, "", http.StatusForbidden}} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			planner, store, keys, model := newBloomSecurityPlanner(t)
			handler := &BloomHandler{planner: planner}
			ginContext, recorder := bloomSecurityContext(method, "/api/v1/admin/settings/bloom-planner", `{"enabled":false}`, identity.userID, identity.role)
			if method == http.MethodGet {
				handler.Planner(ginContext)
			} else {
				handler.UpdatePlanner(ginContext)
			}
			require.Equal(t, identity.status, recorder.Code)
			require.Zero(t, store.reads.Load())
			require.Zero(t, store.writes)
			require.Zero(t, keys.calls.Load())
			require.Zero(t, model.calls.Load())
			require.NotContains(t, recorder.Body.String(), "private")
		}
	}
}

func bloomSecurityCheckGardenResponse(t *testing.T, recorder *httptest.ResponseRecorder) bloomGarden {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	fields := []string{"day", "week_start", "resets_at", "week_resets_at", "timezone", "budget_micros", "claimed_micros", "collected_flowers", "tasks"}
	require.Len(t, envelope.Data, len(fields))
	for field := range envelope.Data {
		require.Contains(t, fields, field)
	}
	var tasks []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Data["tasks"], &tasks))
	taskFields := []string{"id", "template_id", "tier", "metric", "target", "progress", "reward_micros", "claimed"}
	for _, task := range tasks {
		require.Len(t, task, len(taskFields))
		for field := range task {
			require.Contains(t, taskFields, field)
		}
	}
	for _, secret := range []string{"sk-private-planner-credential", "private-admin@example.test", "private-planner-model"} {
		require.NotContains(t, recorder.Body.String(), secret)
	}
	payload, err := json.Marshal(envelope.Data)
	require.NoError(t, err)
	var garden bloomGarden
	require.NoError(t, json.Unmarshal(payload, &garden))
	return garden
}

func TestBloomPlannerSecurityGardenResponsesAndSnapshots(t *testing.T) {
	t.Parallel()
	for _, initialState := range []string{"available", "disabled key", "invalid profiles"} {
		t.Run(initialState, func(t *testing.T) {
			t.Parallel()
			planner, settings, keys, model := newBloomSecurityPlanner(t)
			switch initialState {
			case "disabled key":
				keys.key.Status = service.StatusAPIKeyDisabled
			case "invalid profiles":
				saved, err := planner.current(context.Background())
				require.NoError(t, err)
				saved.Profiles = nil
				bloomSecuritySaveSettings(t, settings, saved)
			}
			store := &gardenStoreFixture{}
			handler := &BloomHandler{store: store, planner: planner}
			ginContext, recorder := bloomSecurityContext(http.MethodGet, "/api/v1/bloom/tasks", "", 42, service.RoleUser)
			handler.List(ginContext)
			listed := bloomSecurityCheckGardenResponse(t, recorder)
			require.Len(t, listed.Tasks, 4)
			require.Len(t, store.week.Tasks, 22)
			easyReward := int64(100_000)
			if initialState == "available" {
				easyReward = 200_000
			}
			require.Equal(t, easyReward, listed.Tasks[0].RewardMicros)
			require.Zero(t, model.calls.Load())
			snapshot, err := json.Marshal(store.week)
			require.NoError(t, err)
			reads, keyCalls := settings.reads.Load(), keys.calls.Load()
			keys.key.Status = service.StatusAPIKeyDisabled
			keys.key.User.Role = service.RoleUser
			settings.value = `{"enabled":false}`
			ginContext, recorder = bloomSecurityContext(http.MethodGet, "/api/v1/bloom/tasks", "", 42, service.RoleUser)
			handler.List(ginContext)
			bloomSecurityCheckGardenResponse(t, recorder)
			again, err := json.Marshal(store.week)
			require.NoError(t, err)
			require.Equal(t, string(snapshot), string(again))
			require.Equal(t, reads, settings.reads.Load())
			require.Equal(t, keyCalls, keys.calls.Load())
			ginContext, recorder = bloomSecurityContext(http.MethodPost, "/api/v1/bloom/tasks/"+listed.Tasks[0].ID+"/claim", "", 42, service.RoleUser)
			ginContext.Params = gin.Params{{Key: "id", Value: listed.Tasks[0].ID}}
			handler.Claim(ginContext)
			bloomSecurityCheckGardenResponse(t, recorder)
			require.Equal(t, 1, store.claimCalls)
			require.Equal(t, reads, settings.reads.Load())
			require.Equal(t, keyCalls, keys.calls.Load())
			require.Zero(t, model.calls.Load())
		})
	}
}

func TestBloomPlannerSecurityConcurrentFirstVisitsNeverCallModel(t *testing.T) {
	t.Parallel()
	planner, settings, keys, model := newBloomSecurityPlanner(t)
	const users = 32
	type result struct {
		garden *bloomGarden
		err    error
	}
	results := make(chan result, users)
	for userID := int64(100); userID < 100+users; userID++ {
		go func() {
			handler := &BloomHandler{store: &gardenStoreFixture{}, planner: planner}
			garden, err := handler.garden(context.Background(), userID, time.Now(), time.UTC)
			results <- result{garden: garden, err: err}
		}()
	}
	for range users {
		current := <-results
		require.NoError(t, current.err)
		require.Len(t, current.garden.Tasks, 4)
		require.EqualValues(t, 200_000, current.garden.Tasks[0].RewardMicros)
	}
	require.EqualValues(t, users, keys.calls.Load())
	require.EqualValues(t, users, settings.reads.Load())
	require.Zero(t, settings.writes)
	require.Zero(t, model.calls.Load())
}

func TestBloomPlannerSecurityGenerationDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	planner, _, _, _ := newBloomSecurityPlanner(t)
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		forwarded.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		http.Redirect(writer, req, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	planner.endpoint = source.URL
	planner.client.Transport = http.DefaultTransport
	settings, err := planner.current(context.Background())
	require.NoError(t, err)
	require.Error(t, planner.generate(context.Background(), settings.AdminID, &settings))
	require.Zero(t, forwarded.Load())
}
