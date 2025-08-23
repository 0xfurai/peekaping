-- Rollback: Drop UUID tables and recreate with SERIAL primary keys

-- Drop notification_sent_history table
DROP TABLE IF EXISTS notification_sent_history;

-- Recreate notification_sent_history table with SERIAL primary key (original structure)
CREATE TABLE notification_sent_history (
    id SERIAL PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    monitor_id VARCHAR(255) NOT NULL,
    days INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    -- Create unique constraint for business logic
    UNIQUE(type, monitor_id, days)
);

-- Create indexes for efficient lookups
CREATE INDEX idx_notification_sent_history_type_monitor ON notification_sent_history(type, monitor_id);
CREATE INDEX idx_notification_sent_history_created_at ON notification_sent_history(created_at);

-- Drop monitor_tls_info table
DROP TABLE IF EXISTS monitor_tls_info;

-- Recreate monitor_tls_info table with SERIAL primary key (original structure)
CREATE TABLE monitor_tls_info (
    id SERIAL PRIMARY KEY,
    monitor_id VARCHAR(255) NOT NULL UNIQUE,
    info_json TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for efficient lookups
CREATE INDEX idx_monitor_tls_info_monitor_id ON monitor_tls_info(monitor_id);
CREATE INDEX idx_monitor_tls_info_updated_at ON monitor_tls_info(updated_at);
