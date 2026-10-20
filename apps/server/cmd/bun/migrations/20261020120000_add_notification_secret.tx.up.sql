-- Credentials get their own column. Config is returned by every response, so a password
-- kept inside it was readable by anyone who could list channels (#257). The Go migration
-- that follows moves what existing rows already carry inside config.
ALTER TABLE notification_channels ADD COLUMN secret TEXT;
