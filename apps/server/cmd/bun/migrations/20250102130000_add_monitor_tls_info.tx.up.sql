-- Create monitor_tls_info table
CREATE TABLE monitor_tls_info (
    id SERIAL PRIMARY KEY,
    monitor_id VARCHAR(255) NOT NULL UNIQUE,
    info_json TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create index for efficient lookups
CREATE INDEX idx_monitor_tls_info_monitor_id ON monitor_tls_info(monitor_id);
CREATE INDEX idx_monitor_tls_info_updated_at ON monitor_tls_info(updated_at);
