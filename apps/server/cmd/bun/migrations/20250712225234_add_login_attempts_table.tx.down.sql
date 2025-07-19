-- Rollback login attempts table
-- This migration removes the login attempts table and all associated indexes
-- Wrapped in a transaction for atomicity

-- Drop indexes first
DROP INDEX IF EXISTS idx_login_attempts_ip_attempted_at;
DROP INDEX IF EXISTS idx_login_attempts_email_attempted_at;
DROP INDEX IF EXISTS idx_login_attempts_ip_success;
DROP INDEX IF EXISTS idx_login_attempts_email_success;
DROP INDEX IF EXISTS idx_login_attempts_success;
DROP INDEX IF EXISTS idx_login_attempts_attempted_at;
DROP INDEX IF EXISTS idx_login_attempts_ip_address;
DROP INDEX IF EXISTS idx_login_attempts_email;

-- Drop the table
DROP TABLE IF EXISTS login_attempts;
