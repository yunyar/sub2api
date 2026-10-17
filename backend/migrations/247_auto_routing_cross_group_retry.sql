ALTER TABLE auto_route_executions
    DROP CONSTRAINT IF EXISTS auto_route_executions_attempt_check;

ALTER TABLE auto_route_executions
    ADD CONSTRAINT auto_route_executions_attempt_check
    CHECK (current_attempt >= 0);

ALTER TABLE auto_route_attempts
    DROP CONSTRAINT IF EXISTS auto_route_attempts_first_attempt_only;

ALTER TABLE auto_route_attempts
    DROP CONSTRAINT IF EXISTS auto_route_attempts_attempt_number_check;

ALTER TABLE auto_route_attempts
    ADD CONSTRAINT auto_route_attempts_attempt_number_check
    CHECK (attempt_no >= 1);

ALTER TABLE playground_image_balance_holds
    DROP CONSTRAINT IF EXISTS playground_image_balance_holds_auto_route_check;

ALTER TABLE playground_image_balance_holds
    ADD CONSTRAINT playground_image_balance_holds_auto_route_check
    CHECK (
        (auto_execution_id IS NULL AND actual_group_id IS NULL AND auto_attempt_no IS NULL)
        OR
        (auto_execution_id IS NOT NULL AND actual_group_id IS NOT NULL AND auto_attempt_no >= 1)
    );
