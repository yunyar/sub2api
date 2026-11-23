ALTER TABLE usage_billing_dedup
    ADD COLUMN wallet_charged_amount NUMERIC(20, 8) CHECK (wallet_charged_amount >= 0);

ALTER TABLE usage_billing_dedup_archive
    ADD COLUMN wallet_charged_amount NUMERIC(20, 8) CHECK (wallet_charged_amount >= 0);

CREATE INDEX idx_playground_image_balance_holds_user_settled
    ON playground_image_balance_holds (user_id, settled_at)
    WHERE status = 'captured';
