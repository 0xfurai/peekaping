package notification_sent_history

import (
	"time"
)

type Model struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "certificate", "monitor", etc.
	MonitorID string    `json:"monitor_id"`
	Days      int       `json:"days"` // Threshold days (e.g., 7, 14, 21)
	CreatedAt time.Time `json:"created_at"`
}
