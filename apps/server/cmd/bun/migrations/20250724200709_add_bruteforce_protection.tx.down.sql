-- Rollback migration: Remove bruteforce protection table
-- This migration removes the bruteforce protection table and indexes
-- Wrapped in a transaction for atomicity

-- Drop index first
DROP INDEX IF EXISTS login_state_locked_until_idx;

-- Drop table
DROP TABLE IF EXISTS login_state;
