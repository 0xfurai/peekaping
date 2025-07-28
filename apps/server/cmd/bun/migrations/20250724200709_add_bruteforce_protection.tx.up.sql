-- Migration: Add bruteforce protection table
-- This migration adds a single table for tracking login state including failures and lockouts
-- Wrapped in a transaction for atomicity

-- Login state table to track failures and locks atomically
CREATE TABLE IF NOT EXISTS login_state (
    key           TEXT PRIMARY KEY,
    fail_count    INT          NOT NULL,
    first_fail_at TIMESTAMPTZ  NOT NULL,
    locked_until  TIMESTAMPTZ
);

-- Create index for efficient expiration queries
CREATE INDEX IF NOT EXISTS login_state_locked_until_idx ON login_state (locked_until);
