-- Down migration for workspaces and user_workspace tables
-- This migration drops the workspaces and user_workspace tables and related indexes

BEGIN;

-- Drop indexes first
DROP INDEX IF EXISTS idx_user_workspace_role;
DROP INDEX IF EXISTS idx_user_workspace_workspace_id;
DROP INDEX IF EXISTS idx_user_workspace_user_id;
DROP INDEX IF EXISTS idx_workspaces_created_at;

-- Drop junction table first (has foreign keys)
DROP TABLE IF EXISTS user_workspace;

-- Drop workspaces table
DROP TABLE IF EXISTS workspaces;

COMMIT;
