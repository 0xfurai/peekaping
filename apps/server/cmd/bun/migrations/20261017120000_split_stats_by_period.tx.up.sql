-- Store minute, hour and day stats in separate tables, the way Mongo keeps
-- separate collections. The old stats table held all three in one row per
-- (monitor_id, timestamp), so the scales overwrote each other.
-- The average response time is ping_sum / ping_count, computed on read.

CREATE TABLE IF NOT EXISTS stat_minutely (
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

CREATE TABLE IF NOT EXISTS stat_hourly (
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

CREATE TABLE IF NOT EXISTS stat_daily (
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
