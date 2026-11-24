package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	apperrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type gardenStoreFixture struct {
	service.BloomRepository
	week           service.BloomWeek
	prior          service.BloomUsageStats
	priorEnd       time.Time
	legacySpend    decimal.Decimal
	aggregateCalls int
	claimCalls     int
}

func (s *gardenStoreFixture) GetWeek(context.Context, int64, time.Time) (service.BloomWeek, error) {
	return s.week, nil
}

func (s *gardenStoreFixture) EnsureTasks(_ context.Context, _ int64, _ time.Time, budget int64, drafts []service.BloomTaskDraft) (service.BloomWeek, error) {
	s.week.Exists, s.week.BudgetMicros = true, budget
	for _, draft := range drafts {
		s.week.Tasks = append(s.week.Tasks, service.BloomTask{ID: int64(len(s.week.Tasks)) + 1, BloomTaskDraft: draft})
	}
	return s.week, nil
}

func (s *gardenStoreFixture) UsageStats(_ context.Context, _ int64, _, end time.Time, _ string) (service.BloomUsageStats, error) {
	if end.Equal(s.priorEnd) {
		s.aggregateCalls++
		return s.prior, nil
	}
	return service.BloomUsageStats{}, nil
}

func (s *gardenStoreFixture) LegacyWalletSpend(context.Context, int64, time.Time, time.Time) (decimal.Decimal, error) {
	return s.legacySpend, nil
}

func TestBloomFirstVisitIncludesEligibleLegacySpend(t *testing.T) {
	monday := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	store := &gardenStoreFixture{
		priorEnd:    monday,
		prior:       service.BloomUsageStats{TotalRecords: 20, WalletCost: decimal.NewFromInt(20)},
		legacySpend: decimal.NewFromInt(60),
	}
	handler := BloomHandler{store: store, legacyWalletSpend: true}
	view, err := handler.garden(t.Context(), 42, monday.Add(time.Hour), time.UTC)
	require.NoError(t, err)
	require.EqualValues(t, 8_000_000, view.BudgetMicros)
	require.EqualValues(t, 80_000, view.Tasks[0].RewardMicros)
	store.legacySpend = decimal.NewFromInt(100)
	again, err := handler.garden(t.Context(), 42, monday.Add(2*time.Hour), time.UTC)
	require.NoError(t, err)
	require.Equal(t, view, again)
}

func (s *gardenStoreFixture) CountClaimedWeekly(context.Context, int64) (int64, error) {
	return 3, nil
}

func (s *gardenStoreFixture) ClaimTask(_ context.Context, _, taskID int64, _ time.Time) (service.BloomClaim, error) {
	s.claimCalls++
	for index := range s.week.Tasks {
		task := &s.week.Tasks[index]
		if task.ID == taskID {
			now := time.Now()
			task.ClaimedAt = &now
			s.week.SpentMicros += task.RewardMicros
			return service.BloomClaim{}, nil
		}
	}
	return service.BloomClaim{}, service.ErrBloomTaskNotFound
}

func TestBloomGardenFreezesIssuedRewardsAndDeniesUnearnedClaims(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	at := time.Date(2026, 11, 23, 13, 0, 0, 0, zone)
	store := &gardenStoreFixture{
		priorEnd: bloomrules.Calendar(at, zone).Week.Start,
		prior:    service.BloomUsageStats{TotalRecords: 60, WalletCost: decimal.RequireFromString("80.0000099")},
	}
	ctx := context.Background()
	h := BloomHandler{store: store}
	view, err := h.garden(ctx, 42, at, zone)
	require.NoError(t, err)
	require.Len(t, view.Tasks, 4)
	require.EqualValues(t, 8_000_000, view.BudgetMicros)
	require.Equal(t, []int64{80_000, 240_000, 480_000, 2_400_000},
		[]int64{view.Tasks[0].RewardMicros, view.Tasks[1].RewardMicros, view.Tasks[2].RewardMicros, view.Tasks[3].RewardMicros})
	require.ErrorIs(t, h.claim(ctx, 42, 5, view), service.ErrBloomTaskNotFound)
	for _, task := range view.Tasks[1:] {
		id, err := strconv.ParseInt(task.ID, 10, 64)
		require.NoError(t, err)
		require.Equal(t, http.StatusConflict, apperrors.Code(h.claim(ctx, 42, id, view)))
	}
	require.Zero(t, store.claimCalls)
	store.prior.WalletCost = decimal.Zero
	again, err := h.garden(ctx, 42, at.Add(time.Minute), zone)
	require.NoError(t, err)
	require.Equal(t, view, again)
	require.Equal(t, 1, store.aggregateCalls)
	require.Len(t, store.week.Tasks, 22)
	var issuedTotal int64
	for _, task := range store.week.Tasks {
		issuedTotal += task.RewardMicros
	}
	require.Equal(t, view.BudgetMicros, issuedTotal)
	sunday, err := h.garden(ctx, 42, at.AddDate(0, 0, 6), zone)
	require.NoError(t, err)
	require.Len(t, sunday.Tasks, 4)
	require.Equal(t, view.Tasks[3], sunday.Tasks[3])
	require.NotEqual(t, view.Tasks[0].ID, sunday.Tasks[0].ID)
	require.Equal(t, 1, store.aggregateCalls)
	require.Len(t, store.week.Tasks, 22)
}

func TestBloomCheckinAndRetry(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	store := &gardenStoreFixture{week: service.BloomWeek{Exists: true, BudgetMicros: 10_000_000,
		Tasks: []service.BloomTask{{ID: 7, BloomTaskDraft: service.BloomTaskDraft{Difficulty: service.BloomDailyEasy,
			PeriodStart: monday, RewardMicros: 100_000}}}}}
	h := BloomHandler{store: store}
	ctx := context.Background()
	view := &bloomGarden{ResetsAt: monday.AddDate(0, 0, 1), WeekResetsAt: monday.AddDate(0, 0, 7),
		Tasks: []bloomTaskView{{ID: "7", Tier: bloomrules.Easy, Target: 1, Metric: bloomrules.CheckIn, RewardMicros: 100_000}}}
	require.NoError(t, h.claim(ctx, 5, 7, view))
	require.NoError(t, h.claim(ctx, 5, 7, view))
	require.Equal(t, 1, store.claimCalls)
	require.EqualValues(t, 100_000, view.ClaimedMicros)
	require.EqualValues(t, 1, view.Tasks[0].Progress)
	require.True(t, view.Tasks[0].Claimed)
}

type bloomAlertTestStore struct {
	gardenStoreFixture
	claimErr error
}

func (s *bloomAlertTestStore) ClaimTask(context.Context, int64, int64, time.Time) (service.BloomClaim, error) {
	return service.BloomClaim{AlertEventID: 31}, s.claimErr
}

type bloomAlertRecorder chan int64

func (r bloomAlertRecorder) NotifyBloomBudgetAlert(_ context.Context, id int64) { r <- id }

func TestBloomNotifiesOnReachingOrExceedingCap(t *testing.T) {
	for _, claimErr := range []error{nil, service.ErrBloomBudgetExceeded} {
		store := &bloomAlertTestStore{claimErr: claimErr}
		alerts := make(bloomAlertRecorder, 1)
		handler := BloomHandler{store: store, alerts: alerts}
		garden := &bloomGarden{Tasks: []bloomTaskView{{ID: "3", Tier: bloomrules.Weekly, Progress: 1, Target: 1}}}
		require.ErrorIs(t, handler.claim(t.Context(), 1, 3, garden), claimErr)
		select {
		case id := <-alerts:
			require.EqualValues(t, 31, id)
		case <-time.After(time.Second):
			t.Fatal("committed admin alert was not dispatched")
		}
	}
}

func TestBloomFirstVisitWithNoUsageOffersStarterCredit(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h := BloomHandler{store: &gardenStoreFixture{}}
	garden, err := h.garden(context.Background(), 8, at, time.UTC)
	require.NoError(t, err)
	require.Equal(t, bloomrules.CheckIn, garden.Tasks[0].Metric)
	require.EqualValues(t, 100_000, garden.Tasks[0].RewardMicros)
	require.EqualValues(t, 10_000_000, garden.BudgetMicros)
	for _, task := range garden.Tasks[1:] {
		require.NotEqual(t, bloomrules.CheckIn, task.Metric)
	}
}

func TestBloomSettledImageChargeWithoutUsageLog(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	store := &gardenStoreFixture{
		priorEnd: bloomrules.Calendar(at, time.UTC).Week.Start,
		prior:    service.BloomUsageStats{WalletCost: decimal.RequireFromString("0.25")},
	}
	h := BloomHandler{store: store}
	garden, err := h.garden(context.Background(), 8, at, time.UTC)
	require.NoError(t, err)
	require.EqualValues(t, 25_000, garden.BudgetMicros)
}

type plannerKeyFixture struct{ key service.APIKey }

func (p plannerKeyFixture) GetByID(context.Context, int64) (*service.APIKey, error) {
	return &p.key, nil
}

func TestBloomPlannerOwnKeyAndBoundedRewards(t *testing.T) {
	t.Parallel()
	settings := bloomPlannerSettings{Enabled: true, KeyID: 8, Model: "fixture-model"}
	planner := newBloomPlanner(nil, plannerKeyFixture{service.APIKey{ID: 8, UserID: 9, Key: "fixture-key", Status: service.StatusActive,
		User: &service.User{ID: 9, Role: service.RoleAdmin, Status: service.StatusActive}}}, 0)
	require.Equal(t, http.StatusBadRequest, apperrors.Code(planner.generate(context.Background(), 11, &settings)))
	profiles := make(map[bloomrules.Modality]bloomSplit)
	for _, modality := range []bloomrules.Modality{bloomrules.ColdStart, bloomrules.Text, bloomrules.Image, bloomrules.Mixed} {
		profiles[modality] = bloomSplit{Easy: 2, Medium: 3, Hard: 5}
	}
	answer, err := json.Marshal(profiles)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer fixture-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(answer)}}}})
	}))
	defer server.Close()
	planner.endpoint = server.URL
	require.NoError(t, planner.generate(context.Background(), 9, &settings))
	split := settings.Profiles[bloomrules.Text]
	require.Equal(t, profiles[bloomrules.Text], split)
	for _, budget := range []int64{0, 1, 999_999, 10_000_000} {
		total := split.reward(budget, bloomrules.Weekly)
		for _, tier := range []bloomrules.Tier{bloomrules.Easy, bloomrules.Medium, bloomrules.Hard} {
			total += 7 * split.reward(budget, tier)
		}
		require.LessOrEqual(t, total, budget)
	}
	answer = []byte(strings.ReplaceAll(string(answer), `"hard":5`, `"hard":99`))
	require.Equal(t, http.StatusBadGateway, apperrors.Code(planner.generate(context.Background(), 9, &settings)))
}
