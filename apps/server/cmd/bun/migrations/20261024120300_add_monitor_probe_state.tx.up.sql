-- A monitor's state on each probe it runs on, kept on the row that says it runs there (#36):
-- the status of the probe's latest heartbeat (the same codes as heartbeats.status), its ping, and
-- when it was made. NULL means the probe has not reported yet. The same file runs on SQLite and
-- PostgreSQL.
ALTER TABLE monitor_probes ADD COLUMN status INTEGER;
ALTER TABLE monitor_probes ADD COLUMN last_ping INTEGER;
ALTER TABLE monitor_probes ADD COLUMN last_heartbeat_at TIMESTAMP;

-- Every monitor so far ran on one probe, so its row takes the state of that probe's latest
-- heartbeat. A monitor whose probe has no heartbeat yet keeps NULL. The lookups use
-- idx_heartbeats_monitor_probe_time.
UPDATE monitor_probes SET
    status = (SELECT h.status FROM heartbeats h
              WHERE h.monitor_id = monitor_probes.monitor_id AND h.probe_id = monitor_probes.probe_id
              ORDER BY h.time DESC LIMIT 1),
    last_ping = (SELECT h.ping FROM heartbeats h
                 WHERE h.monitor_id = monitor_probes.monitor_id AND h.probe_id = monitor_probes.probe_id
                 ORDER BY h.time DESC LIMIT 1),
    last_heartbeat_at = (SELECT h.time FROM heartbeats h
                         WHERE h.monitor_id = monitor_probes.monitor_id AND h.probe_id = monitor_probes.probe_id
                         ORDER BY h.time DESC LIMIT 1);
