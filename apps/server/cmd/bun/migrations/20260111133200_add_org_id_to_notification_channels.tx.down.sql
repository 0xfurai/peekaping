ALTER TABLE notification_channels DROP CONSTRAINT fk_notification_channels_org_id;
ALTER TABLE notification_channels DROP COLUMN org_id;
