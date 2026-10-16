CREATE TABLE IF NOT EXISTS auto_group_profiles (
    id                       BIGSERIAL PRIMARY KEY,
    name                     VARCHAR(100) NOT NULL,
    status                   VARCHAR(20) NOT NULL DEFAULT 'disabled',
    revision                 BIGINT NOT NULL DEFAULT 1,
    max_candidates           INTEGER NOT NULL DEFAULT 10,
    allow_custom_order       BOOLEAN NOT NULL DEFAULT TRUE,
    cross_group_retry_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auto_group_profiles_status_check
        CHECK (status IN ('active', 'disabled')),
    CONSTRAINT auto_group_profiles_revision_check CHECK (revision > 0),
    CONSTRAINT auto_group_profiles_max_candidates_check CHECK (max_candidates > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_group_profiles_name
    ON auto_group_profiles (LOWER(name));

CREATE TABLE IF NOT EXISTS auto_group_profile_members (
    profile_id BIGINT NOT NULL REFERENCES auto_group_profiles(id) ON DELETE RESTRICT,
    group_id   BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    sort_order INTEGER NOT NULL,
    PRIMARY KEY (profile_id, group_id),
    CONSTRAINT auto_group_profile_members_sort_order_check CHECK (sort_order >= 0),
    CONSTRAINT auto_group_profile_members_order_unique UNIQUE (profile_id, sort_order)
);

CREATE INDEX IF NOT EXISTS idx_auto_group_profile_members_group_id
    ON auto_group_profile_members (group_id, profile_id);

CREATE TABLE IF NOT EXISTS auto_key_routing (
    api_key_id        BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    routing_mode      VARCHAR(16) NOT NULL DEFAULT 'auto',
    profile_id        BIGINT NOT NULL REFERENCES auto_group_profiles(id) ON DELETE RESTRICT,
    selection_mode    VARCHAR(16) NOT NULL,
    cross_group_retry BOOLEAN NOT NULL DEFAULT FALSE,
    revision          BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auto_key_routing_mode_check CHECK (routing_mode = 'auto'),
    CONSTRAINT auto_key_routing_selection_mode_check
        CHECK (selection_mode IN ('inherit', 'custom')),
    CONSTRAINT auto_key_routing_revision_check CHECK (revision > 0)
);

CREATE TABLE IF NOT EXISTS auto_key_routing_groups (
    api_key_id BIGINT NOT NULL REFERENCES auto_key_routing(api_key_id) ON DELETE CASCADE,
    group_id   BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    sort_order INTEGER NOT NULL,
    PRIMARY KEY (api_key_id, group_id),
    CONSTRAINT auto_key_routing_groups_sort_order_check CHECK (sort_order >= 0),
    CONSTRAINT auto_key_routing_groups_order_unique UNIQUE (api_key_id, sort_order)
);

CREATE INDEX IF NOT EXISTS idx_auto_key_routing_groups_group_id
    ON auto_key_routing_groups (group_id, api_key_id);

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS routing_mode TEXT NOT NULL DEFAULT 'fixed',
    ADD COLUMN IF NOT EXISTS routing_revision BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'api_keys_routing_mode_check'
          AND conrelid = 'api_keys'::regclass
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_routing_mode_check
            CHECK (
                routing_mode IN ('fixed', 'auto')
                AND routing_revision >= 0
                AND (routing_mode = 'fixed' OR group_id IS NULL)
            );
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS playground_auto_key_bindings (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id BIGINT NOT NULL REFERENCES auto_group_profiles(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, profile_id)
);

CREATE TABLE IF NOT EXISTS auto_route_executions (
    id                  BIGSERIAL PRIMARY KEY,
    api_key_id          BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    request_id          TEXT NOT NULL,
    payload_hash        CHAR(64) NOT NULL,
    profile_id          BIGINT NOT NULL REFERENCES auto_group_profiles(id) ON DELETE RESTRICT,
    profile_revision    BIGINT NOT NULL,
    routing_revision    BIGINT NOT NULL,
    candidate_group_ids JSONB NOT NULL,
    status              VARCHAR(20) NOT NULL DEFAULT 'prepared',
    current_attempt     INTEGER NOT NULL DEFAULT 0,
    actual_group_id     BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    result_reference    TEXT,
    evidence_code       VARCHAR(100),
    settlement_status   VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    CONSTRAINT auto_route_executions_key_request_unique UNIQUE (api_key_id, request_id),
    CONSTRAINT auto_route_executions_status_check
        CHECK (status IN ('prepared', 'dispatching', 'completed', 'failed', 'unknown')),
    CONSTRAINT auto_route_executions_attempt_check CHECK (current_attempt BETWEEN 0 AND 1),
    CONSTRAINT auto_route_executions_revision_check CHECK (routing_revision > 0),
    CONSTRAINT auto_route_executions_profile_revision_check CHECK (profile_revision > 0),
    CONSTRAINT auto_route_executions_candidates_check
        CHECK (jsonb_typeof(candidate_group_ids) = 'array')
);

CREATE INDEX IF NOT EXISTS idx_auto_route_executions_created_at
    ON auto_route_executions (created_at);

CREATE TABLE IF NOT EXISTS auto_route_attempts (
    id                   BIGSERIAL PRIMARY KEY,
    execution_id         BIGINT NOT NULL REFERENCES auto_route_executions(id) ON DELETE RESTRICT,
    attempt_no           INTEGER NOT NULL,
    group_id             BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    status               VARCHAR(20) NOT NULL DEFAULT 'prepared',
    account_id           BIGINT,
    platform             VARCHAR(32),
    billing_snapshot     JSONB,
    billing_snapshot_ref TEXT,
    upstream_request_id  TEXT,
    upstream_task_id     TEXT,
    result_reference     TEXT,
    dispatched_at        TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auto_route_attempts_execution_number_unique UNIQUE (execution_id, attempt_no),
    CONSTRAINT auto_route_attempts_first_attempt_only CHECK (attempt_no = 1),
    CONSTRAINT auto_route_attempts_status_check
        CHECK (status IN ('prepared', 'dispatching', 'completed', 'failed', 'unknown'))
);

CREATE INDEX IF NOT EXISTS idx_auto_route_attempts_group_id
    ON auto_route_attempts (group_id, created_at);

ALTER TABLE playground_image_balance_holds
    ADD COLUMN IF NOT EXISTS auto_execution_id BIGINT
        REFERENCES auto_route_executions(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS actual_group_id BIGINT
        REFERENCES groups(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS auto_attempt_no INTEGER,
    ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'playground_image_balance_holds_auto_route_check'
          AND conrelid = 'playground_image_balance_holds'::regclass
    ) THEN
        ALTER TABLE playground_image_balance_holds
            ADD CONSTRAINT playground_image_balance_holds_auto_route_check
            CHECK (
                (auto_execution_id IS NULL AND actual_group_id IS NULL AND auto_attempt_no IS NULL)
                OR
                (auto_execution_id IS NOT NULL AND actual_group_id IS NOT NULL AND auto_attempt_no = 1)
            );
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_playground_image_balance_holds_auto_execution
    ON playground_image_balance_holds (auto_execution_id)
    WHERE auto_execution_id IS NOT NULL;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS routing_mode TEXT NOT NULL DEFAULT 'fixed',
    ADD COLUMN IF NOT EXISTS auto_profile_id BIGINT
        REFERENCES auto_group_profiles(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS routing_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS route_attempt_count INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'usage_logs_routing_audit_check'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_routing_audit_check
            CHECK (
                (routing_mode = 'fixed' AND auto_profile_id IS NULL AND routing_revision = 0 AND route_attempt_count = 0)
                OR
                (routing_mode = 'auto' AND auto_profile_id IS NOT NULL AND routing_revision > 0 AND route_attempt_count >= 0)
            );
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_api_key_auto_routing_group_null()
RETURNS trigger
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
    auto_routing_revision BIGINT;
BEGIN
    SELECT revision
    INTO auto_routing_revision
    FROM auto_key_routing
    WHERE api_key_id = NEW.id;

    IF NEW.routing_mode = 'auto' THEN
        IF NEW.group_id IS NOT NULL
           OR auto_routing_revision IS NULL
           OR NEW.routing_revision <> auto_routing_revision THEN
            RAISE EXCEPTION 'Auto API key marker requires a null group and Auto routing config'
                USING ERRCODE = '23514',
                      CONSTRAINT = 'api_keys_auto_routing_group_null';
        END IF;
    ELSIF auto_routing_revision IS NOT NULL THEN
        RAISE EXCEPTION 'Fixed API key cannot retain Auto routing config'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'api_keys_auto_routing_group_null';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_keys_auto_routing_group_null ON api_keys;
DROP TRIGGER IF EXISTS trg_api_keys_auto_routing_group_null_update ON api_keys;
CREATE TRIGGER trg_api_keys_auto_routing_group_null
    BEFORE INSERT ON api_keys
    FOR EACH ROW
    EXECUTE FUNCTION enforce_api_key_auto_routing_group_null();
CREATE TRIGGER trg_api_keys_auto_routing_group_null_update
    BEFORE UPDATE OF group_id, routing_mode, routing_revision ON api_keys
    FOR EACH ROW
    EXECUTE FUNCTION enforce_api_key_auto_routing_group_null();

CREATE OR REPLACE FUNCTION enforce_auto_routing_key_group_null()
RETURNS trigger
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
    api_key_group_id BIGINT;
    api_key_routing_revision BIGINT;
    api_key_deleted_at TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.api_key_id <> OLD.api_key_id THEN
        RAISE EXCEPTION 'Auto routing API key identity is immutable'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'auto_key_routing_api_key_immutable';
    END IF;

    SELECT group_id, deleted_at, routing_revision
    INTO api_key_group_id, api_key_deleted_at, api_key_routing_revision
    FROM api_keys
    WHERE id = NEW.api_key_id
    FOR UPDATE;

    IF NOT FOUND OR api_key_deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'Auto routing requires a live API key'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'auto_key_routing_api_key_live';
    END IF;
    IF api_key_group_id IS NOT NULL THEN
        RAISE EXCEPTION 'Auto-routed API keys must have a null group_id'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'auto_key_routing_api_key_group_null';
    END IF;
    NEW.revision := api_key_routing_revision + 1;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_auto_key_routing_group_null ON auto_key_routing;
DROP TRIGGER IF EXISTS trg_auto_key_routing_group_null_update ON auto_key_routing;
CREATE TRIGGER trg_auto_key_routing_group_null
    BEFORE INSERT ON auto_key_routing
    FOR EACH ROW
    EXECUTE FUNCTION enforce_auto_routing_key_group_null();
CREATE TRIGGER trg_auto_key_routing_group_null_update
    BEFORE UPDATE ON auto_key_routing
    FOR EACH ROW
    EXECUTE FUNCTION enforce_auto_routing_key_group_null();

CREATE OR REPLACE FUNCTION sync_api_key_auto_routing_mode()
RETURNS trigger
LANGUAGE plpgsql
VOLATILE
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE api_keys
        SET routing_mode = 'fixed',
            routing_revision = OLD.revision + 1,
            updated_at = NOW()
        WHERE id = OLD.api_key_id AND deleted_at IS NULL;
        RETURN OLD;
    END IF;

    UPDATE api_keys
    SET routing_mode = 'auto',
        routing_revision = NEW.revision,
        updated_at = NOW()
    WHERE id = NEW.api_key_id AND deleted_at IS NULL;

    IF TG_OP = 'UPDATE' AND OLD.api_key_id <> NEW.api_key_id THEN
        UPDATE api_keys
        SET routing_mode = 'fixed',
            routing_revision = OLD.revision + 1,
            updated_at = NOW()
        WHERE id = OLD.api_key_id AND deleted_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_api_key_auto_routing_mode ON auto_key_routing;
CREATE TRIGGER trg_sync_api_key_auto_routing_mode
    AFTER INSERT OR UPDATE OR DELETE ON auto_key_routing
    FOR EACH ROW
    EXECUTE FUNCTION sync_api_key_auto_routing_mode();

CREATE OR REPLACE FUNCTION bump_fixed_api_key_routing_revision()
RETURNS trigger
LANGUAGE plpgsql
VOLATILE
AS $$
BEGIN
    UPDATE api_keys
    SET routing_revision = routing_revision + 1
    WHERE id = NEW.id AND routing_revision = OLD.routing_revision;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_bump_fixed_api_key_routing_revision ON api_keys;
CREATE TRIGGER trg_bump_fixed_api_key_routing_revision
    AFTER UPDATE OF group_id ON api_keys
    FOR EACH ROW
    WHEN (
        OLD.group_id IS DISTINCT FROM NEW.group_id
        AND OLD.routing_mode = 'fixed'
        AND NEW.routing_mode = 'fixed'
        AND OLD.routing_revision = NEW.routing_revision
        AND current_setting('app.auto_routing_write', true) IS DISTINCT FROM 'on'
    )
    EXECUTE FUNCTION bump_fixed_api_key_routing_revision();
