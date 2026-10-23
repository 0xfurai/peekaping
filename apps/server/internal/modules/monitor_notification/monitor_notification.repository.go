package monitor_notification

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, model *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	// FindByMonitorIDs returns the links of all the monitors by monitor ID, each list ordered as
	// FindByMonitorID orders it. A monitor with no links has no entry.
	FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error)
	Delete(ctx context.Context, id string) error
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}
