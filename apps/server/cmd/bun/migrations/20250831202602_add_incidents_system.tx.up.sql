-- Add incidents system for status pages
-- This migration adds support for creating and managing incidents
-- Wrapped in a transaction for atomicity

-- Incidents table for storing incident information
CREATE TABLE IF NOT EXISTS incidents (
    id UUID PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    style VARCHAR(30) NOT NULL DEFAULT 'warning', -- warning, info, success, error
    pin BOOLEAN NOT NULL DEFAULT true,
    active BOOLEAN NOT NULL DEFAULT true,
    status_page_id UUID,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (status_page_id) REFERENCES status_pages(id) ON DELETE CASCADE
);

-- Create indexes for better performance
-- Status page incidents index for efficient queries
CREATE INDEX IF NOT EXISTS idx_incidents_status_page_id ON incidents(status_page_id);

-- Active incidents index for filtering
CREATE INDEX IF NOT EXISTS idx_incidents_active ON incidents(active);

-- Pin and active composite index for status page display
CREATE INDEX IF NOT EXISTS idx_incidents_status_page_pin_active ON incidents(status_page_id, pin, active);

-- Created at index for chronological ordering
CREATE INDEX IF NOT EXISTS idx_incidents_created_at ON incidents(created_at);

-- Composite index for status page incidents with date ordering
CREATE INDEX IF NOT EXISTS idx_incidents_status_page_created_at ON incidents(status_page_id, created_at DESC);

