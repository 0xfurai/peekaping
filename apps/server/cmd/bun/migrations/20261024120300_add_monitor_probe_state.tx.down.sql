ALTER TABLE monitors DROP COLUMN locations_status;
ALTER TABLE monitor_probes DROP COLUMN last_heartbeat_at;
ALTER TABLE monitor_probes DROP COLUMN last_ping;
ALTER TABLE monitor_probes DROP COLUMN status;
