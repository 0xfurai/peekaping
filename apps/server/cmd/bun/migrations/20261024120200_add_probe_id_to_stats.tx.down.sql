-- Add the probes' stats up into one row per monitor and bucket, the way the schema before
-- probe_id held them, and rebuild the tables without the column.

CREATE TABLE stat_minutely_old (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION, -- NULL until the bucket has a response time
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_minutely_old (monitor_id, timestamp, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, SUM(up), SUM(down), SUM(maintenance), SUM(ping_sum), SUM(ping_count), MIN(ping_min), MAX(ping_max)
FROM stat_minutely GROUP BY monitor_id, timestamp;
DROP TABLE stat_minutely;
ALTER TABLE stat_minutely_old RENAME TO stat_minutely;

CREATE TABLE stat_hourly_old (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION,
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_hourly_old (monitor_id, timestamp, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, SUM(up), SUM(down), SUM(maintenance), SUM(ping_sum), SUM(ping_count), MIN(ping_min), MAX(ping_max)
FROM stat_hourly GROUP BY monitor_id, timestamp;
DROP TABLE stat_hourly;
ALTER TABLE stat_hourly_old RENAME TO stat_hourly;

CREATE TABLE stat_daily_old (
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    ping_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_count INTEGER NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION,
    ping_max DOUBLE PRECISION,
    PRIMARY KEY (monitor_id, timestamp),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
);
INSERT INTO stat_daily_old (monitor_id, timestamp, up, down, maintenance, ping_sum, ping_count, ping_min, ping_max)
SELECT monitor_id, timestamp, SUM(up), SUM(down), SUM(maintenance), SUM(ping_sum), SUM(ping_count), MIN(ping_min), MAX(ping_max)
FROM stat_daily GROUP BY monitor_id, timestamp;
DROP TABLE stat_daily;
ALTER TABLE stat_daily_old RENAME TO stat_daily;
