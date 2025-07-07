-- Down migration for Peekaping monitoring system
-- This migration drops all tables created in the initial migration
-- Wrapped in a transaction for atomicity

BEGIN;

-- Drop indexes first
-- Single column indexes for specific filtering needs
DROP INDEX IF EXISTS idx_proxies_host_port;
DROP INDEX IF EXISTS idx_users_active;
DROP INDEX IF EXISTS idx_notification_channels_active;
DROP INDEX IF EXISTS idx_notification_channels_type;
DROP INDEX IF EXISTS idx_status_pages_published;
DROP INDEX IF EXISTS idx_maintenances_active;
DROP INDEX IF EXISTS idx_heartbeats_important;
DROP INDEX IF EXISTS idx_heartbeats_status;

-- Enhanced composite for complex heartbeat queries
DROP INDEX IF EXISTS idx_heartbeats_monitor_time_important;

-- Composite indexes for common filtering patterns
DROP INDEX IF EXISTS idx_heartbeats_monitor_important;
DROP INDEX IF EXISTS idx_monitors_active_status;

-- Composite indexes for time-series queries (CRITICAL for performance)
DROP INDEX IF EXISTS idx_stats_monitor_timestamp;
DROP INDEX IF EXISTS idx_heartbeats_monitor_time;

-- Core foreign key and unique indexes
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_status_pages_slug;
DROP INDEX IF EXISTS idx_maintenances_user_id;
DROP INDEX IF EXISTS idx_monitors_proxy_id;

-- Drop junction tables first (they reference other tables)
DROP TABLE IF EXISTS monitor_status_pages;
DROP TABLE IF EXISTS monitor_maintenances;
DROP TABLE IF EXISTS monitor_notifications;

-- Drop main tables
DROP TABLE IF EXISTS stats;
DROP TABLE IF EXISTS heartbeats;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS maintenances;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS status_pages;
DROP TABLE IF EXISTS monitors;
DROP TABLE IF EXISTS proxies;
DROP TABLE IF EXISTS users;

COMMIT;
