package stats

import (
	"context"
	"time"
)

type Repository interface {
	GetOrCreateStat(ctx context.Context, monitorID string, timestamp time.Time, period StatPeriod) (*Stat, error)
	UpsertStat(ctx context.Context, stat *Stat, period StatPeriod) error
	FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period StatPeriod) ([]*Stat, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
}
