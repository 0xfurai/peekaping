-- Add workspaces and user_workspaces tables for team collaboration
-- This migration creates the workspaces table and user_workspaces junction table for many-to-many relationships

BEGIN;

-- Workspaces table for team collaboration
CREATE TABLE IF NOT EXISTS workspaces (
    id UUID PRIMARY KEY,
    name VARCHAR(150) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- User-Workspace junction table for many-to-many relationship
CREATE TABLE IF NOT EXISTS user_workspace (
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'member',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, workspace_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

-- Create indexes for efficient lookups
CREATE INDEX IF NOT EXISTS idx_workspaces_created_at ON workspaces(created_at);
CREATE INDEX IF NOT EXISTS idx_user_workspace_user_id ON user_workspace(user_id);
CREATE INDEX IF NOT EXISTS idx_user_workspace_workspace_id ON user_workspace(workspace_id);
CREATE INDEX IF NOT EXISTS idx_user_workspace_role ON user_workspace(role);

COMMIT;
