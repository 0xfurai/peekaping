package monitor_notification

import (
	"errors"
	"time"
)

// ErrUnknownChannel is returned, wrapped with the id, when a monitor is given a notification
// channel that isn't in its workspace: it doesn't exist, or it belongs to another workspace, and
// the caller can't tell which (docs/adr/0005 section 8)
var ErrUnknownChannel = errors.New("notification channel not found")

type Model struct {
	ID             string    `json:"id"`
	MonitorID      string    `json:"monitor_id"`
	NotificationID string    `json:"notification_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
