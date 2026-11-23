package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	"github.com/Wei-Shaw/sub2api/internal/config"
	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type BloomHandler struct {
	store    service.BloomRepository
	balances *service.BillingCacheService
	keys     *service.APIKeyService
	planner  *bloomPlanner
	alerts   interface {
		NotifyBloomBudgetAlert(context.Context, int64)
	}
}

func NewBloomHandler(store service.BloomRepository, balances *service.BillingCacheService, keys *service.APIKeyService, settings service.SettingRepository, cfg *config.Config, alerts *service.OpsAlertEvaluatorService) *BloomHandler {
	return &BloomHandler{store: store, balances: balances, keys: keys, planner: newBloomPlanner(settings, keys, cfg.Server.Port), alerts: alerts}
}

type bloomTaskView struct {
	ID           string            `json:"id"`
	TemplateID   string            `json:"template_id"`
	Tier         bloomrules.Tier   `json:"tier"`
	Metric       bloomrules.Metric `json:"metric"`
	Target       int64             `json:"target"`
	Progress     int64             `json:"progress"`
	RewardMicros int64             `json:"reward_micros"`
	Claimed      bool              `json:"claimed"`
}

type bloomGarden struct {
	Day              string          `json:"day"`
	WeekStart        string          `json:"week_start"`
	ResetsAt         time.Time       `json:"resets_at"`
	WeekResetsAt     time.Time       `json:"week_resets_at"`
	Timezone         string          `json:"timezone"`
	BudgetMicros     int64           `json:"budget_micros"`
	ClaimedMicros    int64           `json:"claimed_micros"`
	CollectedFlowers int64           `json:"collected_flowers"`
	Tasks            []bloomTaskView `json:"tasks"`
}

func bloomStats(usage service.BloomUsageStats) bloomrules.Stats {
	return bloomrules.Stats{Requests: usage.TotalRecords, ProductiveRequests: usage.OutputRequests,
		OutputTokens: usage.OutputTokens, Images: usage.ImageCount, ActiveDays: usage.ActiveDays}
}

func (h *BloomHandler) stats(ctx context.Context, userID int64, window bloomrules.Window) (service.BloomUsageStats, error) {
	if !window.Start.Before(window.End) {
		return service.BloomUsageStats{}, nil
	}
	return h.store.UsageStats(ctx, userID, window.Start, window.End, window.Start.Location().String())
}

func currentBloomTasks(week service.BloomWeek, day time.Time) map[bloomrules.Tier]service.BloomTask {
	result := make(map[bloomrules.Tier]service.BloomTask, 4)
	for _, task := range week.Tasks {
		if task.Difficulty == service.BloomWeekly || task.PeriodStart.Equal(day) {
			result[bloomrules.Tier(task.Difficulty)] = task
		}
	}
	return result
}

func (h *BloomHandler) ensureWeek(ctx context.Context, userID int64, at time.Time, periods bloomrules.Periods) (service.BloomWeek, error) {
	week, err := h.store.GetWeek(ctx, userID, periods.Week.Start)
	if err != nil {
		return week, err
	}
	const tasksPerWeek = 3*7 + 1
	if len(week.Tasks) == tasksPerWeek {
		return week, nil
	}
	prior, err := h.stats(ctx, userID, periods.PriorWeek)
	if err != nil {
		return week, err
	}
	completed, err := h.stats(ctx, userID, periods.CompletedDays)
	if err != nil {
		return week, err
	}
	spend := decimal.Min(prior.WalletCost.Shift(6), decimal.NewFromInt(math.MaxInt64))
	priorStats := bloomStats(prior)
	if prior.WalletCost.IsPositive() && priorStats.Requests == 0 {
		priorStats.Requests = 1
	}
	schedule := bloomrules.IssueWeek(bloomrules.IssueInput{AccountID: userID, At: at, Location: periods.Day.Start.Location(),
		PriorWeek: priorStats, CompletedDays: bloomStats(completed), PriorWeekWalletSpendMicros: spend.IntPart()})
	if !week.Exists && priorStats.Requests == 0 && completed.TotalRecords == 0 && !completed.WalletCost.IsPositive() {
		for _, template := range bloomrules.Templates(bloomrules.Easy) {
			if template.ColdMetric == bloomrules.CheckIn {
				dayIndex := (int(periods.Day.Start.Weekday()) + 6) % 7
				easy := &schedule[dayIndex].Daily[0]
				easy.TemplateID, easy.Metric, easy.Target = template.ID, bloomrules.CheckIn, 1
				break
			}
		}
	}
	budget := schedule[0].WeeklyBudgetMicros
	if week.Exists {
		budget = week.BudgetMicros
	}
	modality := bloomrules.Classify(bloomStats(completed))
	if modality == bloomrules.ColdStart {
		modality = bloomrules.Classify(bloomStats(prior))
	}
	split := h.planner.split(ctx, modality)
	issue := func(budget int64) (service.BloomWeek, error) {
		drafts, err := newBloomDrafts(schedule, split, budget)
		if err != nil {
			return service.BloomWeek{}, err
		}
		return h.store.EnsureTasks(ctx, userID, periods.Week.Start, budget, drafts)
	}
	week, err = issue(budget)
	if errors.Is(err, service.ErrBloomBudgetChanged) {
		if len(week.Tasks) == tasksPerWeek {
			return week, nil
		}
		return issue(week.BudgetMicros)
	}
	return week, err
}

func newBloomDrafts(schedule [7]bloomrules.Assignment, split bloomSplit, budget int64) ([]service.BloomTaskDraft, error) {
	var challenges []bloomrules.Challenge
	for _, assignment := range schedule {
		challenges = append(challenges, assignment.Daily[:]...)
	}
	challenges = append(challenges, schedule[0].Weekly)
	drafts := make([]service.BloomTaskDraft, 0, len(challenges))
	for _, challenge := range challenges {
		challenge.RewardMicros = split.reward(budget, challenge.Tier)
		template, err := json.Marshal(challenge)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, service.BloomTaskDraft{PeriodStart: challenge.Window.Start,
			Difficulty: service.BloomDifficulty(challenge.Tier), Template: template, RewardMicros: challenge.RewardMicros})
	}
	return drafts, nil
}

func (h *BloomHandler) garden(ctx context.Context, userID int64, at time.Time, location *time.Location) (*bloomGarden, error) {
	periods := bloomrules.Calendar(at, location)
	week, err := h.ensureWeek(ctx, userID, at, periods)
	if err != nil {
		return nil, err
	}
	today, err := h.stats(ctx, userID, bloomrules.Window{Start: periods.Day.Start, End: at})
	if err != nil {
		return nil, err
	}
	fullWeek := today
	if !periods.Day.Start.Equal(periods.Week.Start) {
		fullWeek, err = h.stats(ctx, userID, bloomrules.Window{Start: periods.Week.Start, End: at})
		if err != nil {
			return nil, err
		}
	}
	flowers, err := h.store.CountClaimedWeekly(ctx, userID)
	if err != nil {
		return nil, err
	}
	view := &bloomGarden{Day: periods.Day.Start.Format(time.DateOnly), WeekStart: periods.Week.Start.Format(time.DateOnly),
		ResetsAt: periods.Day.End, WeekResetsAt: periods.Week.End, Timezone: location.String(),
		BudgetMicros: week.BudgetMicros, ClaimedMicros: week.SpentMicros, CollectedFlowers: flowers}
	current := currentBloomTasks(week, periods.Day.Start)
	for _, tier := range [...]bloomrules.Tier{bloomrules.Easy, bloomrules.Medium, bloomrules.Hard, bloomrules.Weekly} {
		task := current[tier]
		var challenge bloomrules.Challenge
		if err := json.Unmarshal(task.Template, &challenge); err != nil {
			return nil, err
		}
		if challenge.Tier != tier || challenge.Target < 1 {
			return nil, errors.New("invalid persisted bloom challenge")
		}
		source := today
		if tier == bloomrules.Weekly {
			source = fullWeek
		}
		claimed := task.ClaimedAt != nil
		progress := bloomrules.Progress(challenge, bloomStats(source), claimed).Current
		if claimed {
			progress = challenge.Target
		}
		view.Tasks = append(view.Tasks, bloomTaskView{ID: strconv.FormatInt(task.ID, 10), Tier: tier,
			TemplateID: challenge.TemplateID, Metric: challenge.Metric, Target: challenge.Target,
			Progress: min(progress, challenge.Target), RewardMicros: task.RewardMicros, Claimed: claimed})
	}
	return view, nil
}

func (h *BloomHandler) List(c *gin.Context)  { h.serveGarden(c, false) }
func (h *BloomHandler) Claim(c *gin.Context) { h.serveGarden(c, true) }

func (h *BloomHandler) serveGarden(c *gin.Context, claiming bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var taskID int64
	if claiming {
		var err error
		taskID, err = strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || taskID < 1 {
			response.BadRequest(c, "Invalid task ID")
			return
		}
	}
	ctx := c.Request.Context()
	garden, err := h.garden(ctx, subject.UserID, time.Now(), timezone.Location())
	if err == nil && claiming {
		err = h.claim(ctx, subject.UserID, taskID, garden)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBloomTaskNotFound):
			response.NotFound(c, "Task is outside the current garden")
		case errors.Is(err, service.ErrBloomExpired), errors.Is(err, service.ErrBloomBudgetExceeded):
			response.Error(c, http.StatusConflict, "Task reward is no longer available")
		default:
			response.ErrorFrom(c, err)
		}
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, garden)
}

func (h *BloomHandler) claim(ctx context.Context, userID, taskID int64, garden *bloomGarden) error {
	for index := range garden.Tasks {
		task := &garden.Tasks[index]
		if task.ID != strconv.FormatInt(taskID, 10) {
			continue
		}
		if task.Claimed {
			return nil
		}
		checkIn := task.Tier == bloomrules.Easy && task.Metric == bloomrules.CheckIn
		if task.Progress < task.Target && !checkIn {
			return apperrors.Conflict("BLOOM_TASK_INCOMPLETE", "Task is still in progress")
		}
		expires := garden.ResetsAt
		if task.Tier == bloomrules.Weekly {
			expires = garden.WeekResetsAt
		}
		claim, err := h.store.ClaimTask(ctx, userID, taskID, expires)
		if claim.AlertEventID > 0 && h.alerts != nil {
			go func() {
				notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				h.alerts.NotifyBloomBudgetAlert(notifyCtx, claim.AlertEventID)
			}()
		}
		if err != nil {
			return err
		}
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if claim.Applied && claim.RewardMicros > 0 {
			_ = h.balances.InvalidateUserBalance(cacheCtx, userID)
			h.keys.InvalidateAuthCacheByUserID(cacheCtx, userID)
		}
		week, err := h.store.GetWeek(ctx, userID, garden.WeekResetsAt.AddDate(0, 0, -7))
		if err != nil {
			return err
		}
		garden.ClaimedMicros = week.SpentMicros
		current := currentBloomTasks(week, garden.ResetsAt.AddDate(0, 0, -1))
		for index := range garden.Tasks {
			row := &garden.Tasks[index]
			if current[row.Tier].ClaimedAt != nil {
				row.Claimed, row.Progress = true, row.Target
			}
		}
		garden.CollectedFlowers, err = h.store.CountClaimedWeekly(ctx, userID)
		return err
	}
	return service.ErrBloomTaskNotFound
}
