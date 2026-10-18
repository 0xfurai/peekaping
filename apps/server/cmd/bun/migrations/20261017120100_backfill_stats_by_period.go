package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Rebuilds stat_minutely, stat_hourly and stat_daily from heartbeats, then drops
// the old stats table, whose rows mixed all three time scales.
//
// It counts the way stats.ServiceImpl.AggregateHeartbeat did when it was written:
// up counts status 1 (up) and 3 (maintenance), down counts 0 (down) and 2 (pending),
// maintenance counts 3, and only status 1 checks add to the ping columns.
// The work is done in Go rather than SQL so that SQLite and Postgres bucket
// timestamps the same way.
//
// Each monitor is rebuilt in its own transaction, so locks stay short, and rows
// are upserted, so a rerun after a failure is safe. A bucket the new code wrote
// while the backfill ran is overwritten with the value rebuilt from heartbeats.

func init() {
	Migrations.MustRegister(backfillStatsByPeriod, recreateEmptyStatsTable)
}

// backfillBatchSize is how many heartbeats are read per query.
const backfillBatchSize = 1000

var backfillPeriods = []struct {
	table string
	size  time.Duration
}{
	{"stat_minutely", time.Minute},
	{"stat_hourly", time.Hour},
	{"stat_daily", 24 * time.Hour},
}

type backfillHeartbeat struct {
	bun.BaseModel `bun:"table:heartbeats,alias:h"`

	ID     string    `bun:"id"`
	Status int       `bun:"status"`
	Ping   int       `bun:"ping"`
	Time   time.Time `bun:"time"`
}

type backfillStat struct {
	bun.BaseModel `bun:"alias:s"`

	MonitorID   string    `bun:"monitor_id"`
	Timestamp   time.Time `bun:"timestamp"`
	Up          int       `bun:"up"`
	Down        int       `bun:"down"`
	Maintenance int       `bun:"maintenance"`
	PingSum     float64   `bun:"ping_sum"`
	PingCount   int       `bun:"ping_count"`
	PingMin     *float64  `bun:"ping_min"`
	PingMax     *float64  `bun:"ping_max"`
}

func (s *backfillStat) add(hb *backfillHeartbeat) {
	switch hb.Status {
	case 1, 3:
		s.Up++
	case 0, 2:
		s.Down++
	}
	if hb.Status == 3 {
		s.Maintenance++
	}
	if hb.Status == 1 {
		ping := float64(hb.Ping)
		if s.PingCount == 0 || ping < *s.PingMin {
			s.PingMin = &ping
		}
		if s.PingCount == 0 || ping > *s.PingMax {
			s.PingMax = &ping
		}
		s.PingSum += ping
		s.PingCount++
	}
}

func backfillStatsByPeriod(ctx context.Context, db *bun.DB) error {
	var monitorIDs []string
	if err := db.NewSelect().Table("monitors").Column("id").Scan(ctx, &monitorIDs); err != nil {
		return err
	}
	for _, monitorID := range monitorIDs {
		err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			return backfillMonitorStats(ctx, tx, monitorID)
		})
		if err != nil {
			return fmt.Errorf("backfill stats for monitor %s: %w", monitorID, err)
		}
	}
	_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS stats")
	return err
}

// backfillMonitorStats reads the monitor's heartbeats in time order, so each
// bucket is complete once a heartbeat falls past it.
func backfillMonitorStats(ctx context.Context, tx bun.Tx, monitorID string) error {
	open := make([]*backfillStat, len(backfillPeriods))
	done := make([][]backfillStat, len(backfillPeriods))
	var last *backfillHeartbeat
	for {
		var batch []backfillHeartbeat
		q := tx.NewSelect().
			Model(&batch).
			Where("monitor_id = ?", monitorID).
			OrderExpr("time ASC, id ASC").
			Limit(backfillBatchSize)
		if last != nil {
			// time >= ? lets Postgres scan the (monitor_id, time) index from the last row.
			q = q.Where("time >= ? AND (time > ? OR id > ?)", last.Time, last.Time, last.ID)
		}
		if err := q.Scan(ctx); err != nil {
			return err
		}

		for i := range batch {
			hb := &batch[i]
			for p, period := range backfillPeriods {
				start := hb.Time.Truncate(period.size)
				if open[p] != nil && !open[p].Timestamp.Equal(start) {
					done[p] = append(done[p], *open[p])
					open[p] = nil
				}
				if open[p] == nil {
					open[p] = &backfillStat{MonitorID: monitorID, Timestamp: start}
				}
				open[p].add(hb)
			}
		}

		if len(batch) < backfillBatchSize {
			break
		}
		last = &batch[len(batch)-1]
		if err := insertBackfilledStats(ctx, tx, done); err != nil {
			return err
		}
	}

	for p := range backfillPeriods {
		if open[p] != nil {
			done[p] = append(done[p], *open[p])
		}
	}
	return insertBackfilledStats(ctx, tx, done)
}

// insertBackfilledStats upserts the finished buckets of each period and empties done.
func insertBackfilledStats(ctx context.Context, tx bun.Tx, done [][]backfillStat) error {
	for p, period := range backfillPeriods {
		if len(done[p]) == 0 {
			continue
		}
		q := tx.NewInsert().
			Model(&done[p]).
			ModelTableExpr(period.table + " AS s").
			On("CONFLICT (monitor_id, timestamp) DO UPDATE")
		for _, column := range []string{"up", "down", "maintenance", "ping_sum", "ping_count", "ping_min", "ping_max"} {
			q = q.Set(column + " = EXCLUDED." + column)
		}
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
		done[p] = done[p][:0]
	}
	return nil
}

// recreateEmptyStatsTable empties the per-period tables and recreates the old
// stats table without rows; the heartbeats are still there to rebuild from.
func recreateEmptyStatsTable(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, period := range backfillPeriods {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+period.table); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS stats (
    id UUID PRIMARY KEY,
    monitor_id UUID NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    ping DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_min DOUBLE PRECISION NOT NULL DEFAULT 0,
    ping_max DOUBLE PRECISION NOT NULL DEFAULT 0,
    up INTEGER NOT NULL DEFAULT 0,
    down INTEGER NOT NULL DEFAULT 0,
    maintenance INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
    UNIQUE(monitor_id, timestamp)
)`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_stats_monitor_timestamp ON stats(monitor_id, timestamp)")
		return err
	})
}
