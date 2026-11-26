-- When each probe instance was connected (PP-330). A connection starts when the instance says hello,
-- or at its first request after more than a minute of quiet (the online window), and it ends when
-- the instance says bye. An instance that stops without a bye has an open connection (ended_at is
-- NULL) that ends at its last_seen_at, the time of its last request. Restarting a probe's
-- container makes a new instance id, so it ends the old instance's connection and starts a new
-- instance's. The same file runs on SQLite and PostgreSQL.
--
-- last_seen_at is the latest time the connection is known to have lasted to: the instance's latest
-- request, or the bye. The id is a UUID that the server makes, since an instance can start two
-- connections at the same time.
--
-- A connection goes with its instance, so it goes when the sweep forgets the instance after a day,
-- or when a hello takes its place. The probe's id is kept here so that a probe's connections are read
-- without joining.
CREATE TABLE IF NOT EXISTS probe_instance_connections (
    id UUID PRIMARY KEY,
    instance_id VARCHAR(255) NOT NULL,
    probe_id VARCHAR(255) NOT NULL,
    started_at TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL,
    ended_at TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES probe_instances(id) ON DELETE CASCADE,
    FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE
);

-- A request finds its instance's open connection
CREATE INDEX IF NOT EXISTS idx_probe_instance_connections_instance ON probe_instance_connections(instance_id, started_at);

-- A probe's connections are read from a point in time on
CREATE INDEX IF NOT EXISTS idx_probe_instance_connections_probe_seen ON probe_instance_connections(probe_id, last_seen_at);
