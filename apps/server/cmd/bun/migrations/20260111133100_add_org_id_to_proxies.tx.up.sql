ALTER TABLE proxies ADD COLUMN org_id VARCHAR(255);
ALTER TABLE proxies ADD CONSTRAINT fk_proxies_org_id 
  FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
