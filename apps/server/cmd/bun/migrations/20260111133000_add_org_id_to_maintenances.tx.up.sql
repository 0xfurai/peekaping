ALTER TABLE maintenances ADD COLUMN org_id VARCHAR(255);
ALTER TABLE maintenances ADD CONSTRAINT fk_maintenances_org_id 
  FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
