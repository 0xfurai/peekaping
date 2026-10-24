package stats_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// firstPerPeriodMigration is the migration that split the stats table by period.
const firstPerPeriodMigration = "20261017120000"

// migrationsBefore returns the migrations an install had before name.
func migrationsBefore(name string) *migrate.Migrations {
	ms := migrate.NewMigrations()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name < name {
			ms.Add(m)
		}
	}
	return ms
}

func backfillMigration(t *testing.T) migrate.Migration {
	t.Helper()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Comment == "backfill_stats_by_period" {
			return m
		}
	}
	t.Fatal("backfill migration not registered")
	return migrate.Migration{}
}

type heartbeatRow struct {
	bun.BaseModel `bun:"table:heartbeats"`

	ID        string    `bun:"id,pk"`
	MonitorID string    `bun:"monitor_id"`
	Status    int       `bun:"status"`
	Ping      *int      `bun:"ping"`
	Time      time.Time `bun:"time"`
}

func TestUpgradeRebuildsStatsFromHeartbeats(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrateUp(t, db, migrationsBefore(firstPerPeriodMigration))
			monitorID, otherID, tiedID := insertMonitor(t, db), insertMonitor(t, db), insertMonitor(t, db)

			// 3,000 checks 30s apart from 23:00 on Oct 15 to 23:59:30 on Oct 16, more than
			// one backfill batch. Every 10 checks (5 minutes): down with no response time,
			// maintenance, pending, then up with response times 30, 40, ... 90 ms.
			start := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
			rows := make([]heartbeatRow, 0, 4501)
			for i := range 3000 {
				row := heartbeatRow{ID: uuid.NewString(), MonitorID: monitorID, Time: start.Add(time.Duration(i) * 30 * time.Second)}
				switch n := i % 10; n {
				case 0:
					row.Status = int(shared.MonitorStatusDown)
				case 1:
					row.Status, row.Ping = int(shared.MonitorStatusMaintenance), new(int)
				case 2:
					row.Status, row.Ping = int(shared.MonitorStatusPending), new(int)
				default:
					ping := n * 10
					row.Status, row.Ping = int(shared.MonitorStatusUp), &ping
				}
				rows = append(rows, row)
			}
			ping := 7
			hour := time.Date(2026, 10, 16, 5, 0, 0, 0, time.UTC)
			rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: otherID, Status: int(shared.MonitorStatusUp), Ping: &ping, Time: hour.Add(10 * time.Second)})
			// 1,500 checks at the same instant, so a backfill batch ends among equal times.
			for range 1500 {
				rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: tiedID, Status: int(shared.MonitorStatusUp), Ping: &ping, Time: hour})
			}
			_, err := db.NewInsert().Model(&rows).Exec(ctx)
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO stats (id, monitor_id, timestamp, up) VALUES (?, ?, ?, 5)", uuid.NewString(), monitorID, start)
			require.NoError(t, err)

			migrateUp(t, db, migrations.Migrations)

			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			assert.Equal(t, bucket{Up: 1, Down: 1, Maintenance: 1}, findBucket(t, svc, monitorID, start, stats.StatMinutely))
			assert.Equal(t, bucket{Up: 1, Down: 1, Ping: 30, PingMin: 30, PingMax: 30}, findBucket(t, svc, monitorID, start.Add(time.Minute), stats.StatMinutely))
			assert.Equal(t, bucket{Up: 2, Ping: 45, PingMin: 40, PingMax: 50}, findBucket(t, svc, monitorID, start.Add(2*time.Minute), stats.StatMinutely))
			assert.Equal(t, bucket{Up: 96, Down: 24, Maintenance: 12, Ping: 60, PingMin: 30, PingMax: 90}, findBucket(t, svc, monitorID, hour, stats.StatHourly))
			assert.Equal(t, bucket{Up: 96, Down: 24, Maintenance: 12, Ping: 60, PingMin: 30, PingMax: 90}, findBucket(t, svc, monitorID, start.Truncate(24*time.Hour), stats.StatDaily))
			assert.Equal(t, bucket{Up: 2304, Down: 576, Maintenance: 288, Ping: 60, PingMin: 30, PingMax: 90}, findBucket(t, svc, monitorID, hour.Truncate(24*time.Hour), stats.StatDaily))
			assert.Equal(t, bucket{Up: 1, Ping: 7, PingMin: 7, PingMax: 7}, findBucket(t, svc, otherID, hour, stats.StatHourly))
			assert.Equal(t, bucket{Up: 1500, Ping: 7, PingMin: 7, PingMax: 7}, findBucket(t, svc, tiedID, hour, stats.StatMinutely))

			end := start.Add(25 * time.Hour)
			for period, want := range map[stats.StatPeriod]int{stats.StatMinutely: 1500, stats.StatHourly: 25, stats.StatDaily: 2} {
				all, err := svc.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, "", start.Truncate(24*time.Hour), end, period)
				require.NoError(t, err)
				assert.Len(t, all, want, period)
			}

			_, err = db.Exec("SELECT 1 FROM stats")
			assert.Error(t, err, "the old stats table should be dropped")
		})
	}
}

// afterBackfillMigration is the first migration after the one that rebuilds the stats.
const afterBackfillMigration = "20261018120000"

// A rerun, as after a migrate that failed or was killed, succeeds and leaves the stats matching
// the heartbeats again. It runs on the schema the backfill ran on: later migrations change the
// stats tables' keys.
func TestUpgradeBackfillRerunsAfterAFailedMigrate(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrateUp(t, db, migrationsBefore(firstPerPeriodMigration))
			monitorID := insertMonitor(t, db)
			hour := time.Date(2026, 10, 16, 5, 0, 0, 0, time.UTC)
			ping30, ping50 := 30, 50
			rows := []heartbeatRow{
				{ID: uuid.NewString(), MonitorID: monitorID, Status: int(shared.MonitorStatusDown), Time: hour.Add(10 * time.Second)},
				{ID: uuid.NewString(), MonitorID: monitorID, Status: int(shared.MonitorStatusUp), Ping: &ping30, Time: hour.Add(70 * time.Second)},
				{ID: uuid.NewString(), MonitorID: monitorID, Status: int(shared.MonitorStatusUp), Ping: &ping50, Time: hour.Add(100 * time.Second)},
			}
			_, err := db.NewInsert().Model(&rows).Exec(ctx)
			require.NoError(t, err)
			migrateUp(t, db, migrationsBefore(afterBackfillMigration))

			type hourStat struct {
				Up        int      `bun:"up"`
				Down      int      `bun:"down"`
				PingSum   float64  `bun:"ping_sum"`
				PingCount int      `bun:"ping_count"`
				PingMin   *float64 `bun:"ping_min"`
				PingMax   *float64 `bun:"ping_max"`
			}
			readHour := func() hourStat {
				var got hourStat
				require.NoError(t, db.NewRaw("SELECT up, down, ping_sum, ping_count, ping_min, ping_max FROM stat_hourly WHERE monitor_id = ?", monitorID).Scan(ctx, &got))
				return got
			}
			low, high := 30.0, 50.0
			want := hourStat{Up: 2, Down: 1, PingSum: 80, PingCount: 2, PingMin: &low, PingMax: &high}
			require.Equal(t, want, readHour())

			// A check counted while the backfill ran
			_, err = db.Exec("UPDATE stat_hourly SET down = down + 1 WHERE monitor_id = ?", monitorID)
			require.NoError(t, err)
			require.NoError(t, backfillMigration(t).Up(ctx, db, nil))
			assert.Equal(t, want, readHour())
		})
	}
}
