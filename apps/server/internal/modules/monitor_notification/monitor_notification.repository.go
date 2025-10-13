package monitor_notification

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, model *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	Delete(ctx context.Context, id string) error
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}
