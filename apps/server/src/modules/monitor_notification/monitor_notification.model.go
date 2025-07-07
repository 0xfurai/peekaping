package monitor_notification

import "time"

type Model struct {
	ID             string    `json:"id"`
	MonitorID      string    `json:"monitor_id"`
	NotificationID string    `json:"notification_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
