ALTER TABLE monitors ADD COLUMN org_id UUID;
ALTER TABLE monitors ADD CONSTRAINT fk_monitors_organizations FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
