-- The probe registry: the probes that run checks for Peekaping, the instances of each probe
-- that have connected, and the notification channels that hear about a probe. The same file
-- runs on SQLite and PostgreSQL.

-- A probe's id is a UUID, except for the local probe's, which is the string 'local'.
-- token_hash is "sha256$" and the hex SHA-256 of the probe's token. It is NULL until the probe
-- has a token, and the token itself is never stored.
CREATE TABLE IF NOT EXISTS probes (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    location VARCHAR(255) NOT NULL DEFAULT '',
    token_hash VARCHAR(255),
    token_prefix VARCHAR(32) NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT false,
    status VARCHAR(16) NOT NULL DEFAULT 'offline',
    supported_types JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- A token finds its probe by its hash, on every request the probe makes
CREATE UNIQUE INDEX IF NOT EXISTS idx_probes_token_hash ON probes(token_hash);

CREATE TABLE IF NOT EXISTS probe_instances (
    id VARCHAR(255) PRIMARY KEY,
    probe_id VARCHAR(255) NOT NULL,
    hostname VARCHAR(255) NOT NULL DEFAULT '',
    os VARCHAR(64) NOT NULL DEFAULT '',
    arch VARCHAR(64) NOT NULL DEFAULT '',
    version VARCHAR(64) NOT NULL DEFAULT '',
    protocol_version INTEGER NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_probe_instances_probe_id ON probe_instances(probe_id);

CREATE TABLE IF NOT EXISTS probe_notification_channels (
    probe_id VARCHAR(255) NOT NULL,
    notification_channel_id UUID NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (probe_id, notification_channel_id),
    FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE,
    FOREIGN KEY (notification_channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_probe_notification_channels_channel ON probe_notification_channels(notification_channel_id);

-- Every install has the local probe: the checks that Peekaping runs itself. A re-run keeps
-- the row as it is.
INSERT INTO probes (id, name) VALUES ('local', 'Local') ON CONFLICT DO NOTHING;
