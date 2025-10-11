-- Add API Keys table for third-party integrations
-- This migration creates the api_keys table for secure API key management

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(255), -- Can be NULL initially for two-phase creation
    display_key VARCHAR(20) DEFAULT 'pk_****', -- Can be NULL initially for two-phase creation
    last_used TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    usage_count INTEGER NOT NULL DEFAULT 0,
    max_usage_count INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);
