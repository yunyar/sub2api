CREATE TABLE IF NOT EXISTS playground_image_balance_holds (
    batch_id VARCHAR(255) PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    hold_amount NUMERIC(20,8) NOT NULL,
    unit_amount NUMERIC(20,8) NOT NULL,
    requested_count INTEGER NOT NULL,
    payload_hash VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    actual_amount NUMERIC(20,8),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_playground_image_balance_holds_pending
    ON playground_image_balance_holds (created_at)
    WHERE status = 'pending';
