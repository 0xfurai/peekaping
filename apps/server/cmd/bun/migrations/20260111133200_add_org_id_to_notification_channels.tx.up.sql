ALTER TABLE notification_channels ADD COLUMN org_id VARCHAR(255);
ALTER TABLE notification_channels ADD CONSTRAINT fk_notification_channels_org_id 
  FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
