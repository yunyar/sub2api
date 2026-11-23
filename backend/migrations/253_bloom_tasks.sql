CREATE TABLE bloom_week_budgets (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL CHECK (EXTRACT(ISODOW FROM week_start) = 1),
    budget_micros BIGINT NOT NULL CHECK (budget_micros >= 0),
    spent_micros BIGINT NOT NULL DEFAULT 0
        CHECK (spent_micros >= 0 AND spent_micros::numeric <= budget_micros::numeric + budget_micros / 10),
    PRIMARY KEY (user_id, week_start)
);

CREATE TABLE bloom_tasks (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    week_start DATE NOT NULL,
    period_start DATE NOT NULL,
    difficulty TEXT NOT NULL,
    template JSONB NOT NULL,
    reward_micros BIGINT NOT NULL CHECK (reward_micros >= 0),
    claimed_at TIMESTAMPTZ,
    UNIQUE (user_id, period_start, difficulty),
    FOREIGN KEY (user_id, week_start) REFERENCES bloom_week_budgets(user_id, week_start) ON DELETE CASCADE,
    CHECK (
        (difficulty = 'weekly' AND period_start = week_start)
        OR (difficulty IN ('easy', 'medium', 'hard') AND period_start - week_start BETWEEN 0 AND 6)
    )
);

CREATE INDEX bloom_tasks_user_week_idx ON bloom_tasks (user_id, week_start);
CREATE INDEX bloom_tasks_claimed_weekly_idx ON bloom_tasks (user_id)
    WHERE difficulty = 'weekly' AND claimed_at IS NOT NULL;

CREATE TABLE bloom_balance_entries (
    task_id BIGINT PRIMARY KEY REFERENCES bloom_tasks(id) ON DELETE CASCADE,
    delta_micros BIGINT NOT NULL CHECK (delta_micros >= 0),
    balance_after NUMERIC(20, 8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX ops_alert_events_bloom_hard_limit_dedup_idx
    ON ops_alert_events ((dimensions ->> 'user_id'), (dimensions ->> 'week_start'), (dimensions ->> 'reason'))
    WHERE dimensions ->> 'source' = 'bloom_weekly_hard_limit';
