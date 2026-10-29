-- TLS information and certificate notification history belong to a monitor, and now go with it
-- (PP-146, docs/adr/0005-workspaces-and-data-isolation.md). Their monitor_id was a string that
-- named a monitor and nothing checked it, so deleting a monitor left its rows behind. This makes it
-- a UUID with a foreign key and removes the rows whose monitor is gone. The same file runs on
-- SQLite and PostgreSQL.
--
-- A table can't get a foreign key in place in SQLite, so each table is rebuilt: the rows that
-- belong to a monitor go to a holding table, the old table is dropped, the new one is made under
-- its own name (so that its constraint and index names stay as they were), and the rows come back.
-- The join compares text with text because a string can't be cast to a UUID in SQLite, and takes
-- the monitor's id from the monitors table for the same reason.

CREATE TABLE monitor_tls_info_keep AS
SELECT t.id AS id, m.id AS monitor_id, t.info_json AS info_json, t.created_at AS created_at, t.updated_at AS updated_at
FROM monitor_tls_info t
JOIN monitors m ON CAST(m.id AS TEXT) = t.monitor_id;

DROP TABLE monitor_tls_info;

CREATE TABLE monitor_tls_info (
    id UUID PRIMARY KEY,
    monitor_id UUID NOT NULL UNIQUE,
    info_json TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);

CREATE INDEX idx_monitor_tls_info_monitor_id ON monitor_tls_info(monitor_id);
CREATE INDEX idx_monitor_tls_info_updated_at ON monitor_tls_info(updated_at);

INSERT INTO monitor_tls_info (id, monitor_id, info_json, created_at, updated_at)
SELECT id, monitor_id, info_json, created_at, updated_at FROM monitor_tls_info_keep;

DROP TABLE monitor_tls_info_keep;

CREATE TABLE notification_sent_history_keep AS
SELECT h.id AS id, h.type AS type, m.id AS monitor_id, h.days AS days, h.created_at AS created_at
FROM notification_sent_history h
JOIN monitors m ON CAST(m.id AS TEXT) = h.monitor_id;

DROP TABLE notification_sent_history;

CREATE TABLE notification_sent_history (
    id UUID PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    monitor_id UUID NOT NULL,
    days INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
    UNIQUE(type, monitor_id, days)
);

CREATE INDEX idx_notification_sent_history_type_monitor ON notification_sent_history(type, monitor_id);
CREATE INDEX idx_notification_sent_history_created_at ON notification_sent_history(created_at);

INSERT INTO notification_sent_history (id, type, monitor_id, days, created_at)
SELECT id, type, monitor_id, days, created_at FROM notification_sent_history_keep;

DROP TABLE notification_sent_history_keep;
