package stats_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// sqliteDB opens an empty SQLite database in a temp dir.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "stats.db"))
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "stats_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]interface{}{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return db
}

func migrateUp(t *testing.T, db *bun.DB, ms *migrate.Migrations) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, ms)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
}

func insertMonitor(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
	require.NoError(t, err)
	return id
}

// forEachBackend runs fn against a stats service on every database backend,
// with one monitor to aggregate heartbeats for.
func forEachBackend(t *testing.T, fn func(t *testing.T, svc stats.Service, monitorID string)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			migrateUp(t, db, migrations.Migrations)
			fn(t, stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar()), insertMonitor(t, db))
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
	t.Run("Mongo", func(t *testing.T) {
		uri := os.Getenv("PEE_TEST_MONGO_URI")
		if uri == "" {
			t.Skip("set PEE_TEST_MONGO_URI to run MongoDB integration checks")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		require.NoError(t, err)
		require.NoError(t, client.Ping(ctx, nil))
		cfg := &config.Config{DBName: "stats_" + primitive.NewObjectID().Hex()}
		t.Cleanup(func() {
			require.NoError(t, client.Database(cfg.DBName).Drop(context.Background()))
			require.NoError(t, client.Disconnect(context.Background()))
		})
		fn(t, stats.NewService(stats.NewMongoRepository(client, cfg), zap.NewNop().Sugar()), primitive.NewObjectID().Hex())
	})
}

// bucket is the part of a stat row a chart shows.
type bucket struct {
	Up, Down, Maintenance  int
	Ping, PingMin, PingMax float64
}

// findBucket returns the stat row that starts at start, which must be the only one there.
func findBucket(t *testing.T, svc stats.Service, monitorID string, start time.Time, period stats.StatPeriod) bucket {
	t.Helper()
	rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, start, start, period)
	require.NoError(t, err)
	require.Len(t, rows, 1, "%s rows at %s", period, start)
	s := rows[0]
	return bucket{s.Up, s.Down, s.Maintenance, s.Ping, s.PingMin, s.PingMax}
}

func aggregate(t *testing.T, svc stats.Service, monitorID string, status shared.MonitorStatus, ping int, at time.Time) {
	t.Helper()
	require.NoError(t, svc.AggregateHeartbeat(context.Background(), &stats.HeartbeatPayload{MonitorID: monitorID, Status: int(status), Ping: ping, Time: at.Unix()}))
}

func TestMinuteHourAndDayBucketsHoldTheirOwnChecks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		day := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
		at := func(h, m, s int) time.Time {
			return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
		}
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, at(14, 0, 30))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 300, at(14, 1, 30))
		aggregate(t, svc, monitorID, shared.MonitorStatusDown, 0, at(14, 30, 0))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 50, at(9, 15, 0))

		assert.Equal(t, bucket{Up: 1, Ping: 100, PingMin: 100, PingMax: 100}, findBucket(t, svc, monitorID, at(14, 0, 0), stats.StatMinutely))
		assert.Equal(t, bucket{Up: 2, Down: 1, Ping: 200, PingMin: 100, PingMax: 300}, findBucket(t, svc, monitorID, at(14, 0, 0), stats.StatHourly))
		assert.Equal(t, bucket{Up: 3, Down: 1, Ping: 150, PingMin: 50, PingMax: 300}, findBucket(t, svc, monitorID, day, stats.StatDaily))
	})
}

func TestResponseTimesCountOnlyUpChecks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		aggregate(t, svc, monitorID, shared.MonitorStatusMaintenance, 0, minute)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, minute.Add(10*time.Second))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 200, minute.Add(20*time.Second))
		aggregate(t, svc, monitorID, shared.MonitorStatusPending, 0, minute.Add(30*time.Second))
		assert.Equal(t, bucket{Up: 3, Down: 1, Maintenance: 1, Ping: 150, PingMin: 100, PingMax: 200}, findBucket(t, svc, monitorID, minute, stats.StatMinutely))

		zeroFirst := minute.Add(time.Minute)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 0, zeroFirst)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 50, zeroFirst.Add(10*time.Second))
		assert.Equal(t, bucket{Up: 2, Ping: 25, PingMin: 0, PingMax: 50}, findBucket(t, svc, monitorID, zeroFirst, stats.StatMinutely))

		downOnly := minute.Add(2 * time.Minute)
		aggregate(t, svc, monitorID, shared.MonitorStatusDown, 0, downOnly)
		assert.Equal(t, bucket{Down: 1}, findBucket(t, svc, monitorID, downOnly, stats.StatMinutely))
	})
}

func TestDeleteByMonitorIDClearsEveryPeriod(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		ctx := context.Background()
		at := time.Date(2026, 10, 16, 14, 0, 30, 0, time.UTC)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, at)
		require.NoError(t, svc.DeleteByMonitorID(ctx, monitorID))
		for _, period := range []stats.StatPeriod{stats.StatMinutely, stats.StatHourly, stats.StatDaily} {
			rows, err := svc.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, at.Add(-24*time.Hour), at, period)
			require.NoError(t, err)
			assert.Empty(t, rows, period)
		}
	})
}

func TestSummaryAveragesResponseTimesOverUpChecks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		aggregate(t, svc, monitorID, shared.MonitorStatusMaintenance, 0, minute)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, minute.Add(10*time.Second))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 200, minute.Add(time.Minute))
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 300, minute.Add(time.Minute+10*time.Second))

		rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, minute, minute.Add(time.Minute), stats.StatMinutely)
		require.NoError(t, err)
		summary := svc.StatPointsSummary(rows)
		require.NotNil(t, summary.AvgPing)
		assert.Equal(t, 200.0, *summary.AvgPing)

		grouped, err := svc.FindStatsByMonitorIDAndTimeRangeWithInterval(context.Background(), monitorID, minute, minute.Add(2*time.Minute), stats.StatMinutely, 120)
		require.NoError(t, err)
		require.Len(t, grouped, 1)
		assert.Equal(t, 200.0, grouped[0].Ping)
	})
}

func TestSummaryKeepsA0msMinimumResponseTime(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, minute)
		aggregate(t, svc, monitorID, shared.MonitorStatusUp, 0, minute.Add(time.Minute))

		rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, minute, minute.Add(time.Minute), stats.StatMinutely)
		require.NoError(t, err)
		summary := svc.StatPointsSummary(rows)
		require.NotNil(t, summary.MinPing)
		assert.Equal(t, 0.0, *summary.MinPing)

		grouped, err := svc.FindStatsByMonitorIDAndTimeRangeWithInterval(context.Background(), monitorID, minute, minute.Add(2*time.Minute), stats.StatMinutely, 120)
		require.NoError(t, err)
		require.Len(t, grouped, 1)
		assert.Equal(t, 0.0, grouped[0].PingMin)
	})
}

func TestSummarizeCountsDownHeartbeats20HoursAgoIn24HourUptime(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		// Off the hour, so reading hour buckets would drop the checks from 10:30 to 11:00.
		now := time.Date(2026, 10, 17, 10, 30, 0, 0, time.UTC)
		downSpans := [][2]time.Time{
			{now.Add(-28 * time.Hour), now.Add(-26 * time.Hour)}, // before the window
			{now.Add(-20 * time.Hour), now.Add(-18 * time.Hour)},
		}
		// A check every 10 minutes over the last 30 hours.
		for k := range 180 {
			at := now.Add(-30*time.Hour + time.Duration(k)*10*time.Minute + 30*time.Second)
			status := shared.MonitorStatusUp
			for _, span := range downSpans {
				if !at.Before(span[0]) && at.Before(span[1]) {
					status = shared.MonitorStatusDown
				}
			}
			aggregate(t, svc, monitorID, status, 100, at)
		}

		summary, err := svc.Summarize(context.Background(), monitorID, now.Add(-24*time.Hour), now)
		require.NoError(t, err)
		require.NotNil(t, summary.Uptime)
		assert.InDelta(t, 100*22.0/24, *summary.Uptime, 1e-9)
	})
}
