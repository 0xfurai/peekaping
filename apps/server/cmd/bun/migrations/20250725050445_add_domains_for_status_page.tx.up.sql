CREATE TABLE IF NOT EXISTS domain_status_page (
    id UUID PRIMARY KEY,
    status_page_id UUID NOT NULL,
    domain TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (status_page_id) REFERENCES status_pages(id) ON DELETE CASCADE,
    UNIQUE(status_page_id, domain)
);
