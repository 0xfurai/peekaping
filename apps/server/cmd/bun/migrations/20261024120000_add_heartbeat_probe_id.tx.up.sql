-- The probe that ran the check behind each heartbeat. Until now Peekaping ran every check itself, so
-- every heartbeat that exists was made by the local probe. The same file runs on SQLite and PostgreSQL.
--
-- The default keeps a writer that doesn't know the column on the local probe. PostgreSQL adds a column
-- with a constant default without rewriting the table. The index is the next migration, so this one
-- commits first: building it inside this transaction would hold the table against reads for the whole build.
ALTER TABLE heartbeats ADD COLUMN probe_id VARCHAR(255) NOT NULL DEFAULT 'local';
