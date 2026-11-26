-- Scheduling a workspace's deletion (PP-170). The same file runs on SQLite and PostgreSQL.
--
-- deletion_scheduled_at is the moment the workspace will be deleted: when its owner scheduled the
-- deletion, plus 7 days. NULL when no deletion is scheduled. It is a hold of its own beside
-- suspended_at (the email verification's) and admin_suspended_at: a workspace is suspended while
-- any of the three is set, and cancelling the deletion clears only this one. Nothing existing is
-- scheduled, so an install that upgrades changes in no way.
ALTER TABLE workspaces ADD COLUMN deletion_scheduled_at TIMESTAMP;
