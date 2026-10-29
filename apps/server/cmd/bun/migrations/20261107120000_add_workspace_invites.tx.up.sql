-- Invites to a workspace (PP-154, docs/adr/0005-workspaces-and-data-isolation.md section 11). An
-- admin or owner invites someone by email with a role. The invite is a link with a random token
-- that is shown once, when the invite is made, and stored only as a hash. It works once, for the
-- account with that email, until it expires or is revoked. The same file runs on SQLite and
-- PostgreSQL.
--
-- email is lower-cased and trimmed. The invite stays as a record after it is used or revoked: it
-- says who was invited by whom, and accepted_at and accepted_by say who joined through it. A
-- workspace's invites go with the workspace; a deleted user stays on the invites they sent and
-- joined through as no one.
CREATE TABLE IF NOT EXISTS workspace_invites (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    email VARCHAR(255) NOT NULL,
    role VARCHAR(16) NOT NULL,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    invited_by UUID,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    accepted_at TIMESTAMP,
    accepted_by UUID,
    revoked_at TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (accepted_by) REFERENCES users(id) ON DELETE SET NULL
);

-- A workspace's invites are listed by workspace
CREATE INDEX IF NOT EXISTS idx_workspace_invites_workspace_id ON workspace_invites(workspace_id);
