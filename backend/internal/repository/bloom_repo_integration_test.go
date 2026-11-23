//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func newBloomTestDatabase(t *testing.T) (*sql.DB, *dbent.Client, string) {
	t.Helper()
	connection, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	name := fmt.Sprintf("bloom_test_%d", time.Now().UnixNano())
	_, err = integrationDB.ExecContext(t.Context(), "CREATE DATABASE "+name+" TEMPLATE template0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DROP DATABASE "+name)
		require.NoError(t, err)
	})
	connection.Path = "/" + name
	db, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, ApplyMigrations(t.Context(), db))
	return db, client, connection.String()
}

func TestBloomClaimConcurrencyAndSnapshots(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 1})
	other := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	drafts := []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{"goal":5}`), RewardMicros: 600_001},
		{PeriodStart: monday, Difficulty: BloomDailyMedium, Template: json.RawMessage(`{"goal":10}`), RewardMicros: 600_000},
		{PeriodStart: monday, Difficulty: BloomDailyHard, Template: json.RawMessage(`{"goal":15}`), RewardMicros: 400_000},
		{PeriodStart: monday, Difficulty: BloomWeekly, Template: json.RawMessage(`{"goal":20}`), RewardMicros: 300_000},
	}
	initial, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.False(t, initial.Exists)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 900_001, drafts)
	require.NoError(t, err)
	require.Len(t, week.Tasks, 4)
	changed := drafts[0]
	changed.RewardMicros, changed.Template = 1, json.RawMessage(`{"goal":1}`)
	snapshot, err := repo.EnsureTasks(ctx, owner.ID, monday, 900_001, []BloomTaskDraft{changed})
	require.NoError(t, err)
	require.Equal(t, week, snapshot)
	_, err = repo.ClaimTask(ctx, other.ID, week.Tasks[0].ID, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, ErrBloomTaskNotFound)
	_, err = repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, ErrBloomExpired)

	type attempt struct {
		taskID int64
		claim  BloomClaim
		err    error
	}
	const attempts = 8
	results := make(chan attempt, attempts)
	start := make(chan struct{})
	for index := range attempts {
		taskID := week.Tasks[index%2].ID
		go func() {
			<-start
			claim, err := repo.ClaimTask(ctx, owner.ID, taskID, time.Now().Add(time.Hour))
			results <- attempt{taskID, claim, err}
		}()
	}
	close(start)
	var applied attempt
	appliedCount, retries, rejected := 0, 0, 0
	for range attempts {
		result := <-results
		switch {
		case result.err == ErrBloomBudgetExceeded:
			rejected++
		case result.err == nil && result.claim.Applied:
			applied, appliedCount = result, appliedCount+1
		case result.err == nil:
			retries++
		default:
			t.Fatalf("unexpected claim result: %+v", result)
		}
	}
	require.Equal(t, 1, appliedCount)
	require.Equal(t, 3, retries)
	require.Equal(t, 4, rejected)
	require.True(t, decimal.NewFromInt(1).Add(decimal.New(applied.claim.RewardMicros, -6)).Equal(applied.claim.BalanceAfter))
	replay, err := repo.ClaimTask(ctx, owner.ID, applied.taskID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.False(t, replay.Applied)
	require.Equal(t, applied.claim.BalanceAfter, replay.BalanceAfter)
	require.True(t, applied.claim.ClaimedAt.Equal(replay.ClaimedAt))

	for index := range 2 {
		period := monday.AddDate(0, 0, index*7)
		challenge := drafts[3]
		challenge.PeriodStart = period
		stored, err := repo.EnsureTasks(ctx, owner.ID, period, 900_001, []BloomTaskDraft{challenge})
		require.NoError(t, err)
		claim, err := repo.ClaimTask(ctx, owner.ID, stored.Tasks[len(stored.Tasks)-1].ID, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.True(t, claim.Applied)
	}
	count, err := repo.CountClaimedWeekly(ctx, owner.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	count, err = repo.CountClaimedWeekly(ctx, other.ID)
	require.NoError(t, err)
	require.Zero(t, count)
	week, err = repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, applied.claim.RewardMicros+300_000, week.SpentMicros)

	var entries int64
	var credited decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(entry.delta_micros)
		FROM bloom_balance_entries AS entry JOIN bloom_tasks AS task ON task.id = entry.task_id
		WHERE task.user_id = $1
	`, owner.ID).Scan(&entries, &credited))
	require.EqualValues(t, 3, entries)
	require.True(t, credited.Equal(decimal.NewFromInt(applied.claim.RewardMicros+600_000)))
}

func TestBloomConcurrentIssuanceUsesPersistedBudget(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	type issuance struct {
		budget int64
		draft  BloomTaskDraft
		week   BloomWeek
		err    error
	}
	start := make(chan struct{})
	results := make(chan issuance, 2)
	for index, budget := range []int64{100, 200} {
		go func() {
			draft := BloomTaskDraft{PeriodStart: monday.AddDate(0, 0, index), Difficulty: BloomDailyEasy,
				Template: json.RawMessage(`{"goal":1}`), RewardMicros: budget / 10}
			<-start
			week, err := repo.EnsureTasks(ctx, owner.ID, monday, budget, []BloomTaskDraft{draft})
			results <- issuance{budget: budget, draft: draft, week: week, err: err}
		}()
	}
	close(start)
	winner, loser := <-results, <-results
	if winner.err != nil {
		winner, loser = loser, winner
	}
	require.NoError(t, winner.err)
	require.ErrorIs(t, loser.err, ErrBloomBudgetChanged)
	require.Equal(t, winner.budget, loser.week.BudgetMicros)
	require.Equal(t, winner.week, loser.week)
	require.Len(t, loser.week.Tasks, 1)
	snapshot, err := repo.EnsureTasks(ctx, owner.ID, monday, loser.budget, nil)
	require.NoError(t, err)
	require.Equal(t, winner.week, snapshot)
	loser.draft.RewardMicros = snapshot.BudgetMicros / 10
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, snapshot.BudgetMicros, []BloomTaskDraft{loser.draft})
	require.NoError(t, err)
	require.Len(t, week.Tasks, 2)
	for _, task := range week.Tasks {
		require.Equal(t, winner.draft.RewardMicros, task.RewardMicros)
	}
}

func TestBloomClaimWalletFailureRollsBack(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 1, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{"goal":1}`), RewardMicros: 1},
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance = 999999999999.99999999 WHERE id = $1`, owner.ID)
	require.NoError(t, err)
	claim, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Zero(t, claim.AlertEventID)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, week, after)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance = 0 WHERE id = $1`, owner.ID)
	require.NoError(t, err)
	claim, err = repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claim.Applied)
	require.Positive(t, claim.AlertEventID)
	require.Equal(t, claim.AlertEventID, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)["reached"].ID)
	require.True(t, decimal.New(1, -6).Equal(claim.BalanceAfter))
}

func bloomHardLimitAlerts(t *testing.T, integrationDB *sql.DB, userID int64, monday time.Time) map[string]*service.OpsAlertEvent {
	t.Helper()
	ctx := context.Background()
	ops := NewOpsRepository(integrationDB)
	list, err := ops.ListAlertEvents(ctx, &service.OpsAlertEventFilter{Limit: 500, Severity: "P0"})
	require.NoError(t, err)
	alerts := make(map[string]*service.OpsAlertEvent)
	for _, event := range list {
		if event.Dimensions["source"] != "bloom_weekly_hard_limit" ||
			event.Dimensions["user_id"] != strconv.FormatInt(userID, 10) ||
			event.Dimensions["week_start"] != monday.Format(time.DateOnly) {
			continue
		}
		require.Equal(t, "P0", event.Severity)
		require.Contains(t, event.Title, "花园奖励领取")
		require.Contains(t, event.Description, "微单位")
		detail, err := ops.GetAlertEventByID(ctx, event.ID)
		require.NoError(t, err)
		require.Equal(t, event, detail)
		reason, ok := event.Dimensions["reason"].(string)
		require.True(t, ok)
		require.NotContains(t, alerts, reason)
		alerts[reason] = event
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ops_alert_events
		WHERE dimensions ->> 'source' = 'bloom_weekly_hard_limit'
		  AND dimensions ->> 'user_id' = $1 AND dimensions ->> 'week_start' = $2
	`, strconv.FormatInt(userID, 10), monday.Format(time.DateOnly)).Scan(&count))
	require.Len(t, alerts, count)
	return alerts
}

func TestBloomHardLimitNormalBudgetAndAdminAlerts(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 1})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10_000_000, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 10_000_000},
		{PeriodStart: monday, Difficulty: BloomDailyMedium, Template: json.RawMessage(`{}`), RewardMicros: 1_000_000},
		{PeriodStart: monday, Difficulty: BloomDailyHard, Template: json.RawMessage(`{}`), RewardMicros: 1},
		{PeriodStart: monday, Difficulty: BloomWeekly, Template: json.RawMessage(`{}`)},
	})
	require.NoError(t, err)
	expires := time.Now().Add(time.Hour)
	normal, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, expires)
	require.NoError(t, err)
	require.True(t, normal.Applied)
	require.True(t, decimal.NewFromInt(11).Equal(normal.BalanceAfter))
	require.Zero(t, normal.AlertEventID)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
	reached, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[1].ID, expires)
	require.NoError(t, err)
	require.True(t, reached.Applied)
	require.True(t, decimal.NewFromInt(12).Equal(reached.BalanceAfter))
	require.Positive(t, reached.AlertEventID)
	alerts := bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)
	require.Len(t, alerts, 1)
	require.Equal(t, reached.AlertEventID, alerts["reached"].ID)
	require.Equal(t, "10000000", alerts["reached"].Dimensions["budget_micros"])
	require.Equal(t, "11000000", alerts["reached"].Dimensions["hard_limit_micros"])
	require.Contains(t, alerts["reached"].Title, "达到")
	require.NoError(t, NewOpsRepository(integrationDB).UpdateAlertEventStatus(ctx, reached.AlertEventID, service.OpsAlertStatusManualResolved, nil))
	replay, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[1].ID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.False(t, replay.Applied)
	require.Zero(t, replay.AlertEventID)

	before, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.EqualValues(t, 10_000_000, before.BudgetMicros)
	require.EqualValues(t, 11_000_000, before.SpentMicros)
	rejected, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[2].ID, expires)
	require.ErrorIs(t, err, ErrBloomBudgetExceeded)
	require.Positive(t, rejected.AlertEventID)
	require.Equal(t, BloomClaim{AlertEventID: rejected.AlertEventID}, rejected)
	again, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[2].ID, expires)
	require.ErrorIs(t, err, ErrBloomBudgetExceeded)
	require.Equal(t, BloomClaim{}, again)
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, before, after)
	alerts = bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)
	require.Len(t, alerts, 2)
	require.Equal(t, rejected.AlertEventID, alerts["exceeded"].ID)
	require.Equal(t, service.OpsAlertStatusManualResolved, alerts["reached"].Status)
	require.Contains(t, alerts["exceeded"].Description, "整笔拒绝")
	zero, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[3].ID, expires)
	require.NoError(t, err)
	require.True(t, zero.Applied)
	require.Zero(t, zero.AlertEventID)
	require.True(t, reached.BalanceAfter.Equal(zero.BalanceAfter))
	require.Len(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday), 2)
	payload, err := json.Marshal(reached)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "AlertEventID")
	require.NotContains(t, string(payload), "hard_limit")
	var entries int
	var credited int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(entry.delta_micros) FROM bloom_balance_entries AS entry
		JOIN bloom_tasks AS task ON task.id = entry.task_id WHERE task.user_id = $1
	`, owner.ID).Scan(&entries, &credited))
	require.Equal(t, 3, entries)
	require.EqualValues(t, 11_000_000, credited)
}

func TestBloomHardLimitConcurrentClaimsAndEventIDs(t *testing.T) {
	integrationDB, integrationEntClient, integrationDSN := newBloomTestDatabase(t)
	for _, connections := range []int{1, 4} {
		t.Run(strconv.Itoa(connections)+"_connections", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			db, err := sql.Open("postgres", integrationDSN)
			require.NoError(t, err)
			defer db.Close()
			db.SetMaxOpenConns(connections)
			repo := NewBloomRepository(db)
			owner := mustCreateUser(t, integrationEntClient, &service.User{})
			monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
			week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10_000_000, []BloomTaskDraft{
				{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 10_000_000},
				{PeriodStart: monday, Difficulty: BloomDailyMedium, Template: json.RawMessage(`{}`), RewardMicros: 1_000_000},
				{PeriodStart: monday, Difficulty: BloomDailyHard, Template: json.RawMessage(`{}`), RewardMicros: 1_000_000},
			})
			require.NoError(t, err)
			expires := time.Now().Add(time.Hour)
			_, err = repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, expires)
			require.NoError(t, err)
			type attempt struct {
				claim BloomClaim
				err   error
			}
			const attempts = 16
			start := make(chan struct{})
			results := make(chan attempt, attempts)
			for index := range attempts {
				taskID := week.Tasks[1+index%2].ID
				go func() {
					<-start
					claim, err := repo.ClaimTask(ctx, owner.ID, taskID, expires)
					results <- attempt{claim, err}
				}()
			}
			close(start)
			applied, replays, rejected := 0, 0, 0
			eventIDs := make(map[int64]bool)
			for range attempts {
				result := <-results
				switch {
				case result.err == ErrBloomBudgetExceeded:
					rejected++
					require.False(t, result.claim.Applied)
				case result.err == nil && result.claim.Applied:
					applied++
					require.Positive(t, result.claim.AlertEventID)
				case result.err == nil:
					replays++
					require.Zero(t, result.claim.AlertEventID)
				default:
					t.Fatalf("unexpected claim error: %v", result.err)
				}
				if result.claim.AlertEventID != 0 {
					require.NotContains(t, eventIDs, result.claim.AlertEventID)
					eventIDs[result.claim.AlertEventID] = true
				}
			}
			require.Equal(t, 1, applied)
			require.Equal(t, 7, replays)
			require.Equal(t, 8, rejected)
			alerts := bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)
			require.Len(t, alerts, 2)
			require.Len(t, eventIDs, 2)
			for _, alert := range alerts {
				require.Contains(t, eventIDs, alert.ID)
			}
			after, err := repo.GetWeek(ctx, owner.ID, monday)
			require.NoError(t, err)
			require.EqualValues(t, 11_000_000, after.SpentMicros)
			var balance decimal.Decimal
			var entries int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, owner.ID).Scan(&balance))
			require.True(t, decimal.NewFromInt(11).Equal(balance))
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM bloom_balance_entries AS entry JOIN bloom_tasks AS task ON task.id = entry.task_id
				WHERE task.user_id = $1
			`, owner.ID).Scan(&entries))
			require.Equal(t, 2, entries)
		})
	}
}

func TestBloomHardLimitIntegerBoundaries(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	cases := []struct {
		budget, reward int64
		rejected       bool
		reason         string
	}{
		{0, 0, false, ""},
		{0, 1, true, "exceeded"},
		{1, 1, false, "reached"},
		{9, 9, false, "reached"},
		{9, 10, true, "exceeded"},
		{10, 10, false, ""},
		{10, 11, false, "reached"},
		{10, 12, true, "exceeded"},
		{11, 12, false, "reached"},
		{11, 13, true, "exceeded"},
		{19, 20, false, "reached"},
		{19, 21, true, "exceeded"},
		{101, 111, false, "reached"},
		{101, 112, true, "exceeded"},
		{10, math.MaxInt64, true, "exceeded"},
		{math.MaxInt64, 1, false, ""},
	}
	for _, testCase := range cases {
		t.Run(strconv.FormatInt(testCase.budget, 10)+"_"+strconv.FormatInt(testCase.reward, 10), func(t *testing.T) {
			ctx := context.Background()
			repo := NewBloomRepository(integrationDB)
			owner := mustCreateUser(t, integrationEntClient, &service.User{})
			monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
			week, err := repo.EnsureTasks(ctx, owner.ID, monday, testCase.budget, []BloomTaskDraft{
				{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: testCase.reward},
			})
			require.NoError(t, err)
			claim, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, time.Now().Add(time.Hour))
			if testCase.rejected {
				require.ErrorIs(t, err, ErrBloomBudgetExceeded)
				require.False(t, claim.Applied)
			} else {
				require.NoError(t, err)
				require.True(t, claim.Applied)
				require.True(t, decimal.New(testCase.reward, -6).Equal(claim.BalanceAfter))
			}
			alerts := bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)
			if testCase.reason == "" {
				require.Empty(t, alerts)
				require.Zero(t, claim.AlertEventID)
			} else {
				require.Len(t, alerts, 1)
				require.Positive(t, claim.AlertEventID)
				require.Equal(t, claim.AlertEventID, alerts[testCase.reason].ID)
			}
			after, err := repo.GetWeek(ctx, owner.ID, monday)
			require.NoError(t, err)
			require.Equal(t, testCase.budget, after.BudgetMicros)
			if testCase.rejected {
				require.Equal(t, week, after)
			} else {
				require.Equal(t, testCase.reward, after.SpentMicros)
			}
		})
	}
}

func TestBloomHardLimitLargeAccumulationDoesNotOverflow(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, math.MaxInt64, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: math.MaxInt64},
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE bloom_week_budgets SET spent_micros = $2 WHERE user_id = $1
	`, owner.ID, int64(math.MaxInt64-1))
	require.NoError(t, err)
	claim, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, ErrBloomBudgetExceeded)
	require.Positive(t, claim.AlertEventID)
	alerts := bloomHardLimitAlerts(t, integrationDB, owner.ID, monday)
	require.Len(t, alerts, 1)
	require.Equal(t, claim.AlertEventID, alerts["exceeded"].ID)
	require.Equal(t, "18446744073709551613", alerts["exceeded"].Dimensions["attempted_micros"])
	require.Equal(t, "10145709240540253387", alerts["exceeded"].Dimensions["hard_limit_micros"])
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64-1, after.SpentMicros)
	require.Nil(t, after.Tasks[0].ClaimedAt)
}

func TestBloomHardLimitLedgerFailureRollsBackAlertAndCredit(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 1})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 11},
	})
	require.NoError(t, err)
	taskID := week.Tasks[0].ID
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO bloom_balance_entries (task_id, delta_micros, balance_after, created_at) VALUES ($1, 0, 1, NOW())
	`, taskID)
	require.NoError(t, err)
	claim, err := repo.ClaimTask(ctx, owner.ID, taskID, time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Equal(t, BloomClaim{}, claim)
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, week, after)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
	var balance decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, owner.ID).Scan(&balance))
	require.True(t, decimal.NewFromInt(1).Equal(balance))
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM bloom_balance_entries WHERE task_id = $1`, taskID)
	require.NoError(t, err)
	claim, err = repo.ClaimTask(ctx, owner.ID, taskID, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claim.Applied)
	require.Positive(t, claim.AlertEventID)
	require.Len(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday), 1)
}

func TestBloomHardLimitAlertFailureDoesNotCredit(t *testing.T) {
	integrationDB, integrationEntClient, integrationDSN := newBloomTestDatabase(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", integrationDSN)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `
		CREATE TEMP TABLE ops_alert_events (LIKE public.ops_alert_events INCLUDING ALL, CHECK (false))
	`)
	require.NoError(t, err)
	repo := NewBloomRepository(db)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 11},
		{PeriodStart: monday, Difficulty: BloomDailyMedium, Template: json.RawMessage(`{}`), RewardMicros: 12},
	})
	require.NoError(t, err)
	for _, task := range week.Tasks {
		claim, err := repo.ClaimTask(ctx, owner.ID, task.ID, time.Now().Add(time.Hour))
		require.ErrorContains(t, err, "persist bloom budget alert")
		require.Equal(t, BloomClaim{}, claim)
	}
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, week, after)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
}

func TestBloomHardLimitCommitFailureReturnsNoEventID(t *testing.T) {
	integrationDB, integrationEntClient, integrationDSN := newBloomTestDatabase(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", integrationDSN)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TEMP TABLE ops_alert_events (LIKE public.ops_alert_events INCLUDING ALL)`,
		`CREATE TEMP TABLE bloom_alert_rules (id BIGINT PRIMARY KEY)`,
		`ALTER TABLE ops_alert_events ALTER COLUMN rule_id SET DEFAULT -1`,
		`ALTER TABLE ops_alert_events ADD FOREIGN KEY (rule_id) REFERENCES bloom_alert_rules(id) DEFERRABLE INITIALLY DEFERRED`,
	} {
		_, err := db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	repo := NewBloomRepository(db)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 11},
		{PeriodStart: monday, Difficulty: BloomDailyMedium, Template: json.RawMessage(`{}`), RewardMicros: 12},
	})
	require.NoError(t, err)
	for _, task := range week.Tasks {
		claim, err := repo.ClaimTask(ctx, owner.ID, task.ID, time.Now().Add(time.Hour))
		require.ErrorContains(t, err, "foreign key")
		require.Equal(t, BloomClaim{}, claim)
	}
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, week, after)
	var balance decimal.Decimal
	var events int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, owner.ID).Scan(&balance))
	require.True(t, balance.IsZero())
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ops_alert_events`).Scan(&events))
	require.Zero(t, events)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
}

func TestBloomHardLimitExpiryRollsBackPendingAlert(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 10, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomDailyEasy, Template: json.RawMessage(`{}`), RewardMicros: 11},
	})
	require.NoError(t, err)
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	var blockerPID int
	var expires time.Time
	require.NoError(t, blocker.QueryRowContext(ctx, `
		SELECT pg_backend_pid(), clock_timestamp() + INTERVAL '2 seconds' FROM users WHERE id = $1 FOR UPDATE
	`, owner.ID).Scan(&blockerPID, &expires))
	type attempt struct {
		claim BloomClaim
		err   error
	}
	results := make(chan attempt, 1)
	go func() {
		claim, err := repo.ClaimTask(ctx, owner.ID, week.Tasks[0].ID, expires)
		results <- attempt{claim, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
	_, err = blocker.ExecContext(ctx, `SELECT pg_sleep_until($1::timestamptz)`, expires)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit())
	result := <-results
	require.ErrorIs(t, result.err, ErrBloomExpired)
	require.Equal(t, BloomClaim{}, result.claim)
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.Equal(t, week, after)
	require.Empty(t, bloomHardLimitAlerts(t, integrationDB, owner.ID, monday))
	var balance decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, owner.ID).Scan(&balance))
	require.True(t, balance.IsZero())
}

func TestBloomHardLimitDatabaseConstraint(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	repo := NewBloomRepository(integrationDB)
	_, err := repo.EnsureTasks(ctx, owner.ID, monday, 10, nil)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE bloom_week_budgets SET spent_micros = 11 WHERE user_id = $1`, owner.ID)
	require.NoError(t, err)
	for _, spent := range []int64{12, -1} {
		_, err = integrationDB.ExecContext(ctx, `UPDATE bloom_week_budgets SET spent_micros = $2 WHERE user_id = $1`, owner.ID, spent)
		require.Error(t, err)
	}
	after, err := repo.GetWeek(ctx, owner.ID, monday)
	require.NoError(t, err)
	require.EqualValues(t, 11, after.SpentMicros)
}

func TestBloomQueuedClaimExpiresAndZeroRewardCanBeCollected(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 1})
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	week, err := repo.EnsureTasks(ctx, owner.ID, monday, 0, []BloomTaskDraft{
		{PeriodStart: monday, Difficulty: BloomWeekly, Template: json.RawMessage(`{"goal":1}`)},
	})
	require.NoError(t, err)
	taskID := week.Tasks[0].ID
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	var blockerPID int
	var expires time.Time
	require.NoError(t, blocker.QueryRowContext(ctx, `
		SELECT pg_backend_pid(), clock_timestamp() + INTERVAL '2 seconds' FROM bloom_tasks WHERE id = $1 FOR UPDATE
	`, taskID).Scan(&blockerPID, &expires))

	results := make(chan error, 1)
	go func() {
		_, err := repo.ClaimTask(ctx, owner.ID, taskID, expires)
		results <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
	_, err = blocker.ExecContext(ctx, `SELECT pg_sleep_until($1::timestamptz)`, expires)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit())
	require.ErrorIs(t, <-results, ErrBloomExpired)

	claim, err := repo.ClaimTask(ctx, owner.ID, taskID, expires.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claim.Applied)
	require.Zero(t, claim.RewardMicros)
	require.True(t, decimal.NewFromInt(1).Equal(claim.BalanceAfter))
	count, err := repo.CountClaimedWeekly(ctx, owner.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestBloomUsageStatsCalendarAndOutput(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := context.Background()
	repo := NewBloomRepository(integrationDB)
	owner := mustCreateUser(t, integrationEntClient, &service.User{})
	other := mustCreateUser(t, integrationEntClient, &service.User{})
	key := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: owner.ID})
	otherKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: owner.ID})
	upstream := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "bloom-usage"})
	zone, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, zone)
	end := start.AddDate(0, 0, 2)
	insert := func(userID, keyID int64, requestID any, at time.Time, output, images, videos, billing int, actual string) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `
			INSERT INTO usage_logs (user_id, api_key_id, account_id, request_id, model, created_at,
			    output_tokens, image_count, video_count, billing_type, actual_cost, total_cost, image_size)
			VALUES ($1, $2, $3, $4, 'bloom', $5, $6, $7, $8, $9, $10, 99, '1K')
		`, userID, keyID, upstream.ID, requestID, at, output, images, videos, billing, actual)
		require.NoError(t, err)
	}
	insert(owner.ID, key.ID, "outside-left", start.Add(-time.Second), 1, 0, 0, 0, "5")
	insert(owner.ID, key.ID, "shared", start, 7, 0, 0, 0, "0.1000000001")
	insert(owner.ID, otherKey.ID, "shared", start.Add(time.Hour), 0, 2, 0, 1, "3")
	insert(owner.ID, key.ID, "archive", start.Add(time.Hour), 0, 0, 0, 0, "0.2")
	insert(owner.ID, key.ID, "log-only", start.Add(time.Hour), 0, 0, 0, 0, "4")
	insert(owner.ID, key.ID, "zero-amount", start.Add(time.Hour), 0, 0, 0, 0, "0")
	insert(owner.ID, key.ID, nil, start.AddDate(0, 0, 1), 0, 1, 1, 0, "0.2")
	insert(owner.ID, key.ID, nil, start.AddDate(0, 0, 1), 0, 0, 0, 0, "0")
	insert(owner.ID, key.ID, "outside-right", end, 1, 0, 0, 0, "5")
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, wallet_charged_amount)
		VALUES ('shared', $1, 'test', 0.11), ('shared', $2, 'test', 0),
		       ('log-only', $2, 'test', 4), ('zero-amount', $1, 'test', 0)
	`, key.ID, otherKey.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at, wallet_charged_amount)
		VALUES ('archive', $1, 'test', NOW(), 0.2), ('shared', $1, 'test', NOW(), 0.11),
		       ('log-only', $1, 'test', NOW(), NULL)
	`, key.ID)
	require.NoError(t, err)

	stats, err := repo.UsageStats(ctx, owner.ID, start, end, zone.String())
	require.NoError(t, err)
	require.EqualValues(t, 7, stats.TotalRecords)
	require.True(t, decimal.RequireFromString("0.31").Equal(stats.WalletCost))
	require.EqualValues(t, 2, stats.OutputRequests)
	require.EqualValues(t, 7, stats.OutputTokens)
	require.EqualValues(t, 2, stats.ImageCount)
	require.EqualValues(t, 1, stats.ActiveDays)
	stats, err = repo.UsageStats(ctx, other.ID, start, end, zone.String())
	require.NoError(t, err)
	require.Zero(t, stats.TotalRecords)
}

func TestBloomWalletEvidenceSingleBatchAndArchive(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := t.Context()
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 1})
	key := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: owner.ID})
	upstream := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "bloom-wallet"})
	billing := NewUsageBillingRepository(nil, integrationDB)
	bloom := NewBloomRepository(integrationDB)
	now := time.Now().UTC()
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	single := &service.UsageBillingCommand{UserID: owner.ID, APIKeyID: key.ID, RequestID: "single", BalanceCost: 0.123456785}
	batch := &service.UsageBillingCommand{UserID: owner.ID, APIKeyID: key.ID, RequestID: "overdraft", BalanceCost: 1.234567895}
	rateOnly := &service.UsageBillingCommand{UserID: owner.ID, APIKeyID: key.ID, RequestID: "rate-only", APIKeyRateLimitCost: 2}
	result, err := billing.Apply(ctx, single)
	require.NoError(t, err)
	require.True(t, result.Applied)
	outcomes, err := billing.ApplyBatch(ctx, []*service.UsageBillingCommand{batch, rateOnly})
	require.NoError(t, err)
	require.True(t, outcomes[0].Result.BalanceOverdrafted)
	require.True(t, outcomes[1].Result.Applied)

	failure := &service.UsageBillingCommand{UserID: owner.ID, APIKeyID: key.ID, RequestID: "rolled-back", BalanceCost: 1,
		AutoExecutionID: 1234567, ActualGroupID: &key.ID}
	_, err = billing.Apply(ctx, failure)
	require.Error(t, err)
	var remaining decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, owner.ID).Scan(&remaining))
	require.Equal(t, "-0.35802469", remaining.String())

	assertCharge := func(table, request, expected string) {
		t.Helper()
		var amount decimal.Decimal
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT wallet_charged_amount FROM "+table+
			" WHERE api_key_id = $1 AND request_id = $2", key.ID, request).Scan(&amount))
		require.Equal(t, expected, amount.String())
	}
	assertCharge("usage_billing_dedup", single.RequestID, "0.12345679")
	assertCharge("usage_billing_dedup", batch.RequestID, "1.2345679")
	assertCharge("usage_billing_dedup", rateOnly.RequestID, "0")
	var invalidCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup
		WHERE api_key_id = $1 AND request_id = $2`, key.ID, failure.RequestID).Scan(&invalidCount))
	require.Zero(t, invalidCount)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, request_id, model, created_at, actual_cost)
		SELECT $1, $2, $3, id, 'bloom', $4, 99
		FROM unnest(ARRAY['single', 'overdraft', 'rate-only', 'rolled-back']) AS requests(id)
	`, owner.ID, key.ID, upstream.ID, now)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at)
		SELECT request_id, api_key_id, request_fingerprint, created_at
		FROM usage_billing_dedup WHERE api_key_id = $1 AND request_id = $2
	`, key.ID, batch.RequestID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_dedup SET created_at = $2
		WHERE api_key_id = $1`, key.ID, now.AddDate(-5, 0, 0))
	require.NoError(t, err)
	require.NoError(t, NewDashboardAggregationRepository(integrationDB).CleanupUsageBillingDedup(ctx, now.AddDate(-4, 0, 0)))
	assertCharge("usage_billing_dedup_archive", single.RequestID, "0.12345679")
	assertCharge("usage_billing_dedup_archive", batch.RequestID, "1.2345679")
	assertCharge("usage_billing_dedup_archive", rateOnly.RequestID, "0")
	result, err = billing.Apply(ctx, single)
	require.NoError(t, err)
	require.False(t, result.Applied)
	stats, err := bloom.UsageStats(ctx, owner.ID, start, end, "UTC")
	require.NoError(t, err)
	require.EqualValues(t, 4, stats.TotalRecords)
	require.Equal(t, "1.35802469", stats.WalletCost.String())
}

func TestBloomWalletEvidenceImageHolds(t *testing.T) {
	integrationDB, integrationEntClient, _ := newBloomTestDatabase(t)
	ctx := t.Context()
	owner := mustCreateUser(t, integrationEntClient, &service.User{Balance: 10})
	key := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: owner.ID})
	upstream := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "bloom-holds"})
	billing := NewUsageBillingRepository(nil, integrationDB)
	playground := billing.(service.PlaygroundImageBillingRepository)
	ownerSuffix := strconv.FormatInt(owner.ID, 10)
	batchID := "batch-bloom-" + ownerSuffix
	playgroundID := "playground-image:bloom-" + ownerSuffix
	now := time.Now().UTC()
	makeHold := func(batchID string) *service.BatchImageBalanceHoldCommand {
		return &service.BatchImageBalanceHoldCommand{BatchID: batchID, RequestID: service.BatchImageHoldRequestID(batchID),
			UserID: owner.ID, APIKeyID: key.ID, HoldAmount: 2, RequestedCount: 1, UnitAmount: 2}
	}
	for _, hold := range []*service.BatchImageBalanceHoldCommand{makeHold(batchID), makeHold(playgroundID)} {
		result, err := billing.ReserveBatchImageBalance(ctx, hold)
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	batchCapture := makeHold(batchID)
	batchCapture.RequestID, batchCapture.ActualAmount = service.BatchImageCaptureRequestID(batchID), 0.125000005
	captured, err := billing.CaptureBatchImageBalance(ctx, batchCapture)
	require.NoError(t, err)
	require.True(t, captured.Applied)
	var batchCharge decimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT wallet_charged_amount FROM usage_billing_dedup
		WHERE api_key_id = $1 AND request_id = $2`, key.ID, batchCapture.RequestID).Scan(&batchCharge))
	require.Equal(t, "0.125", batchCharge.String())
	playgroundCapture := makeHold(playgroundID)
	playgroundCapture.RequestID, playgroundCapture.ActualAmount = service.PlaygroundImageTerminalRequestID(playgroundID), 0.25
	usage := &service.UsageBillingCommand{UserID: owner.ID, APIKeyID: key.ID, RequestID: "playground-usage"}
	for _, expected := range []bool{true, false} {
		result, err := playground.SettlePlaygroundImageBalance(ctx, playgroundCapture, usage)
		require.NoError(t, err)
		require.Equal(t, expected, result.Applied)
	}
	historicalBatchID := "old-" + batchID
	historicalRequest := service.BatchImageCaptureRequestID(historicalBatchID)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO batch_image_jobs (batch_id, user_id, api_key_id, provider, model, status, item_count, actual_cost, settled_at)
		VALUES ($1, $2, $3, 'vertex', 'bloom', 'completed', 1, 0.35, $4)
	`, historicalBatchID, owner.ID, key.ID, now)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, 'legacy')
	`, historicalRequest, key.ID)
	require.NoError(t, err)
	for _, request := range []string{batchCapture.RequestID, usage.RequestID, historicalRequest} {
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO usage_logs (user_id, api_key_id, account_id, request_id, model, created_at, actual_cost)
			VALUES ($1, $2, $3, $4, 'bloom', $5, 99)
		`, owner.ID, key.ID, upstream.ID, request, now)
		require.NoError(t, err)
	}
	stats, err := NewBloomRepository(integrationDB).UsageStats(ctx, owner.ID, now.Add(-time.Minute), now.Add(time.Minute), "UTC")
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.TotalRecords)
	require.Equal(t, "0.725", stats.WalletCost.String())
}
