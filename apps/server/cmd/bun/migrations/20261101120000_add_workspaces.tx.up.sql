-- Workspaces and their members (PP-146, docs/adr/0005-workspaces-and-data-isolation.md). A workspace
-- will own the monitors and everything around them; this migration only adds the tables and the
-- instance admin flag, and the records are not linked to a workspace yet. The same file runs on
-- SQLite and PostgreSQL.
--
-- The upgrade's workspace is made by the migration after this one, in Go, because its id is made
-- there and because it must leave an install with nothing in it without a workspace.
CREATE TABLE IF NOT EXISTS workspaces (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- A user's place in a workspace, with one role: owner, admin or member. The first workspace a user
-- joined is their default one. A workspace's members go with the workspace, and a user's places
-- with the user.
CREATE TABLE IF NOT EXISTS workspace_members (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role VARCHAR(16) NOT NULL,
    joined_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, user_id),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- A user's workspaces are listed by user
CREATE INDEX IF NOT EXISTS idx_workspace_members_user_id ON workspace_members(user_id);

-- The settings of a workspace. Nothing writes it yet: PP-150 moves the settings that belong to a
-- workspace here.
CREATE TABLE IF NOT EXISTS workspace_settings (
    workspace_id UUID NOT NULL,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    type VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, key),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

-- Whether the user manages what belongs to the install and not to a workspace. Off until the
-- migration after this one, or the first signup, turns it on.
ALTER TABLE users ADD COLUMN instance_admin BOOLEAN NOT NULL DEFAULT false;
