-- Workspace suspension (PP-155): a suspended workspace's monitors are kept and can be edited, but
-- none runs: no checks, no missed-push detection, no notifications. The same file runs on SQLite
-- and PostgreSQL.
--
-- suspended_at is when the workspace was suspended, NULL when it runs. suspended_reason says why:
-- 'unverified' is a workspace made by an open signup whose owner has not verified their email yet
-- (verifying lifts it). Nothing existing is suspended, so an install that upgrades changes in no way.
ALTER TABLE workspaces ADD COLUMN suspended_at TIMESTAMP;
ALTER TABLE workspaces ADD COLUMN suspended_reason VARCHAR(32);
