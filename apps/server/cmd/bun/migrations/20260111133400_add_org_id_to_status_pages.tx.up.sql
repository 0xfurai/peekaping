ALTER TABLE status_pages ADD COLUMN org_id VARCHAR(255);
ALTER TABLE status_pages ADD CONSTRAINT fk_status_pages_org_id 
  FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
