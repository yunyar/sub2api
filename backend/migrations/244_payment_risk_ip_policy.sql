CREATE TABLE IF NOT EXISTS payment_risk_ip_locks (
    ip VARCHAR(64) PRIMARY KEY,
    updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS payment_risk_user_ips (
    ip VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    source VARCHAR(32) NOT NULL,
    first_seen BIGINT NOT NULL,
    last_seen BIGINT NOT NULL,
    PRIMARY KEY (ip, user_id)
);
CREATE INDEX IF NOT EXISTS payment_risk_user_ips_user_idx ON payment_risk_user_ips(user_id);

CREATE TABLE IF NOT EXISTS payment_risk_ips (
    ip VARCHAR(64) PRIMARY KEY,
    reason VARCHAR(512) NOT NULL,
    evidence TEXT NOT NULL,
    actor VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    released_at BIGINT
);

CREATE TABLE IF NOT EXISTS payment_risk_events (
    id VARCHAR(36) PRIMARY KEY,
    ip VARCHAR(64) NOT NULL,
    user_id BIGINT,
    kind VARCHAR(64) NOT NULL,
    reason VARCHAR(512) NOT NULL,
    evidence TEXT NOT NULL,
    actor VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS payment_risk_events_ip_time_idx ON payment_risk_events(ip, kind, created_at);
