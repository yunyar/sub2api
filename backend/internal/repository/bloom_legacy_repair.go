package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/bloomrules"
	"github.com/shopspring/decimal"
)

func (r *bloomRepository) LegacyWalletSpend(ctx context.Context, userID int64, start, end time.Time) (decimal.Decimal, error) {
	if userID <= 0 || !start.Before(end) {
		return decimal.Zero, ErrBloomInvalidInput
	}
	var spend decimal.Decimal
	err := r.db.QueryRowContext(ctx, `
		WITH cutover AS (
			SELECT applied_at FROM schema_migrations WHERE filename = '254_usage_billing_wallet_charged_amount.sql'
			  AND applied_at > $2
		), legacy AS (
			SELECT DISTINCT ON (log.api_key_id, log.request_id) log.actual_cost
			FROM cutover
			JOIN usage_logs AS log ON log.user_id = $1 AND log.created_at >= $2
			    AND log.created_at < LEAST($3::timestamptz, cutover.applied_at)
			JOIN api_keys AS key ON key.id = log.api_key_id AND key.user_id = log.user_id
			LEFT JOIN usage_billing_dedup AS hot USING (api_key_id, request_id)
			LEFT JOIN usage_billing_dedup_archive AS archived USING (api_key_id, request_id)
			WHERE log.billing_type = 0 AND log.subscription_id IS NULL AND log.billing_mode = 'token'
			  AND log.image_count = 0 AND COALESCE(log.video_count, 0) = 0 AND log.actual_cost > 0
			  AND log.request_id <> ''
			  AND GREATEST(key.rate_limit_5h, key.rate_limit_1d, key.rate_limit_7d,
			               key.usage_5h, key.usage_1d, key.usage_7d) = 0
			  AND LEAST(hot.created_at, archived.created_at) < cutover.applied_at
			  AND COALESCE(hot.wallet_charged_amount, archived.wallet_charged_amount) IS NULL
			ORDER BY log.api_key_id, log.request_id, log.created_at, log.id
		)
		SELECT COALESCE(SUM(ROUND(actual_cost, 8)), 0) FROM legacy
	`, userID, start, end).Scan(&spend)
	return spend, err
}

func (r *bloomRepository) ReconcileLegacyWeek(ctx context.Context, userID int64, weekStart time.Time, budgetMicros int64) (BloomWeek, bool, error) {
	weekDate, err := bloomWeekDate(weekStart)
	if userID <= 0 || err != nil || budgetMicros < 0 {
		return BloomWeek{}, false, ErrBloomInvalidInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return BloomWeek{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		SELECT task.id
		FROM bloom_tasks AS task JOIN bloom_week_budgets AS budget USING (user_id, week_start)
		WHERE task.user_id = $1 AND task.week_start = $2::date AND budget.legacy_repair_pending
		ORDER BY task.id FOR UPDATE OF task
	`, userID, weekDate); err != nil {
		return BloomWeek{}, false, err
	}
	updated, err := tx.ExecContext(ctx, `
		UPDATE bloom_week_budgets SET budget_micros = $3, legacy_repair_pending = FALSE
		WHERE user_id = $1 AND week_start = $2::date AND legacy_repair_pending
		  AND budget_micros = 0 AND spent_micros = 0
		  AND NOT EXISTS (SELECT 1 FROM bloom_tasks WHERE user_id = $1 AND week_start = $2::date AND reward_micros > 0)
	`, userID, weekDate, budgetMicros)
	if err != nil {
		return BloomWeek{}, false, err
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return BloomWeek{}, false, err
	}
	var credited bool
	if count > 0 && budgetMicros > 0 {
		credited, err = reconcileBloomLegacyRewards(ctx, tx, userID, weekDate, budgetMicros)
		if err != nil {
			return BloomWeek{}, false, err
		}
	}
	week, err := getBloomWeek(ctx, tx, userID, weekDate, weekStart.Location())
	if err != nil {
		return BloomWeek{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return BloomWeek{}, false, err
	}
	return week, credited, nil
}

func reconcileBloomLegacyRewards(ctx context.Context, tx *sql.Tx, userID int64, weekDate string, budget int64) (bool, error) {
	var creditMicros int64
	err := tx.QueryRowContext(ctx, `
		WITH rewards(difficulty, amount) AS (VALUES
			('easy', $3::bigint), ('medium', $4::bigint), ('hard', $5::bigint), ('weekly', $6::bigint)
		), repaired AS (
			UPDATE bloom_tasks AS task
			SET reward_micros = rewards.amount,
			    template = jsonb_set(task.template, '{reward_micros}', to_jsonb(rewards.amount))
			FROM rewards
			WHERE task.user_id = $1 AND task.week_start = $2::date AND task.reward_micros = 0
			  AND task.difficulty = rewards.difficulty
			RETURNING task.id, task.reward_micros, task.claimed_at
		)
		SELECT COALESCE(SUM(repaired.reward_micros), 0)::bigint
		FROM repaired
		JOIN bloom_balance_entries AS entry ON entry.task_id = repaired.id AND entry.delta_micros = 0
		WHERE repaired.claimed_at IS NOT NULL
	`, userID, weekDate, bloomrules.RewardMicros(bloomrules.Easy, budget),
		bloomrules.RewardMicros(bloomrules.Medium, budget), bloomrules.RewardMicros(bloomrules.Hard, budget),
		bloomrules.RewardMicros(bloomrules.Weekly, budget)).Scan(&creditMicros)
	if err != nil || creditMicros == 0 {
		return false, err
	}
	if creditMicros > budget {
		return false, ErrBloomBudgetExceeded
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE bloom_week_budgets SET spent_micros = $3 WHERE user_id = $1 AND week_start = $2::date
	`, userID, weekDate, creditMicros); err != nil {
		return false, err
	}
	var balance decimal.Decimal
	err = tx.QueryRowContext(ctx, `
		UPDATE users SET balance = balance + $2::numeric * 0.000001, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL AND status = 'active' RETURNING balance
	`, userID, creditMicros).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrBloomUserNotFound
	}
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE bloom_balance_entries AS entry
		SET delta_micros = task.reward_micros, balance_after = $3::numeric, created_at = NOW()
		FROM bloom_tasks AS task
		WHERE task.user_id = $1 AND task.week_start = $2::date AND task.claimed_at IS NOT NULL
		  AND entry.task_id = task.id AND entry.delta_micros = 0 AND task.reward_micros > 0
	`, userID, weekDate, balance.String())
	return err == nil, err
}
