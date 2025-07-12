-- Down migration for tags system
-- This migration removes the tags system tables and indexes
-- Wrapped in a transaction for atomicity

BEGIN;

-- Drop indexes first
DROP INDEX IF EXISTS idx_monitor_tags_tag_monitor;
DROP INDEX IF EXISTS idx_monitor_tags_tag_id;
DROP INDEX IF EXISTS idx_monitor_tags_monitor_id;
DROP INDEX IF EXISTS idx_tags_name;

-- Drop junction table first (has foreign key constraints)
DROP TABLE IF EXISTS monitor_tags;

-- Drop tags table
DROP TABLE IF EXISTS tags;

COMMIT;
