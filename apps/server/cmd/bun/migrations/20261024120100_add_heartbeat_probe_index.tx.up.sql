-- A monitor's heartbeats from one probe, newest first: the heartbeat list filtered by probe, and
-- the previous heartbeat of a monitor on a probe. On PostgreSQL the build blocks heartbeat writes,
-- not reads.
CREATE INDEX IF NOT EXISTS idx_heartbeats_monitor_probe_time ON heartbeats(monitor_id, probe_id, time);
