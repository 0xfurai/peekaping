-- Rolling back loses the stored credentials: they are not put back inside config.
ALTER TABLE notification_channels DROP COLUMN secret;
