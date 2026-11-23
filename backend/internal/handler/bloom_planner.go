package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const bloomPlannerKey = "custom_bloom_planner_v1"

type bloomSplit struct {
	Easy   int64 `json:"easy"`
	Medium int64 `json:"medium"`
	Hard   int64 `json:"hard"`
}

var defaultBloomSplit = bloomSplit{Easy: 1, Medium: 3, Hard: 6}

func (s bloomSplit) valid() bool {
	return s.Easy >= 1 && s.Easy <= 2 && s.Medium > s.Easy && s.Medium <= 4 &&
		s.Hard > s.Medium && s.Hard == 10-s.Easy-s.Medium
}

func (s bloomSplit) reward(budget int64, tier bloomrules.Tier) int64 {
	if !s.valid() {
		s = defaultBloomSplit
	}
	var rate int64
	switch tier {
	case bloomrules.Easy:
		rate = s.Easy
	case bloomrules.Medium:
		rate = s.Medium
	case bloomrules.Hard:
		rate = s.Hard
	default:
		return bloomrules.RewardMicros(tier, budget)
	}
	return budget/100*rate + budget%100*rate/100
}

type bloomPlannerSettings struct {
	Enabled  bool                               `json:"enabled"`
	KeyID    int64                              `json:"key_id"`
	AdminID  int64                              `json:"admin_id,omitempty"`
	Model    string                             `json:"model"`
	Profiles map[bloomrules.Modality]bloomSplit `json:"profiles,omitempty"`
}

func validBloomProfiles(profiles map[bloomrules.Modality]bloomSplit) bool {
	return len(profiles) == 4 && profiles[bloomrules.ColdStart].valid() && profiles[bloomrules.Text].valid() &&
		profiles[bloomrules.Image].valid() && profiles[bloomrules.Mixed].valid()
}

type bloomKeyReader interface {
	GetByID(context.Context, int64) (*service.APIKey, error)
}

type bloomPlanner struct {
	settings service.SettingRepository
	keys     bloomKeyReader
	endpoint string
	client   *http.Client
}

func newBloomPlanner(settings service.SettingRepository, keys bloomKeyReader, port int) *bloomPlanner {
	return &bloomPlanner{settings: settings, keys: keys,
		endpoint: "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/chat/completions",
		client: &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second},
			Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (p *bloomPlanner) current(ctx context.Context) (bloomPlannerSettings, error) {
	var settings bloomPlannerSettings
	if p == nil || p.settings == nil {
		return settings, nil
	}
	raw, err := p.settings.GetValue(ctx, bloomPlannerKey)
	if errors.Is(err, service.ErrSettingNotFound) {
		return settings, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &settings)
	}
	return settings, err
}

func (p *bloomPlanner) split(ctx context.Context, modality bloomrules.Modality) bloomSplit {
	switch modality {
	case bloomrules.ColdStart, bloomrules.Text, bloomrules.Image, bloomrules.Mixed:
	default:
		return defaultBloomSplit
	}
	settings, err := p.current(ctx)
	if err != nil || !settings.Enabled || !validBloomProfiles(settings.Profiles) {
		return defaultBloomSplit
	}
	if _, err := p.activeKey(ctx, settings.AdminID, settings.KeyID); err != nil {
		return defaultBloomSplit
	}
	return settings.Profiles[modality]
}

const bloomPlannerPrompt = `Design daily task reward splits for four anonymous activity profiles: cold_start (new), text, image, mixed.
Prioritize an attainable first reward and sustained productive effort. Reply with exactly one JSON object, keyed by cold_start, text, image, mixed.
Each value must be an object with integer keys easy, medium, hard. Pick one valid split per profile: [1,2,7], [1,3,6], [1,4,5], [2,3,5].
These are percentages of a fixed weekly credit budget per day. The separate weekly challenge already has 30%. Return no markdown.`

func (p *bloomPlanner) activeKey(ctx context.Context, adminID, keyID int64) (*service.APIKey, error) {
	badKey := apperrors.BadRequest("BLOOM_PLANNER_KEY", "Choose an active model API key owned by your active administrator account")
	if p == nil || p.keys == nil || adminID < 1 || keyID < 1 {
		return nil, badKey
	}
	key, err := p.keys.GetByID(ctx, keyID)
	if err != nil || key == nil || key.ID != keyID || key.UserID != adminID || strings.TrimSpace(key.Key) == "" ||
		!key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() ||
		key.User == nil || key.User.ID != adminID || !key.User.IsAdmin() || !key.User.IsActive() || key.User.DeletedAt != nil {
		return nil, badKey
	}
	if key.GroupID != nil && (key.Group == nil || key.Group.ID != *key.GroupID || !key.Group.IsActive()) {
		return nil, badKey
	}
	if key.RateLimit5h > 0 && key.EffectiveUsage5h() >= key.RateLimit5h ||
		key.RateLimit1d > 0 && key.EffectiveUsage1d() >= key.RateLimit1d ||
		key.RateLimit7d > 0 && key.EffectiveUsage7d() >= key.RateLimit7d {
		return nil, badKey
	}
	return key, nil
}

func (p *bloomPlanner) generate(ctx context.Context, adminID int64, settings *bloomPlannerSettings) error {
	if settings == nil {
		return apperrors.BadRequest("BLOOM_PLANNER_KEY", "Choose an active model API key owned by your active administrator account")
	}
	key, err := p.activeKey(ctx, adminID, settings.KeyID)
	if err != nil {
		return err
	}
	model := strings.TrimSpace(settings.Model)
	if model == "" || len(model) > 200 {
		return apperrors.BadRequest("BLOOM_PLANNER_MODEL", "Choose a valid model")
	}
	badResponse := apperrors.New(http.StatusBadGateway, "BLOOM_PLANNER_RESPONSE", "The planner could not generate a valid reward split with this key and model")
	if p.client == nil {
		return badResponse
	}
	payload, err := json.Marshal(gin.H{"model": model, "stream": false, "max_tokens": 400,
		"messages": []gin.H{{"role": "user", "content": bloomPlannerPrompt}}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key.Key)
	res, err := p.client.Do(req)
	if err != nil {
		return badResponse
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return badResponse
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&envelope); err != nil || len(envelope.Choices) == 0 {
		return badResponse
	}
	content := strings.TrimSpace(envelope.Choices[0].Message.Content)
	if strings.HasPrefix(content, "```json") && strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```")
	}
	var profiles map[bloomrules.Modality]bloomSplit
	if err := json.Unmarshal([]byte(content), &profiles); err != nil || !validBloomProfiles(profiles) {
		return badResponse
	}
	settings.Profiles = profiles
	return nil
}

func bloomPlannerAdmin(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID < 1 {
		response.Unauthorized(c, "Administrator login required")
		return 0, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator login required")
		return 0, false
	}
	return subject.UserID, true
}

func (h *BloomHandler) Planner(c *gin.Context) {
	if _, ok := bloomPlannerAdmin(c); !ok {
		return
	}
	settings, err := h.planner.current(c.Request.Context())
	if !response.ErrorFrom(c, err) {
		response.Success(c, settings)
	}
}

func (h *BloomHandler) UpdatePlanner(c *gin.Context) {
	adminID, ok := bloomPlannerAdmin(c)
	if !ok {
		return
	}
	var input struct {
		Enabled *bool  `json:"enabled"`
		KeyID   int64  `json:"key_id"`
		Model   string `json:"model"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		response.BadRequest(c, "Invalid reward planner configuration")
		return
	}
	ctx := c.Request.Context()
	var settings bloomPlannerSettings
	if *input.Enabled {
		settings = bloomPlannerSettings{Enabled: true, KeyID: input.KeyID, AdminID: adminID, Model: strings.TrimSpace(input.Model)}
		if settings.KeyID < 1 || settings.Model == "" || len(settings.Model) > 200 {
			response.BadRequest(c, "Model and key are required")
			return
		}
		if err := h.planner.generate(ctx, adminID, &settings); response.ErrorFrom(c, err) {
			return
		}
	}
	payload, err := json.Marshal(settings)
	if err == nil {
		err = h.planner.settings.Set(ctx, bloomPlannerKey, string(payload))
	}
	if !response.ErrorFrom(c, err) {
		response.Success(c, settings)
	}
}
