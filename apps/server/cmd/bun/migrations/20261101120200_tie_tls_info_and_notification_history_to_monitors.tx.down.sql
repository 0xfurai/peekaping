-- Gives monitor_id back its old type, a string nothing checks. The rows stay.

CREATE TABLE monitor_tls_info_keep AS
SELECT id, CAST(monitor_id AS VARCHAR(255)) AS monitor_id, info_json, created_at, updated_at FROM monitor_tls_info;

DROP TABLE monitor_tls_info;

CREATE TABLE monitor_tls_info (
    id UUID PRIMARY KEY,
    monitor_id VARCHAR(255) NOT NULL UNIQUE,
    info_json TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_monitor_tls_info_monitor_id ON monitor_tls_info(monitor_id);
CREATE INDEX idx_monitor_tls_info_updated_at ON monitor_tls_info(updated_at);

INSERT INTO monitor_tls_info (id, monitor_id, info_json, created_at, updated_at)
SELECT id, monitor_id, info_json, created_at, updated_at FROM monitor_tls_info_keep;

DROP TABLE monitor_tls_info_keep;

CREATE TABLE notification_sent_history_keep AS
SELECT id, type, CAST(monitor_id AS VARCHAR(255)) AS monitor_id, days, created_at FROM notification_sent_history;

DROP TABLE notification_sent_history;

CREATE TABLE notification_sent_history (
    id UUID PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    monitor_id VARCHAR(255) NOT NULL,
    days INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(type, monitor_id, days)
);

CREATE INDEX idx_notification_sent_history_type_monitor ON notification_sent_history(type, monitor_id);
CREATE INDEX idx_notification_sent_history_created_at ON notification_sent_history(created_at);

INSERT INTO notification_sent_history (id, type, monitor_id, days, created_at)
SELECT id, type, monitor_id, days, created_at FROM notification_sent_history_keep;

DROP TABLE notification_sent_history_keep;
