package stats

import (
	"context"
	"time"
)

type Repository interface {
	// AddToStat adds stat's counts and response times to the stat for its monitor
	// and timestamp, and creates that stat if it doesn't exist, in one atomic write.
	AddToStat(ctx context.Context, stat *Stat, period StatPeriod) error
	// FindStatsByMonitorIDAndTimeRange returns a monitor's stats for the period's buckets from
	// since to until, oldest first, one stat per bucket. With a probeID each stat counts that
	// probe's checks; with none it adds up every probe's.
	FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period StatPeriod) ([]*Stat, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	// DeleteOlderThan deletes the period's stats whose bucket started before cutoff
	// and returns how many it deleted.
	DeleteOlderThan(ctx context.Context, period StatPeriod, cutoff time.Time) (int64, error)
}
