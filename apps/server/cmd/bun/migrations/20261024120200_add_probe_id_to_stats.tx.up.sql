-- Count each probe's checks in a stat row of its own, so a monitor that runs on several probes
-- keeps the figures of each. Until now Peekaping ran every check itself, so every stat that
-- exists counts local checks. The same file runs on SQLite and PostgreSQL.
--
-- probe_id is part of the primary key, which SQLite can't change in place, so each table is
-- rebuilt: copied into a table with the new key, then swapped for the old one. The key lists
-- probe_id last, so a monitor's stats over a time range are still one range of the key.
-- On PostgreSQL the renamed table's key keeps the name of the copy, stat_<period>_new_pkey.

CREATE TABLE stat_minutely_new (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    probe_id VARCHAR(255) NOT NULL DEFAULT 'local',
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION, -- NULL until the bucket has a response time
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp, probe_id),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_minutely_new (monitor_id, timestamp, probe_id, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, 'local', up, down, maintenance, ping_sum, ping_count, ping_min, ping_max FROM stat_minutely;
DROP TABLE stat_minutely;
ALTER TABLE stat_minutely_new RENAME TO stat_minutely;

CREATE TABLE stat_hourly_new (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    probe_id VARCHAR(255) NOT NULL DEFAULT 'local',
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION,
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp, probe_id),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_hourly_new (monitor_id, timestamp, probe_id, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, 'local', up, down, maintenance, ping_sum, ping_count, ping_min, ping_max FROM stat_hourly;
DROP TABLE stat_hourly;
ALTER TABLE stat_hourly_new RENAME TO stat_hourly;

CREATE TABLE stat_daily_new (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    probe_id VARCHAR(255) NOT NULL DEFAULT 'local',
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION,
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp, probe_id),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_daily_new (monitor_id, timestamp, probe_id, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, 'local', up, down, maintenance, ping_sum, ping_count, ping_min, ping_max FROM stat_daily;
DROP TABLE stat_daily;
ALTER TABLE stat_daily_new RENAME TO stat_daily;
