package probe_notification

import (
	"errors"
	"time"
)

var (
	// ErrUnknownChannel is returned, wrapped with the id, when a probe is given a notification
	// channel that doesn't exist
	ErrUnknownChannel = errors.New("notification channel not found")
)

// Model links a probe to a notification channel that hears about it
type Model struct {
	ProbeID        string
	NotificationID string
	CreatedAt      time.Time
}
