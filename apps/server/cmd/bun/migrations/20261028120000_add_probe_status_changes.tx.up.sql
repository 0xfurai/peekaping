-- The history of each probe's status (PP-330): one row for every time it went online or offline, so
-- that the web app can draw when a probe was reachable. probes.status stays the current status;
-- this table is how it got there. The same file runs on SQLite and PostgreSQL.
--
-- at is when the status really changed: the time the probe's instances were last seen, for a probe
-- that went offline, and not the time the server noticed. It is never earlier than the probe's
-- row before it. recorded_at is when the server stored the row. Two changes can share an at (a
-- probe that comes back and is lost again within a second), and then recorded_at puts them in
-- order, which is why neither is the primary key. The id is a UUID that the server makes.
--
-- A probe's rows go with the probe. A probe starts as offline at its creation (the server writes
-- that row). The rows of the probes that already exist are written by the migration after
-- add_probe_instance_connections, which makes their ids in Go.
CREATE TABLE IF NOT EXISTS probe_status_changes (
    id UUID PRIMARY KEY,
    probe_id VARCHAR(255) NOT NULL,
    status VARCHAR(16) NOT NULL,
    at TIMESTAMP NOT NULL,
    recorded_at TIMESTAMP NOT NULL,
    FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE
);

-- A probe's history is read from a point in time on, and the hourly cleanup looks for each
-- probe's latest row before its cutoff
CREATE INDEX IF NOT EXISTS idx_probe_status_changes_probe_at ON probe_status_changes(probe_id, at);
