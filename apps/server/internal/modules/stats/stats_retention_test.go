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
	rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, now.AddDate(-5, 0, 0), now, period)
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
			now.Add(-48 * time.Hour),                // minute bucket 15:30 on Oct 16: the oldest one kept
			now.Add(-48*time.Hour - time.Minute),    // minute bucket 15:29 on Oct 16: expired
			now.AddDate(0, 0, -90),                  // hour bucket 15:00 on Jul 20: the oldest one kept
			now.AddDate(0, 0, -90).Add(-time.Hour),  // hour bucket 14:00 on Jul 20: expired
			now.AddDate(-2, 0, 0),                   // day bucket Oct 18, 2024: kept forever
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

type statRow struct {
	MonitorID string    `bun:"monitor_id"`
	Timestamp time.Time `bun:"timestamp"`
	Up        int       `bun:"up"`
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

			// 100 days of history with one row per bucket, like a monitor that ran
			// before stats had retention.
			now := time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC)
			insertHistory := func(table string, bucket time.Duration, n int) {
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
			insertHistory("stat_minutely", time.Minute, 100*24*60)
			insertHistory("stat_hourly", time.Hour, 100*24)

			deletes := &deleteCounter{}
			db.AddQueryHook(deletes)
			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			deleted, err := svc.DeleteExpired(ctx, now)
			require.NoError(t, err)

			assert.Equal(t, map[stats.StatPeriod]int64{stats.StatMinutely: 141_120, stats.StatHourly: 240}, deleted)
			assert.Len(t, bucketStarts(t, svc, monitorID, stats.StatMinutely, now), 48*60)
			assert.Len(t, bucketStarts(t, svc, monitorID, stats.StatHourly, now), 90*24)
			var total int64
			for _, n := range deletes.rows {
				assert.LessOrEqual(t, n, int64(5000), "rows deleted by one statement")
				total += n
			}
			assert.Equal(t, int64(141_120+240), total)
		})
	}
}
