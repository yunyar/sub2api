package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

const (
	BloomDailyEasy   = service.BloomDailyEasy
	BloomDailyMedium = service.BloomDailyMedium
	BloomDailyHard   = service.BloomDailyHard
	BloomWeekly      = service.BloomWeekly
)

var (
	ErrBloomInvalidInput   = service.ErrBloomInvalidInput
	ErrBloomUserNotFound   = service.ErrBloomUserNotFound
	ErrBloomTaskNotFound   = service.ErrBloomTaskNotFound
	ErrBloomExpired        = service.ErrBloomExpired
	ErrBloomBudgetExceeded = service.ErrBloomBudgetExceeded
	ErrBloomBudgetChanged  = service.ErrBloomBudgetChanged
)

type (
	BloomDifficulty = service.BloomDifficulty
	BloomTaskDraft  = service.BloomTaskDraft
	BloomTask       = service.BloomTask
	BloomWeek       = service.BloomWeek
	BloomClaim      = service.BloomClaim
	BloomUsageStats = service.BloomUsageStats
)

type bloomRepository struct {
	db *sql.DB
}

func NewBloomRepository(db *sql.DB) service.BloomRepository {
	return &bloomRepository{db: db}
}

func bloomWeekDate(weekStart time.Time) (string, error) {
	if weekStart.IsZero() || weekStart.Year() < 1 || weekStart.Year() > 9999 || weekStart.Weekday() != time.Monday {
		return "", ErrBloomInvalidInput
	}
	return weekStart.Format(time.DateOnly), nil
}

func (r *bloomRepository) GetWeek(ctx context.Context, userID int64, weekStart time.Time) (BloomWeek, error) {
	weekDate, err := bloomWeekDate(weekStart)
	if userID <= 0 || err != nil {
		return BloomWeek{}, ErrBloomInvalidInput
	}
	return getBloomWeek(ctx, r.db, userID, weekDate, weekStart.Location())
}

type bloomRowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func getBloomWeek(ctx context.Context, db bloomRowsQuerier, userID int64, weekDate string, zone *time.Location) (BloomWeek, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT budget.budget_micros, budget.spent_micros,
		       task.id, task.period_start, task.difficulty, task.template, task.reward_micros, task.claimed_at
		FROM users AS account
		LEFT JOIN bloom_week_budgets AS budget ON budget.user_id = account.id AND budget.week_start = $2::date
		LEFT JOIN bloom_tasks AS task ON task.user_id = budget.user_id AND task.week_start = budget.week_start
		WHERE account.id = $1 AND account.deleted_at IS NULL AND account.status = 'active'
		ORDER BY task.period_start, CASE task.difficulty WHEN 'easy' THEN 1 WHEN 'medium' THEN 2 WHEN 'hard' THEN 3 ELSE 4 END
	`, userID, weekDate)
	if err != nil {
		return BloomWeek{}, err
	}
	defer func() { _ = rows.Close() }()

	result := BloomWeek{Tasks: []BloomTask{}}
	foundUser := false
	for rows.Next() {
		foundUser = true
		var budget, spent, id, reward sql.NullInt64
		var period, claimed sql.NullTime
		var difficulty sql.NullString
		var template []byte
		if err := rows.Scan(&budget, &spent, &id, &period, &difficulty, &template, &reward, &claimed); err != nil {
			return BloomWeek{}, err
		}
		result.Exists, result.BudgetMicros, result.SpentMicros = budget.Valid, budget.Int64, spent.Int64
		if id.Valid {
			year, month, day := period.Time.Date()
			task := BloomTask{
				ID: id.Int64,
				BloomTaskDraft: BloomTaskDraft{
					PeriodStart:  time.Date(year, month, day, 0, 0, 0, 0, zone),
					Difficulty:   BloomDifficulty(difficulty.String),
					Template:     template,
					RewardMicros: reward.Int64,
				},
			}
			if claimed.Valid {
				task.ClaimedAt = &claimed.Time
			}
			result.Tasks = append(result.Tasks, task)
		}
	}
	if err := rows.Err(); err != nil {
		return BloomWeek{}, err
	}
	if !foundUser {
		return BloomWeek{}, ErrBloomUserNotFound
	}
	return result, nil
}

func (r *bloomRepository) EnsureTasks(ctx context.Context, userID int64, weekStart time.Time, budgetMicros int64, drafts []BloomTaskDraft) (BloomWeek, error) {
	weekDate, err := bloomWeekDate(weekStart)
	if userID <= 0 || err != nil || budgetMicros < 0 {
		return BloomWeek{}, ErrBloomInvalidInput
	}
	weekEnd := weekStart.AddDate(0, 0, 7).Format(time.DateOnly)
	for _, draft := range drafts {
		periodDate := draft.PeriodStart.Format(time.DateOnly)
		if draft.RewardMicros < 0 || !json.Valid(draft.Template) || periodDate < weekDate || periodDate >= weekEnd {
			return BloomWeek{}, ErrBloomInvalidInput
		}
		switch draft.Difficulty {
		case BloomDailyEasy, BloomDailyMedium, BloomDailyHard:
		case BloomWeekly:
			if periodDate != weekDate {
				return BloomWeek{}, ErrBloomInvalidInput
			}
		default:
			return BloomWeek{}, ErrBloomInvalidInput
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return BloomWeek{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var active bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND status = 'active')
	`, userID).Scan(&active)
	if err != nil {
		return BloomWeek{}, err
	}
	if !active {
		return BloomWeek{}, ErrBloomUserNotFound
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO bloom_week_budgets (user_id, week_start, budget_micros)
		VALUES ($1, $2::date, $3) ON CONFLICT (user_id, week_start) DO NOTHING
	`, userID, weekDate, budgetMicros)
	if err != nil {
		return BloomWeek{}, err
	}
	if len(drafts) > 0 {
		var storedBudget int64
		err := tx.QueryRowContext(ctx, `
			SELECT budget_micros FROM bloom_week_budgets
			WHERE user_id = $1 AND week_start = $2::date FOR KEY SHARE
		`, userID, weekDate).Scan(&storedBudget)
		if err != nil {
			return BloomWeek{}, err
		}
		if storedBudget != budgetMicros {
			result, err := getBloomWeek(ctx, tx, userID, weekDate, weekStart.Location())
			if err != nil {
				return BloomWeek{}, err
			}
			return result, ErrBloomBudgetChanged
		}
		type taskRow struct {
			PeriodStart  string          `json:"period_start"`
			Difficulty   BloomDifficulty `json:"difficulty"`
			Template     json.RawMessage `json:"template"`
			RewardMicros int64           `json:"reward_micros"`
		}
		tasks := make([]taskRow, 0, len(drafts))
		for _, draft := range drafts {
			tasks = append(tasks, taskRow{draft.PeriodStart.Format(time.DateOnly), draft.Difficulty, draft.Template, draft.RewardMicros})
		}
		payload, err := json.Marshal(tasks)
		if err != nil {
			return BloomWeek{}, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO bloom_tasks (user_id, week_start, period_start, difficulty, template, reward_micros)
			SELECT $1, $2::date, draft.period_start, draft.difficulty, (entry -> 'template'), draft.reward_micros
			FROM jsonb_array_elements($3::jsonb) AS input(entry)
			CROSS JOIN LATERAL jsonb_to_record(input.entry) AS draft(period_start date, difficulty text, reward_micros bigint)
			ORDER BY draft.period_start, draft.difficulty
			ON CONFLICT (user_id, period_start, difficulty) DO NOTHING
		`, userID, weekDate, string(payload))
		if err != nil {
			return BloomWeek{}, err
		}
	}
	result, err := getBloomWeek(ctx, tx, userID, weekDate, weekStart.Location())
	if err != nil {
		return BloomWeek{}, err
	}
	if err := tx.Commit(); err != nil {
		return BloomWeek{}, err
	}
	return result, nil
}

type bloomBudgetAlert struct {
	userID, taskID, budget, spent, reward int64
	weekDate, reason                      string
	attempted, limit                      decimal.Decimal
}

func insertBloomBudgetAlert(ctx context.Context, tx *sql.Tx, alert *bloomBudgetAlert) (int64, error) {
	dimensions, err := json.Marshal(map[string]string{
		"source":            "bloom_weekly_hard_limit",
		"user_id":           strconv.FormatInt(alert.userID, 10),
		"task_id":           strconv.FormatInt(alert.taskID, 10),
		"week_start":        alert.weekDate,
		"reason":            alert.reason,
		"budget_micros":     strconv.FormatInt(alert.budget, 10),
		"spent_micros":      strconv.FormatInt(alert.spent, 10),
		"reward_micros":     strconv.FormatInt(alert.reward, 10),
		"attempted_micros":  alert.attempted.String(),
		"hard_limit_micros": alert.limit.String(),
	})
	if err != nil {
		return 0, err
	}
	state, settlement := "达到", "本次奖励与告警在同一事务结算"
	if alert.reason == "exceeded" {
		state, settlement = "超出", "本次奖励已整笔拒绝，钱包、周累计、任务及流水均未变更"
	}
	var eventID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ops_alert_events (severity, status, title, description, metric_value, threshold_value, dimensions)
		VALUES ('P0', 'firing', $1, $2, $3::numeric / 1000000, $4::numeric / 1000000, $5::jsonb)
		ON CONFLICT ((dimensions ->> 'user_id'), (dimensions ->> 'week_start'), (dimensions ->> 'reason'))
		WHERE dimensions ->> 'source' = 'bloom_weekly_hard_limit'
		DO NOTHING RETURNING id
	`, fmt.Sprintf("花园奖励领取%s周预算110%%硬上限", state), fmt.Sprintf(
		"用户%d在%s周的奖励领取%s110%%硬上限。原预算%d微单位，已领取%d微单位，本次奖励%d微单位，预计累计%s微单位，硬上限%s微单位。%s。",
		alert.userID, alert.weekDate, state, alert.budget, alert.spent, alert.reward, alert.attempted, alert.limit, settlement,
	), alert.attempted.String(), alert.limit.String(), string(dimensions)).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("persist bloom budget alert: %w", err)
	}
	return eventID, nil
}

func (r *bloomRepository) ClaimTask(ctx context.Context, userID, taskID int64, expiresAt time.Time) (BloomClaim, error) {
	if userID <= 0 || taskID <= 0 {
		return BloomClaim{}, ErrBloomInvalidInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return BloomClaim{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result BloomClaim
	var weekDate string
	var claimed sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT to_char(task.week_start, 'YYYY-MM-DD'), task.reward_micros, task.claimed_at
		FROM bloom_tasks AS task
		JOIN users AS account ON account.id = task.user_id
		WHERE task.id = $1 AND task.user_id = $2 AND account.deleted_at IS NULL AND account.status = 'active'
		FOR UPDATE OF task
	`, taskID, userID).Scan(&weekDate, &result.RewardMicros, &claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return BloomClaim{}, ErrBloomTaskNotFound
	}
	if err != nil {
		return BloomClaim{}, err
	}
	if claimed.Valid {
		result.ClaimedAt = claimed.Time
		if err := tx.QueryRowContext(ctx, `
			SELECT balance_after FROM bloom_balance_entries WHERE task_id = $1
		`, taskID).Scan(&result.BalanceAfter); err != nil {
			return BloomClaim{}, err
		}
		return result, nil
	}
	var budgetMicros, spentMicros int64
	err = tx.QueryRowContext(ctx, `
		SELECT budget_micros, spent_micros FROM bloom_week_budgets
		WHERE user_id = $1 AND week_start = $2::date FOR UPDATE
	`, userID, weekDate).Scan(&budgetMicros, &spentMicros)
	if err != nil {
		return BloomClaim{}, err
	}
	var beforeExpiry bool
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp() < $1::timestamptz`, expiresAt).Scan(&beforeExpiry); err != nil {
		return BloomClaim{}, err
	}
	if !beforeExpiry {
		return BloomClaim{}, ErrBloomExpired
	}
	limit := decimal.NewFromInt(budgetMicros).Add(decimal.NewFromInt(budgetMicros / 10))
	attempted := decimal.NewFromInt(spentMicros).Add(decimal.NewFromInt(result.RewardMicros))
	reason := "reached"
	if attempted.GreaterThan(limit) {
		reason = "exceeded"
	}
	var alertEventID int64
	if result.RewardMicros > 0 && attempted.GreaterThanOrEqual(limit) {
		alertEventID, err = insertBloomBudgetAlert(ctx, tx, &bloomBudgetAlert{
			userID: userID, taskID: taskID, weekDate: weekDate, reason: reason,
			budget: budgetMicros, spent: spentMicros, reward: result.RewardMicros,
			attempted: attempted, limit: limit,
		})
		if err != nil {
			return BloomClaim{}, err
		}
	}
	if attempted.GreaterThan(limit) {
		if err := tx.Commit(); err != nil {
			return BloomClaim{}, err
		}
		return BloomClaim{AlertEventID: alertEventID}, ErrBloomBudgetExceeded
	}
	budget, err := tx.ExecContext(ctx, `
		UPDATE bloom_week_budgets SET spent_micros = spent_micros + $3
		WHERE user_id = $1 AND week_start = $2::date
		  AND $3::numeric <= budget_micros::numeric + budget_micros / 10 - spent_micros
	`, userID, weekDate, result.RewardMicros)
	if err != nil {
		return BloomClaim{}, err
	}
	count, err := budget.RowsAffected()
	if err != nil {
		return BloomClaim{}, err
	}
	if count == 0 {
		return BloomClaim{}, ErrBloomBudgetExceeded
	}
	err = tx.QueryRowContext(ctx, `
		UPDATE users SET balance = balance + $2::numeric * 0.000001,
		    updated_at = CASE WHEN $2::numeric > 0 THEN NOW() ELSE updated_at END
		WHERE id = $1 AND deleted_at IS NULL AND status = 'active' RETURNING balance
	`, userID, result.RewardMicros).Scan(&result.BalanceAfter)
	if errors.Is(err, sql.ErrNoRows) {
		return BloomClaim{}, ErrBloomUserNotFound
	}
	if err != nil {
		return BloomClaim{}, err
	}
	err = tx.QueryRowContext(ctx, `
		WITH timing AS (SELECT clock_timestamp() AS claimed_at),
		claimed AS (
			UPDATE bloom_tasks AS task SET claimed_at = timing.claimed_at
			FROM timing WHERE task.id = $1 AND timing.claimed_at < $4::timestamptz
			RETURNING task.id, task.claimed_at
		)
		INSERT INTO bloom_balance_entries (task_id, delta_micros, balance_after, created_at)
		SELECT id, $2, $3, claimed_at FROM claimed RETURNING created_at
	`, taskID, result.RewardMicros, result.BalanceAfter, expiresAt).Scan(&result.ClaimedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return BloomClaim{}, ErrBloomExpired
	}
	if err != nil {
		return BloomClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return BloomClaim{}, err
	}
	result.Applied = true
	result.AlertEventID = alertEventID
	return result, nil
}

func (r *bloomRepository) CountClaimedWeekly(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, ErrBloomInvalidInput
	}
	var count int64
	err := r.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM bloom_tasks WHERE user_id = account.id AND difficulty = 'weekly' AND claimed_at IS NOT NULL)
		FROM users AS account
		WHERE account.id = $1 AND account.deleted_at IS NULL AND account.status = 'active'
	`, userID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBloomUserNotFound
	}
	return count, err
}

func (r *bloomRepository) UsageStats(ctx context.Context, userID int64, start, end time.Time, zone string) (BloomUsageStats, error) {
	if userID <= 0 || !start.Before(end) || zone == "" || zone == "Local" {
		return BloomUsageStats{}, ErrBloomInvalidInput
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return BloomUsageStats{}, ErrBloomInvalidInput
	}

	var stats BloomUsageStats
	err := r.db.QueryRowContext(ctx, `
		WITH usage AS (
			SELECT DISTINCT ON (api_key_id, NULLIF(request_id, ''), CASE WHEN NULLIF(request_id, '') IS NULL THEN id END)
			       api_key_id, NULLIF(request_id, '') AS request_id, created_at, billing_type,
			       GREATEST(output_tokens, 0) AS tokens,
			       CASE WHEN COALESCE(video_count, 0) > 0 OR billing_mode = 'video'
			            THEN 0 ELSE GREATEST(image_count, 0) END AS images
			FROM usage_logs
			WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
			ORDER BY api_key_id, NULLIF(request_id, ''), CASE WHEN NULLIF(request_id, '') IS NULL THEN id END, created_at, id
		), playground AS (
			SELECT COALESCE(SUM(actual_amount), 0) AS charged
			FROM playground_image_balance_holds
			WHERE user_id = $1 AND status = 'captured' AND settled_at >= $2 AND settled_at < $3
		)
		SELECT stats.*
		FROM users AS account
		CROSS JOIN (
			SELECT COUNT(*),
			       (SELECT charged FROM playground) +
			       COALESCE(SUM(COALESCE(hot.wallet_charged_amount, archived.wallet_charged_amount, ROUND(batch.actual_cost, 8)))
			           FILTER (WHERE usage.billing_type = 0), 0),
			       COUNT(*) FILTER (WHERE tokens > 0 OR images > 0),
			       COALESCE(SUM(tokens), 0), COALESCE(SUM(images), 0),
			       COUNT(DISTINCT (usage.created_at AT TIME ZONE $4::text)::date) FILTER (WHERE tokens > 0 OR images > 0)
			FROM usage
			LEFT JOIN usage_billing_dedup AS hot USING (request_id, api_key_id)
			LEFT JOIN usage_billing_dedup_archive AS archived USING (request_id, api_key_id)
			LEFT JOIN batch_image_jobs AS batch
			       ON (hot.id IS NOT NULL OR archived.request_id IS NOT NULL)
			      AND hot.wallet_charged_amount IS NULL AND archived.wallet_charged_amount IS NULL
			      AND batch.batch_id = CASE WHEN starts_with(usage.request_id, 'batch_image_capture:')
			                                THEN substr(usage.request_id, length('batch_image_capture:') + 1) END
			      AND batch.user_id = $1 AND batch.api_key_id = usage.api_key_id
			      AND batch.status = 'completed' AND batch.settled_at IS NOT NULL AND batch.actual_cost > 0
		) AS stats
		WHERE account.id = $1 AND account.deleted_at IS NULL AND account.status = 'active'
	`, userID, start, end, zone).Scan(
		&stats.TotalRecords, &stats.WalletCost, &stats.OutputRequests,
		&stats.OutputTokens, &stats.ImageCount, &stats.ActiveDays,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BloomUsageStats{}, ErrBloomUserNotFound
	}
	return stats, err
}
