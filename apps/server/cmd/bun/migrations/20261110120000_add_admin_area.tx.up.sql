-- The admin area (PP-371). The same file runs on SQLite and PostgreSQL.
--
-- users.last_login_at is when the user last signed in (a login, or the signup that signed them in).
-- NULL means they have not since this column was added.
ALTER TABLE users ADD COLUMN last_login_at TIMESTAMP;

-- probes.is_default marks the probes that a monitor created without choosing probes runs on, which
-- an instance admin sets. Before this a monitor created without probes ran on the local probe, so
-- the local probe is the default one: an install that upgrades changes in no way.
ALTER TABLE probes ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false;
UPDATE probes SET is_default = true WHERE id = 'local';
