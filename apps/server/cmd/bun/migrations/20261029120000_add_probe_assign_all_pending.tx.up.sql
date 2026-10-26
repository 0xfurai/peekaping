-- A probe that waits to be assigned every monitor (PP-324). An admin who creates a probe asks for
-- this so that it takes over all the monitors on its first hello, and the server can't do it at the
-- creation: the monitor types the probe runs come with its instances. The flag stays set until a
-- hello assigns the monitors, and is cleared in the same transaction. The same file runs on SQLite
-- and PostgreSQL.
ALTER TABLE probes ADD COLUMN assign_all_pending BOOLEAN NOT NULL DEFAULT false;
