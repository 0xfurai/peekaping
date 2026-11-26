-- How many of a monitor's locations must be Down for the monitor to be Down (#35). location_rule
-- is any, all or at_least, and location_threshold is the number at_least asks for: it is set with
-- at_least and NULL with the other rules. Every existing monitor is any, which is how a monitor
-- was judged before. The same file runs on SQLite and PostgreSQL.
ALTER TABLE monitors ADD COLUMN location_rule VARCHAR(16) NOT NULL DEFAULT 'any';
ALTER TABLE monitors ADD COLUMN location_threshold INTEGER;
