package heartbeat

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, heartbeat *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int) ([]*Model, error)
	FindActive(ctx context.Context) ([]*Model, error)
	Delete(ctx context.Context, id string) error

	FindByMonitorIDPaginated(
		ctx context.Context,
		monitorID string,
		limit int,
		page int,
		important *bool,
		reverse bool,
	) ([]*Model, error)
	FindUptimeStatsByMonitorID(
		ctx context.Context,
		monitorID string,
		periods map[string]time.Duration,
		now time.Time,
	) (map[string]float64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
}
