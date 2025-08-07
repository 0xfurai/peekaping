-- Create notification_sent_history table
CREATE TABLE notification_sent_history (
    id SERIAL PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    monitor_id VARCHAR(255) NOT NULL,
    days INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    -- Create index for efficient lookups
    UNIQUE(type, monitor_id, days)
);

-- Create index for cleanup operations
CREATE INDEX idx_notification_sent_history_type_monitor ON notification_sent_history(type, monitor_id);
CREATE INDEX idx_notification_sent_history_created_at ON notification_sent_history(created_at);
