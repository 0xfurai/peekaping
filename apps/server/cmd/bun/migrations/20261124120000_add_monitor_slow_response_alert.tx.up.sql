-- Per-monitor slow response time alert settings and per-probe alert state.
-- The same file runs on SQLite and PostgreSQL.
--
-- slow_response_mode is off, fixed or baseline. Every existing monitor defaults to off.
ALTER TABLE monitors ADD COLUMN slow_response_mode VARCHAR(16) NOT NULL DEFAULT 'off';
ALTER TABLE monitors ADD COLUMN slow_response_threshold_ms INTEGER;
ALTER TABLE monitors ADD COLUMN slow_response_multiplier DOUBLE PRECISION;
ALTER TABLE monitors ADD COLUMN slow_response_min_ms INTEGER;
ALTER TABLE monitors ADD COLUMN slow_response_consecutive INTEGER NOT NULL DEFAULT 3;

-- Per-(monitor, probe) slow response tracking state so consecutive slow checks and recovery
-- notifications persist across restarts and across API replicas.
CREATE TABLE IF NOT EXISTS monitor_slow_response_states (
    monitor_id UUID NOT NULL,
    probe_id VARCHAR(255) NOT NULL,
    consecutive_count INTEGER NOT NULL DEFAULT 0,
    alerting BOOLEAN NOT NULL DEFAULT FALSE,
    last_ping_ms INTEGER NOT NULL DEFAULT 0,
    threshold_ms INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (monitor_id, probe_id),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
