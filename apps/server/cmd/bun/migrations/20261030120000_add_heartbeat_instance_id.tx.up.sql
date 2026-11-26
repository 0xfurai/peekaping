-- The probe instance that ran the check behind each heartbeat (PP-331): the id the probe protocol
-- carries in X-Probe-Instance. NULL for a heartbeat no probe instance made: the ones stored before
-- this migration, a push, and a check the peekaping process ran itself in single mode. The same file
-- runs on SQLite and PostgreSQL.
--
-- Nullable, with no default and no index, so neither database rewrites the table.
ALTER TABLE heartbeats ADD COLUMN instance_id VARCHAR(255);
