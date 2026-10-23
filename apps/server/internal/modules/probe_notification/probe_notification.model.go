package probe_notification

import (
	"errors"
	"time"
)

var (
	// ErrUnknownChannel is returned, wrapped with the id, when a probe is given a notification
	// channel that doesn't exist
	ErrUnknownChannel = errors.New("notification channel not found")
	// ErrUnsupportedStorage is returned on a MongoDB install, which has no probes (ADR 0001)
	ErrUnsupportedStorage = errors.New("probes need SQLite or PostgreSQL storage")
)

// Model links a probe to a notification channel that hears about it
type Model struct {
	ProbeID        string
	NotificationID string
	CreatedAt      time.Time
}
