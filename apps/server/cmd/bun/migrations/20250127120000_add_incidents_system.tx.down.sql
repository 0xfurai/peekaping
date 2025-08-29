-- Down migration for incidents system
-- This migration drops the incidents table and related indexes
-- Wrapped in a transaction for atomicity

BEGIN;

-- Drop indexes first
DROP INDEX IF EXISTS idx_incidents_status_page_created_at;
DROP INDEX IF EXISTS idx_incidents_created_at;
DROP INDEX IF EXISTS idx_incidents_status_page_pin_active;
DROP INDEX IF EXISTS idx_incidents_active;
DROP INDEX IF EXISTS idx_incidents_status_page_id;

-- Drop incidents table
DROP TABLE IF EXISTS incidents;

COMMIT;

