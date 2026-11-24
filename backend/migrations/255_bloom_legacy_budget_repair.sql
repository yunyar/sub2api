ALTER TABLE bloom_week_budgets ADD COLUMN legacy_repair_pending BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE bloom_week_budgets
SET legacy_repair_pending = TRUE
WHERE budget_micros = 0 AND spent_micros = 0;
