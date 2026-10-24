package stats_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// sqliteDB opens an empty SQLite database in a temp dir, with the settings the server uses.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "stats.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
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
	forEachBackendCounting(t, func(t *testing.T, svc stats.Service, monitorID string, _ *atomic.Int64) {
		fn(t, svc, monitorID)
	})
}

// statementCounter counts the SQL statements a bun.DB runs.
type statementCounter struct{ n *atomic.Int64 }

func (c statementCounter) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (c statementCounter) AfterQuery(context.Context, *bun.QueryEvent) { c.n.Add(1) }

// forEachBackendCounting is forEachBackend that also counts the statements, or
// Mongo commands, the service sends to the database once fn starts.
func forEachBackendCounting(t *testing.T, fn func(t *testing.T, svc stats.Service, monitorID string, statements *atomic.Int64)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			migrateUp(t, db, migrations.Migrations)
			monitorID := insertMonitor(t, db)
			statements := new(atomic.Int64)
			db.AddQueryHook(statementCounter{statements})
			fn(t, stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar()), monitorID, statements)
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
	t.Run("Mongo", func(t *testing.T) {
		var counting atomic.Bool
		statements := new(atomic.Int64)
		client, cfg := mongoDB(t, &event.CommandMonitor{Started: func(context.Context, *event.CommandStartedEvent) {
			if counting.Load() {
				statements.Add(1)
			}
		}})
		svc := stats.NewService(stats.NewMongoRepository(client, cfg), zap.NewNop().Sugar())
		counting.Store(true)
		fn(t, svc, primitive.NewObjectID().Hex(), statements)
	})
}

// mongoDB connects to PEE_TEST_MONGO_URI and returns the client and a config
// naming an empty database there, which is dropped when the test ends.
func mongoDB(t *testing.T, monitor *event.CommandMonitor) (*mongo.Client, *config.Config) {
	t.Helper()
	uri := os.Getenv("PEE_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set PEE_TEST_MONGO_URI to run MongoDB integration checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetMonitor(monitor))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))
	cfg := &config.Config{DBName: "stats_" + primitive.NewObjectID().Hex()}
	t.Cleanup(func() {
		require.NoError(t, client.Database(cfg.DBName).Drop(context.Background()))
		require.NoError(t, client.Disconnect(context.Background()))
	})
	return client, cfg
}

// bucket is the part of a stat row a chart shows.
type bucket struct {
	Up, Down, Maintenance  int
	Ping, PingMin, PingMax float64
}

// findBucket returns the stat row that starts at start, which must be the only one there.
func findBucket(t *testing.T, svc stats.Service, monitorID string, start time.Time, period stats.StatPeriod) bucket {
	t.Helper()
	rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, "", start, start, period)
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
			rows, err := svc.FindStatsByMonitorIDAndTimeRange(ctx, monitorID, "", at.Add(-24*time.Hour), at, period)
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

		rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, "", minute, minute.Add(time.Minute), stats.StatMinutely)
		require.NoError(t, err)
		summary := svc.StatPointsSummary(rows)
		require.NotNil(t, summary.AvgPing)
		assert.Equal(t, 200.0, *summary.AvgPing)

		grouped, err := svc.FindStatsByMonitorIDAndTimeRangeWithInterval(context.Background(), monitorID, "", minute, minute.Add(2*time.Minute), stats.StatMinutely, 120)
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

		rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, "", minute, minute.Add(time.Minute), stats.StatMinutely)
		require.NoError(t, err)
		summary := svc.StatPointsSummary(rows)
		require.NotNil(t, summary.MinPing)
		assert.Equal(t, 0.0, *summary.MinPing)

		grouped, err := svc.FindStatsByMonitorIDAndTimeRangeWithInterval(context.Background(), monitorID, "", minute, minute.Add(2*time.Minute), stats.StatMinutely, 120)
		require.NoError(t, err)
		require.Len(t, grouped, 1)
		assert.Equal(t, 0.0, grouped[0].PingMin)
	})
}

func TestConcurrentHeartbeatsInOneBucketAreAllCounted(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		// 50 checks land in the same minute at once, as when several ingester workers
		// handle them together: 10 down, 10 in maintenance, and 30 up with response
		// times 2, 3, 4, 7, 8, 9, ... 47, 48, 49 ms (sum 765).
		errs := make([]error, 50)
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				hb := &stats.HeartbeatPayload{MonitorID: monitorID, Status: int(shared.MonitorStatusUp), Ping: i, Time: minute.Add(time.Duration(i) * time.Second).Unix()}
				switch i % 5 {
				case 0:
					hb.Status, hb.Ping = int(shared.MonitorStatusDown), 0
				case 1:
					hb.Status, hb.Ping = int(shared.MonitorStatusMaintenance), 0
				}
				errs[i] = svc.AggregateHeartbeat(context.Background(), hb)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}

		want := bucket{Up: 40, Down: 10, Maintenance: 10, Ping: 25.5, PingMin: 2, PingMax: 49}
		for period, start := range map[stats.StatPeriod]time.Time{
			stats.StatMinutely: minute,
			stats.StatHourly:   minute,
			stats.StatDaily:    minute.Truncate(24 * time.Hour),
		} {
			got := findBucket(t, svc, monitorID, start, period)
			// Mongo keeps a running average, which can be off in the last bits.
			assert.InDelta(t, want.Ping, got.Ping, 1e-9, period)
			got.Ping = want.Ping
			assert.Equal(t, want, got, period)
		}
	})
}

func TestEachCheckWritesStatsWithOneStatementPerPeriod(t *testing.T) {
	forEachBackendCounting(t, func(t *testing.T, svc stats.Service, monitorID string, statements *atomic.Int64) {
		minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		for _, hb := range []struct {
			name   string
			status shared.MonitorStatus
			at     time.Time
		}{
			{"new buckets", shared.MonitorStatusUp, minute},
			{"existing buckets", shared.MonitorStatusDown, minute.Add(10 * time.Second)},
		} {
			before := statements.Load()
			aggregate(t, svc, monitorID, hb.status, 100, hb.at)
			assert.Equal(t, int64(3), statements.Load()-before, hb.name)
		}
	})
}

func TestMongoStatWrittenBeforeUpgradeKeepsCounting(t *testing.T) {
	client, cfg := mongoDB(t, nil)
	svc := stats.NewService(stats.NewMongoRepository(client, cfg), zap.NewNop().Sugar())
	ctx := context.Background()
	monitorID := primitive.NewObjectID()
	minute := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)

	// Documents as the old code wrote them: a random _id, the average response
	// time, and zeros in ping_min and ping_max while there's no response time.
	// The first minute had checks of 100 and 200 ms, a maintenance check and a
	// down check; the second minute only a down check.
	withPings, downOnly := primitive.NewObjectID(), primitive.NewObjectID()
	_, err := client.Database(cfg.DBName).Collection("stat_minutely").InsertMany(ctx, []any{
		bson.M{"_id": withPings, "monitor_id": monitorID, "timestamp": minute, "ping": 150.0, "ping_min": 100.0, "ping_max": 200.0, "up": 3, "down": 1, "maintenance": 1},
		bson.M{"_id": downOnly, "monitor_id": monitorID, "timestamp": minute.Add(time.Minute), "ping": 0.0, "ping_min": 0.0, "ping_max": 0.0, "up": 0, "down": 1, "maintenance": 0},
	})
	require.NoError(t, err)

	aggregate(t, svc, monitorID.Hex(), shared.MonitorStatusUp, 300, minute.Add(30*time.Second))
	aggregate(t, svc, monitorID.Hex(), shared.MonitorStatusUp, 50, minute.Add(time.Minute+30*time.Second))

	rows, err := svc.FindStatsByMonitorIDAndTimeRange(ctx, monitorID.Hex(), "", minute, minute.Add(time.Minute), stats.StatMinutely)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	got := func(s *stats.Stat) bucket { return bucket{s.Up, s.Down, s.Maintenance, s.Ping, s.PingMin, s.PingMax} }
	assert.Equal(t, withPings.Hex(), rows[0].ID)
	assert.Equal(t, bucket{Up: 4, Down: 1, Maintenance: 1, Ping: 200, PingMin: 100, PingMax: 300}, got(rows[0]))
	assert.Equal(t, downOnly.Hex(), rows[1].ID)
	assert.Equal(t, bucket{Up: 1, Down: 1, Ping: 50, PingMin: 50, PingMax: 50}, got(rows[1]))
}

func TestFailedStatsAggregationForAHeartbeatIsLogged(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	// No migrations, so there are no stats tables and aggregating fails.
	svc := stats.NewService(stats.NewSQLRepository(sqliteDB(t)), zap.New(core).Sugar())
	bus := eventstest.NewBus()
	svc.RegisterEventHandlers(bus)

	monitorID := uuid.NewString()
	require.NoError(t, events.Publish(context.Background(), bus, events.HeartbeatEvent,
		shared.HeartBeatModel{MonitorID: monitorID, Status: shared.MonitorStatusUp, Ping: 100, Time: time.Now()}))

	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, monitorID, fields["monitor_id"])
	assert.Contains(t, fields["error"], "no such table")
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

		summary, err := svc.Summarize(context.Background(), monitorID, "", now.Add(-24*time.Hour), now)
		require.NoError(t, err)
		require.NotNil(t, summary.Uptime)
		assert.InDelta(t, 100*22.0/24, *summary.Uptime, 1e-9)
	})
}

func TestSummarizeCountsDownHeartbeatsAtTheStartOf7And30DayUptime(t *testing.T) {
	for _, days := range []int{7, 30} {
		t.Run(fmt.Sprintf("%dd", days), func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
				// Off midnight, so reading day buckets would drop the checks before the first midnight.
				now := time.Date(2026, 10, 17, 10, 30, 0, 0, time.UTC)
				since := now.AddDate(0, 0, -days)
				// A check at a quarter past every hour from a day before the window, the last at 10:15 today.
				for k := range (days+1)*24 + 1 {
					at := since.AddDate(0, 0, -1).Truncate(time.Hour).Add(time.Duration(k)*time.Hour + 15*time.Minute)
					status := shared.MonitorStatusUp
					// 09:15 is before the window; 11:15 and 12:15 are its first two checks.
					if at.Equal(since.Add(-75*time.Minute)) || at.Equal(since.Add(45*time.Minute)) || at.Equal(since.Add(105*time.Minute)) {
						status = shared.MonitorStatusDown
					}
					aggregate(t, svc, monitorID, status, 100, at)
				}

				summary, err := svc.Summarize(context.Background(), monitorID, "", since, now)
				require.NoError(t, err)
				require.NotNil(t, summary.Uptime)
				checks := float64(days * 24)
				assert.InDelta(t, 100*(checks-2)/checks, *summary.Uptime, 1e-9)
			})
		})
	}
}

// rowCounter records how many rows each SELECT the database runs returns.
type rowCounter struct {
	db   *bun.DB
	rows *[]int
}

func (c rowCounter) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (c rowCounter) AfterQuery(ctx context.Context, e *bun.QueryEvent) {
	if !strings.HasPrefix(e.Query, "SELECT") || strings.HasPrefix(e.Query, "SELECT count(*) FROM (") {
		return
	}
	var n int
	if err := c.db.NewRaw("SELECT count(*) FROM ("+e.Query+") AS q").Scan(ctx, &n); err == nil {
		*c.rows = append(*c.rows, n)
	}
}

// statRow is one row of a stat_minutely, stat_hourly or stat_daily table.
type statRow struct {
	MonitorID string    `bun:"monitor_id"`
	Timestamp time.Time `bun:"timestamp"`
	Up        int       `bun:"up"`
	PingSum   float64   `bun:"ping_sum"`
	PingCount int       `bun:"ping_count"`
}

func TestUptimeForOneMonitorReadsAtMost2700StatsRows(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrateUp(t, db, migrations.Migrations)
			monitorID := insertMonitor(t, db)
			ctx := context.Background()
			now := time.Now().UTC()
			// More rows than any window reads: 48h of minute stats, 91 days of hour
			// stats and 366 days of day stats.
			for _, p := range []struct {
				table string
				step  time.Duration
				keep  time.Duration
			}{
				{"stat_minutely", time.Minute, 48 * time.Hour},
				{"stat_hourly", time.Hour, 91 * 24 * time.Hour},
				{"stat_daily", 24 * time.Hour, 366 * 24 * time.Hour},
			} {
				var rows []statRow
				for at := now.Add(-p.keep).Truncate(p.step); !at.After(now); at = at.Add(p.step) {
					rows = append(rows, statRow{MonitorID: monitorID, Timestamp: at, Up: 1, PingSum: 100, PingCount: 1})
				}
				for len(rows) > 0 {
					batch := rows[:min(len(rows), 500)]
					rows = rows[len(batch):]
					_, err := db.NewInsert().Model(&batch).ModelTableExpr(p.table).Exec(ctx)
					require.NoError(t, err)
				}
			}

			var read []int
			db.AddQueryHook(rowCounter{db, &read})
			svc := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
			for _, window := range []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 365 * 24 * time.Hour} {
				_, err := svc.Summarize(ctx, monitorID, "", now.Add(-window), now)
				require.NoError(t, err)
			}

			// 1,440 minute rows for 24h, 168 and 720 hour rows for 7d and 30d, 365 day rows for 365d.
			require.Len(t, read, 4)
			total := 0
			for _, n := range read {
				total += n
			}
			assert.LessOrEqual(t, total, 2700)
			assert.InDeltaSlice(t, []float64{1440, 168, 720, 365}, []float64{float64(read[0]), float64(read[1]), float64(read[2]), float64(read[3])}, 1)
		})
	}
}
