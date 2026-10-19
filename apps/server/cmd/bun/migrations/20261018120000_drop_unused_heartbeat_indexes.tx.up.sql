-- Drop the heartbeat indexes no query needs. Checked with EXPLAIN on SQLite and
-- PostgreSQL for every query in heartbeat.sql.repository.go:
-- idx_heartbeats_monitor_time is a prefix of idx_heartbeats_monitor_time_important,
-- which serves the same queries; idx_heartbeats_status only served FindActive,
-- which nothing calls; no query filters on important without monitor_id.
--
-- The next migration adds idx_heartbeats_time. It's a migration of its own so
-- this one commits first: on PostgreSQL, DROP INDEX locks heartbeats against
-- reads until the transaction ends, which would otherwise last the whole build.

DROP INDEX IF EXISTS idx_heartbeats_monitor_time;
DROP INDEX IF EXISTS idx_heartbeats_status;
DROP INDEX IF EXISTS idx_heartbeats_important;
