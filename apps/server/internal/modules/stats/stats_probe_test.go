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
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// addProbeIDToStatsMigration is the migration that added probe_id to the stats tables.
const addProbeIDToStatsMigration = "20261024120200"

func aggregateFrom(t *testing.T, svc stats.Service, monitorID, probeID string, status shared.MonitorStatus, ping int, at time.Time) {
	t.Helper()
	require.NoError(t, svc.AggregateHeartbeat(context.Background(), &stats.HeartbeatPayload{MonitorID: monitorID, ProbeID: probeID, Status: int(status), Ping: ping, Time: at.Unix()}))
}

// probeStatRow is a row of a stat table with the probe it counts.
type probeStatRow struct {
	ProbeID   string  `bun:"probe_id"`
	Up        int     `bun:"up"`
	Down      int     `bun:"down"`
	PingSum   float64 `bun:"ping_sum"`
	PingCount int     `bun:"ping_count"`
}

func probeStatRows(t *testing.T, db *bun.DB, table string) []probeStatRow {
	t.Helper()
	var rows []probeStatRow
	require.NoError(t, db.NewRaw("SELECT probe_id, up, down, ping_sum, ping_count FROM "+table+" ORDER BY monitor_id, timestamp, probe_id").Scan(context.Background(), &rows))
	return rows
}

// A monitor's overall uptime and response time count every probe's checks.
func TestStatsOfEveryProbeAddUpInOneBucket(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		aggregateFrom(t, svc, monitorID, "local", shared.MonitorStatusUp, 100, minute.Add(10*time.Second))
		aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusUp, 300, minute.Add(20*time.Second))
		aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusDown, 0, minute.Add(30*time.Second))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 200, minute.Add(40*time.Second)) // no probe: the local probe

		want := bucket{Up: 3, Down: 1, Ping: 200, PingMin: 100, PingMax: 300}
		assert.Equal(t, want, findBucket(t, svc, monitorID, minute, stats.StatMinutely))
		assert.Equal(t, want, findBucket(t, svc, monitorID, minute, stats.StatHourly))
		assert.Equal(t, want, findBucket(t, svc, monitorID, minute.Truncate(24*time.Hour), stats.StatDaily))

		summary, err := svc.Summarize(context.Background(), monitorID, "", minute, minute.Add(time.Minute))
		require.NoError(t, err)
		require.NotNil(t, summary.Uptime)
		assert.Equal(t, 75.0, *summary.Uptime)
		require.NotNil(t, summary.AvgPing)
		assert.Equal(t, 200.0, *summary.AvgPing)
	})
}

func TestSQLRepository_KeepsAStatRowPerProbe(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			db := backend.open(t)
			migrateUp(t, db, migrations.Migrations)
			monitorID := insertMonitor(t, db)
			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

			aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusUp, 300, minute)
			aggregateFrom(t, svc, monitorID, "local", shared.MonitorStatusUp, 100, minute.Add(time.Second))
			aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusDown, 0, minute.Add(2*time.Second))
			aggregate(t, svc, monitorID, shared.MonitorStatusUp, 50, minute.Add(3*time.Second))

			want := []probeStatRow{
				{ProbeID: "local", Up: 2, PingSum: 150, PingCount: 2},
				{ProbeID: "probe-frankfurt", Up: 1, Down: 1, PingSum: 300, PingCount: 1},
			}
			for _, table := range []string{"stat_minutely", "stat_hourly", "stat_daily"} {
				assert.Equal(t, want, probeStatRows(t, db, table), table)
			}
		})
	}
}

// insertStatRow writes a stat row as the schema before the probe column has it.
func insertStatRow(t *testing.T, db *bun.DB, table, monitorID string, at time.Time, up, down int, pingSum float64, pingCount int, pingMin, pingMax *float64) {
	t.Helper()
	_, err := db.Exec("INSERT INTO "+table+" (monitor_id, timestamp, up, down, ping_sum, ping_count, ping_min, ping_max) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		monitorID, at, up, down, pingSum, pingCount, pingMin, pingMax)
	require.NoError(t, err)
}

// A stat counted before the upgrade counts checks the local probe ran: nothing else ran checks.
func TestMigrationsGiveExistingStatsToTheLocalProbe(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			db := backend.open(t)
			migrateUp(t, db, migrationsBefore(addProbeIDToStatsMigration))
			monitorID, quietID := insertMonitor(t, db), insertMonitor(t, db)
			day := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
			low, high := 100.0, 300.0
			for _, table := range []string{"stat_minutely", "stat_hourly", "stat_daily"} {
				insertStatRow(t, db, table, monitorID, day, 3, 1, 600, 2, &low, &high)
				insertStatRow(t, db, table, quietID, day, 0, 2, 0, 0, nil, nil)
			}

			migrateUp(t, db, migrations.Migrations)

			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			for _, period := range []stats.StatPeriod{stats.StatMinutely, stats.StatHourly, stats.StatDaily} {
				assert.Equal(t, bucket{Up: 3, Down: 1, Ping: 300, PingMin: 100, PingMax: 300}, findBucket(t, svc, monitorID, day, period), period)
				assert.Equal(t, bucket{Down: 2}, findBucket(t, svc, quietID, day, period), period)
			}
			for _, table := range []string{"stat_minutely", "stat_hourly", "stat_daily"} {
				rows := probeStatRows(t, db, table)
				require.Len(t, rows, 2, table)
				assert.Equal(t, "local", rows[0].ProbeID, table)
				assert.Equal(t, "local", rows[1].ProbeID, table)
			}

			// The local probe's next check lands in the stat that was there
			aggregate(t, svc, monitorID, shared.MonitorStatusUp, 500, day.Add(time.Hour))
			assert.Equal(t, bucket{Up: 4, Down: 1, Ping: 366.6666666666667, PingMin: 100, PingMax: 500}, findBucket(t, svc, monitorID, day, stats.StatDaily))
			assert.Len(t, probeStatRows(t, db, "stat_daily"), 2)
		})
	}
}

// oldStatRow is a row of the schema before the probe column.
type oldStatRow struct {
	Timestamp time.Time `bun:"timestamp"`
	Up        int       `bun:"up"`
	Down      int       `bun:"down"`
	PingSum   float64   `bun:"ping_sum"`
	PingCount int       `bun:"ping_count"`
	PingMin   *float64  `bun:"ping_min"`
	PingMax   *float64  `bun:"ping_max"`
}

// Rolling the migration back adds the probes' stats up into the one row per bucket the old
// schema holds.
func TestRollbackAddsTheProbesStatsUpAgain(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrateUp(t, db, migrationsBefore(addProbeIDToStatsMigration))
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			monitorID := insertMonitor(t, db)
			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
			aggregateFrom(t, svc, monitorID, "local", shared.MonitorStatusUp, 100, minute)
			aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusUp, 300, minute)
			aggregateFrom(t, svc, monitorID, "probe-frankfurt", shared.MonitorStatusDown, 0, minute)
			aggregateFrom(t, svc, monitorID, "probe-oslo", shared.MonitorStatusDown, 0, minute.Add(time.Minute))

			_, err = migrator.Rollback(ctx)
			require.NoError(t, err)

			var rows []oldStatRow
			require.NoError(t, db.NewRaw("SELECT timestamp, up, down, ping_sum, ping_count, ping_min, ping_max FROM stat_minutely ORDER BY timestamp").Scan(ctx, &rows))
			require.Len(t, rows, 2)
			assert.True(t, rows[0].Timestamp.Equal(minute), rows[0].Timestamp)
			assert.True(t, rows[1].Timestamp.Equal(minute.Add(time.Minute)), rows[1].Timestamp)
			rows[0].Timestamp, rows[1].Timestamp = time.Time{}, time.Time{}
			low, high := 100.0, 300.0
			assert.Equal(t, []oldStatRow{
				{Up: 2, Down: 1, PingSum: 400, PingCount: 2, PingMin: &low, PingMax: &high},
				{Down: 1},
			}, rows)
			_, err = db.Exec("SELECT probe_id FROM stat_minutely")
			assert.Error(t, err, "the old schema has no probe_id")
		})
	}
}
