package notification_sent_history

import (
	"context"
)

type Repository interface {
	// CheckIfSent checks if a notification was already sent for the given criteria
	CheckIfSent(ctx context.Context, notificationType string, monitorID string, days int) (bool, error)

	// RecordSent records that a notification was sent
	RecordSent(ctx context.Context, dto *CreateDto) error

	// ClearByMonitorAndType clears notification history for a specific monitor and type
	ClearByMonitorAndType(ctx context.Context, monitorID string, notificationType string) error

	// CleanupOldRecords removes old notification records (older than specified days)
	CleanupOldRecords(ctx context.Context, olderThanDays int) error

	// GetByMonitorAndType gets all notification history for a monitor and type
	GetByMonitorAndType(ctx context.Context, monitorID string, notificationType string) ([]*Model, error)
}
