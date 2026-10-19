-- Drop the heartbeat indexes no query needs. Checked with EXPLAIN on SQLite and
-- PostgreSQL for every query in heartbeat.sql.repository.go:
-- idx_heartbeats_monitor_time is a prefix of idx_heartbeats_monitor_time_important,
-- which serves the same queries; idx_heartbeats_status only served FindActive,
-- which nothing calls; no query filters on important without monitor_id.

DROP INDEX IF EXISTS idx_heartbeats_monitor_time;
DROP INDEX IF EXISTS idx_heartbeats_status;
DROP INDEX IF EXISTS idx_heartbeats_important;

-- The hourly cleanup deletes the oldest heartbeats in batches through this
-- index, whatever order they were written in. Created after the drops, so
-- SQLite builds it in the pages they freed instead of growing the file.
CREATE INDEX IF NOT EXISTS idx_heartbeats_time ON heartbeats(time);
