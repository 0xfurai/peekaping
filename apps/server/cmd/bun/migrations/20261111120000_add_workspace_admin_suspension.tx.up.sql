-- Suspending a workspace by an instance admin (PP-372). The same file runs on SQLite and
-- PostgreSQL.
--
-- admin_suspended_at is when an instance admin suspended the workspace, NULL when no admin holds
-- it. It is a hold of its own beside suspended_at and suspended_reason, which are the email
-- verification's: lifting the verification hold leaves this one, and resuming leaves that one. A
-- workspace is suspended while either is set. Nothing existing is held, so an install that upgrades
-- changes in no way.
ALTER TABLE workspaces ADD COLUMN admin_suspended_at TIMESTAMP;
