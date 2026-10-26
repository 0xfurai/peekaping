-- Whether a status page lists each location of a monitor that runs on several probes. Off for every
-- existing page, so they look as they did. The same file runs on SQLite and PostgreSQL.
ALTER TABLE status_pages ADD COLUMN show_locations BOOLEAN NOT NULL DEFAULT false;
