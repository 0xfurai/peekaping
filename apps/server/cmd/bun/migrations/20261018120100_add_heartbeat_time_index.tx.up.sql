-- The hourly cleanup deletes the oldest heartbeats in batches through this
-- index, whatever order they were written in. It runs after the previous
-- migration dropped three indexes, so SQLite builds it in the pages they freed
-- instead of growing the file. On PostgreSQL the build blocks heartbeat writes,
-- not reads.
CREATE INDEX IF NOT EXISTS idx_heartbeats_time ON heartbeats(time);
