-- Add tags system for monitors
-- This migration adds support for tagging monitors
-- Wrapped in a transaction for atomicity

-- Tags table for storing tag definitions
CREATE TABLE IF NOT EXISTS tags (
    id UUID PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    color VARCHAR(7) NOT NULL DEFAULT '#3B82F6', -- Hex color code
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Monitor tags junction table for many-to-many relationship
CREATE TABLE IF NOT EXISTS monitor_tags (
    id UUID PRIMARY KEY,
    monitor_id UUID NOT NULL,
    tag_id UUID NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
    UNIQUE(monitor_id, tag_id)
);

-- Create indexes for better performance
-- Tag name index for searches
CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name);

-- Monitor tags indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_monitor_tags_monitor_id ON monitor_tags(monitor_id);
CREATE INDEX IF NOT EXISTS idx_monitor_tags_tag_id ON monitor_tags(tag_id);

-- Composite index for tag filtering
CREATE INDEX IF NOT EXISTS idx_monitor_tags_tag_monitor ON monitor_tags(tag_id, monitor_id);
