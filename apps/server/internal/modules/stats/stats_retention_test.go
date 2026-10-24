package stats_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// bucketStarts returns the start of every stat row the monitor has for period.
func bucketStarts(t *testing.T, svc stats.Service, monitorID string, period stats.StatPeriod, now time.Time) []time.Time {
	t.Helper()
	rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, "", now.AddDate(-5, 0, 0), now, period)
	require.NoError(t, err)
	starts := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		starts = append(starts, row.Timestamp.UTC())
	}
	return starts
}

func TestDeleteExpiredKeepsEachPeriodForItsRetention(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		now := time.Date(2026, 10, 18, 15, 30, 20, 0, time.UTC)
		checks := []time.Time{
			now.Add(-48 * time.Hour),               // minute bucket 15:30 on Oct 16: the oldest one kept
			now.Add(-48*time.Hour - time.Minute),   // minute bucket 15:29 on Oct 16: expired
			now.AddDate(0, 0, -90),                 // hour bucket 15:00 on Jul 20: the oldest one kept
			now.AddDate(0, 0, -90).Add(-time.Hour), // hour bucket 14:00 on Jul 20: expired
			now.AddDate(-2, 0, 0),                  // day bucket Oct 18, 2024: kept forever
		}
		for _, at := range checks {
			aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, at)
		}

		deleted, err := svc.DeleteExpired(context.Background(), now)
		require.NoError(t, err)

		day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
		assert.Equal(t, []time.Time{
			time.Date(2026, 10, 16, 15, 30, 0, 0, time.UTC),
		}, bucketStarts(t, svc, monitorID, stats.StatMinutely, now))
		assert.Equal(t, []time.Time{
			time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC),
		}, bucketStarts(t, svc, monitorID, stats.StatHourly, now))
		assert.Equal(t, []time.Time{
			day(2024, 10, 18), day(2026, 7, 20), day(2026, 10, 16),
		}, bucketStarts(t, svc, monitorID, stats.StatDaily, now))
		assert.Equal(t, map[stats.StatPeriod]int64{stats.StatMinutely: 4, stats.StatHourly: 2}, deleted)
	})
}

func TestDeleteExpiredKeepsEveryStatUptimeAndBadgesRead(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		// At midnight, so each window starts on a minute, hour and day bucket, and the
		// oldest bucket it reads holds its first check.
		now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
		// The uptime % and badge windows, shortest first.
		windows := []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 90 * 24 * time.Hour, 365 * 24 * time.Hour}
		// A Down check 30 seconds into each window, and one Up check at the end.
		for _, window := range windows {
			aggregate(t, svc, monitorID, shared.MonitorStatusDown, 100, now.Add(-window+30*time.Second))
		}
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, now.Add(-30*time.Second))

		_, err := svc.DeleteExpired(context.Background(), now)
		require.NoError(t, err)

		// Each window still counts its own Down check, the shorter windows' ones and the Up check.
		for i, window := range windows {
			summary, err := svc.Summarize(context.Background(), monitorID, "", now.Add(-window), now)
			require.NoError(t, err)
			require.NotNil(t, summary.Uptime, window)
			assert.InDelta(t, 100/float64(i+2), *summary.Uptime, 1e-9, window)
		}
	})
}

// deleteCounter records how many rows each DELETE statement removed.
type deleteCounter struct{ rows []int64 }

func (*deleteCounter) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }

func (h *deleteCounter) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	if event.Operation() == "DELETE" && event.Err == nil {
		n, err := event.Result.RowsAffected()
		if err == nil {
			h.rows = append(h.rows, n)
		}
	}
}

func TestDeleteExpiredClearsABacklogLargerThanOneBatch(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrateUp(t, db, migrations.Migrations)
			monitorID := insertMonitor(t, db)

			now := time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC)
			// insertHistory writes the monitor's n latest buckets before now, newest first.
			insertHistory := func(table, monitorID string, bucket time.Duration, n int) {
				rows := make([]statRow, 0, n)
				for i := 1; i <= n; i++ {
					rows = append(rows, statRow{MonitorID: monitorID, Timestamp: now.Add(-time.Duration(i) * bucket), Up: 1})
				}
				for len(rows) > 0 {
					chunk := rows[:min(len(rows), 10_000)]
					_, err := db.NewInsert().Model(&chunk).ModelTableExpr(table).Exec(ctx)
					require.NoError(t, err)
					rows = rows[len(chunk):]
				}
			}
			// Two monitors' last 48 hours come first, so the first batch has nothing to delete.
			for range 2 {
				insertHistory("stat_minutely", insertMonitor(t, db), time.Minute, 48*60)
			}
			// 100 days of history with one row per bucket, like a monitor that ran
			// before stats had retention.
			insertHistory("stat_minutely", monitorID, time.Minute, 100*24*60)
			insertHistory("stat_hourly", monitorID, time.Hour, 100*24)
			// A hole of 20,000 expired rows, wider than a batch, like the gap a deleted monitor leaves.
			_, err := db.ExecContext(ctx, "DELETE FROM stat_minutely WHERE timestamp >= ? AND timestamp < ?", now.Add(-70_000*time.Minute), now.Add(-50_000*time.Minute))
			require.NoError(t, err)

			deletes := &deleteCounter{}
			db.AddQueryHook(deletes)
			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			deleted, err := svc.DeleteExpired(ctx, now)
			require.NoError(t, err)

			assert.Equal(t, map[stats.StatPeriod]int64{stats.StatMinutely: 121_120, stats.StatHourly: 240}, deleted)
			assert.Len(t, bucketStarts(t, svc, monitorID, stats.StatMinutely, now), 48*60)
			assert.Len(t, bucketStarts(t, svc, monitorID, stats.StatHourly, now), 90*24)
			var total int64
			for _, n := range deletes.rows {
				assert.LessOrEqual(t, n, int64(5000), "rows deleted by one statement")
				total += n
			}
			assert.Equal(t, int64(121_120+240), total)
		})
	}
}
