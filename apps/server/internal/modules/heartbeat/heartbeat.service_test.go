package heartbeat_test

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
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

type backend struct {
	svc      heartbeat.Service
	bus      *eventstest.Bus
	monitorA string
	monitorB string
}

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "heartbeats.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "heartbeat_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema)); err != nil {
		_ = admin.Close()
		require.NoError(t, err)
	}
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return db
}

// forEachBackend runs fn against a heartbeat service on every database
// backend, with two monitors to record heartbeats for.
func forEachBackend(t *testing.T, fn func(t *testing.T, b backend)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			monitorA, monitorB := uuid.NewString(), uuid.NewString()
			for _, id := range []string{monitorA, monitorB} {
				_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
				require.NoError(t, err)
			}
			bus := eventstest.NewBus()
			fn(t, backend{heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, zap.NewNop().Sugar()), bus, monitorA, monitorB})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

// heartbeat records a heartbeat now. The pause keeps the times of the
// heartbeats apart on every backend.
func (b backend) heartbeat(t *testing.T, monitorID string, status shared.MonitorStatus, msg string, important bool) *heartbeat.Model {
	t.Helper()
	created, err := b.svc.Create(context.Background(), &heartbeat.CreateUpdateDto{
		MonitorID: monitorID,
		Status:    status,
		Msg:       msg,
		Important: important,
		Time:      time.Now().UTC(),
	})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	return created
}

func msgs(heartbeats []*heartbeat.Model) []string {
	out := []string{}
	for _, hb := range heartbeats {
		out = append(out, hb.Msg)
	}
	return out
}

func TestCreateStoresTheHeartbeatAndPublishesIt(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		created, err := b.svc.Create(ctx, &heartbeat.CreateUpdateDto{
			MonitorID: b.monitorA,
			Status:    shared.MonitorStatusUp,
			Msg:       "200 OK",
			Ping:      42,
			Important: true,
			Time:      time.Now().UTC(),
		})
		require.NoError(t, err)

		stored, err := b.svc.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, b.monitorA, stored.MonitorID)
		assert.Equal(t, shared.MonitorStatusUp, stored.Status)
		assert.Equal(t, "200 OK", stored.Msg)
		assert.Equal(t, 42, stored.Ping)
		assert.True(t, stored.Important)

		assert.Equal(t, []string{events.HeartbeatEvent.Name()}, b.bus.Topics())
		assert.Equal(t, []heartbeat.Model{*created}, eventstest.Published(b.bus, events.HeartbeatEvent))
	})
}

// A heartbeat is stored at the time its check started, so that a result that
// arrives late, such as a probe's buffered one, lands where it belongs in the monitor's history.
// Only a heartbeat with no time is stamped with the insert time.
func TestSQLRepository_CreateKeepsAGivenTime(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			monitorID := uuid.NewString()
			_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID)
			require.NoError(t, err)
			svc := heartbeat.NewService(heartbeat.NewSQLRepository(db), eventstest.NewBus(), zap.NewNop().Sugar())
			checkedAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Millisecond)

			given, err := svc.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, Status: shared.MonitorStatusUp, Msg: "given", Time: checkedAt, EndTime: checkedAt.Add(time.Second)})
			require.NoError(t, err)
			unstamped, err := svc.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, Status: shared.MonitorStatusUp, Msg: "unstamped"})
			require.NoError(t, err)

			assert.True(t, checkedAt.Equal(given.Time), "the time given: %s", given.Time)
			stored, err := svc.FindByID(ctx, given.ID)
			require.NoError(t, err)
			assert.True(t, checkedAt.Equal(stored.Time), "and the time stored: %s", stored.Time)
			assert.WithinDuration(t, time.Now(), unstamped.Time, time.Minute, "a heartbeat with no time is stamped now")
			newestFirst, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, false)
			require.NoError(t, err)
			assert.Equal(t, []string{"unstamped", "given"}, msgs(newestFirst), "and the history is in the order of the times")
		})
	}
}

func TestFindByMonitorIDPaginatedReturnsTheNewestPageInTimeOrder(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		for _, msg := range []string{"a1", "a2", "a3"} {
			b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, msg, false)
		}
		b.heartbeat(t, b.monitorB, shared.MonitorStatusUp, "b1", false)
		for _, msg := range []string{"a4", "a5"} {
			b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, msg, false)
		}

		pages := [][]string{}
		for page := range 3 {
			heartbeats, err := b.svc.FindByMonitorIDPaginated(ctx, b.monitorA, 2, page, nil, true)
			require.NoError(t, err)
			pages = append(pages, msgs(heartbeats))
		}
		assert.Equal(t, [][]string{{"a4", "a5"}, {"a2", "a3"}, {"a1"}}, pages)

		// Without reverse, the newest heartbeat comes first: the ingester reads
		// the previous heartbeat this way.
		latest, err := b.svc.FindByMonitorIDPaginated(ctx, b.monitorA, 2, 0, nil, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"a5", "a4"}, msgs(latest))
	})
}

func TestFindByMonitorIDPaginatedFiltersImportantHeartbeats(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		b.heartbeat(t, b.monitorA, shared.MonitorStatusDown, "went down", true)
		b.heartbeat(t, b.monitorA, shared.MonitorStatusDown, "still down", false)
		b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, "came back", true)
		b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, "still up", false)

		yes, no := true, false
		important, err := b.svc.FindByMonitorIDPaginated(ctx, b.monitorA, 10, 0, &yes, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"went down", "came back"}, msgs(important))

		routine, err := b.svc.FindByMonitorIDPaginated(ctx, b.monitorA, 10, 0, &no, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"still down", "still up"}, msgs(routine))

		all, err := b.svc.FindByMonitorIDPaginated(ctx, b.monitorA, 10, 0, nil, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"went down", "still down", "came back", "still up"}, msgs(all))
	})
}

// Only Up and Down heartbeats here: how Pending and Maintenance count is
// PP-183.
func TestUptimeIsTheShareOfUpHeartbeatsInEachPeriod(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()

		empty, err := b.svc.FindUptimeStatsByMonitorID(ctx, b.monitorA, map[string]time.Duration{"1h": time.Hour}, time.Now())
		require.NoError(t, err)
		assert.Equal(t, map[string]float64{"1h": 0}, empty)

		b.heartbeat(t, b.monitorA, shared.MonitorStatusDown, "", false)
		b.heartbeat(t, b.monitorA, shared.MonitorStatusDown, "", false)
		b.heartbeat(t, b.monitorB, shared.MonitorStatusUp, "", false)
		b.heartbeat(t, b.monitorB, shared.MonitorStatusDown, "", false)
		time.Sleep(10 * time.Millisecond)
		cutoff := time.Now()
		time.Sleep(10 * time.Millisecond)
		for _, status := range []shared.MonitorStatus{shared.MonitorStatusUp, shared.MonitorStatusUp, shared.MonitorStatusDown, shared.MonitorStatusUp} {
			b.heartbeat(t, b.monitorA, status, "", false)
		}
		now := time.Now()

		// "half" starts at the cutoff, so it holds only the newer heartbeats.
		periods := map[string]time.Duration{"1h": time.Hour, "half": now.Sub(cutoff)}
		statsA, err := b.svc.FindUptimeStatsByMonitorID(ctx, b.monitorA, periods, now)
		require.NoError(t, err)
		assert.Equal(t, map[string]float64{"1h": 50, "half": 75}, statsA)

		statsB, err := b.svc.FindUptimeStatsByMonitorID(ctx, b.monitorB, periods, now)
		require.NoError(t, err)
		assert.Equal(t, map[string]float64{"1h": 50, "half": 0}, statsB)

		// A minute on, the last 30 seconds hold no heartbeats.
		later, err := b.svc.FindUptimeStatsByMonitorID(ctx, b.monitorA, map[string]time.Duration{"30s": 30 * time.Second}, now.Add(time.Minute))
		require.NoError(t, err)
		assert.Equal(t, map[string]float64{"30s": 0}, later)
	})
}

func TestDeleteOlderThanKeepsNewerHeartbeats(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, "old 1", false)
		b.heartbeat(t, b.monitorB, shared.MonitorStatusUp, "old 2", false)
		time.Sleep(10 * time.Millisecond)
		cutoff := time.Now()
		time.Sleep(10 * time.Millisecond)
		b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, "new 1", false)
		b.heartbeat(t, b.monitorB, shared.MonitorStatusUp, "new 2", false)

		deleted, err := b.svc.DeleteOlderThan(ctx, cutoff)
		require.NoError(t, err)
		assert.Equal(t, int64(2), deleted)

		for monitorID, want := range map[string]string{b.monitorA: "new 1", b.monitorB: "new 2"} {
			left, err := b.svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
			require.NoError(t, err)
			assert.Equal(t, []string{want}, msgs(left))
		}
	})
}

func TestHasAnyReportsWhetherAHeartbeatExists(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		has, err := b.svc.HasAny(ctx)
		require.NoError(t, err)
		assert.False(t, has, "before the first heartbeat")

		b.heartbeat(t, b.monitorA, shared.MonitorStatusUp, "first", false)
		has, err = b.svc.HasAny(ctx)
		require.NoError(t, err)
		assert.True(t, has, "after the first heartbeat")
	})
}

func TestDeleteOlderThanDeletesHeartbeatsWrittenOutOfTimeOrder(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			monitorID := uuid.NewString()
			_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID)
			require.NoError(t, err)

			// The first heartbeat was stamped while the server's clock ran a
			// year ahead; the clock was then fixed.
			now := time.Now().UTC()
			cutoff := now.Add(-30 * 24 * time.Hour)
			rows := []heartbeatRow{{ID: uuid.NewString(), MonitorID: monitorID, Status: 1, Msg: "ahead", Time: now.AddDate(1, 0, 0)}}
			for minute := 6000; minute > 0; minute-- {
				rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: monitorID, Status: 1, Msg: "old", Time: cutoff.Add(-time.Duration(minute) * time.Minute)})
			}
			rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: monitorID, Status: 1, Msg: "new", Time: now})
			for len(rows) > 0 {
				chunk := rows[:min(len(rows), 1000)]
				_, err := db.NewInsert().Model(&chunk).Exec(ctx)
				require.NoError(t, err)
				rows = rows[len(chunk):]
			}

			svc := heartbeat.NewService(heartbeat.NewSQLRepository(db), eventstest.NewBus(), zap.NewNop().Sugar())
			deleted, err := svc.DeleteOlderThan(ctx, cutoff)
			require.NoError(t, err)

			assert.Equal(t, int64(6000), deleted)
			left, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
			require.NoError(t, err)
			assert.Equal(t, []string{"new", "ahead"}, msgs(left))
		})
	}
}

// reworkHeartbeatIndexesMigration is the first of the two migrations that
// dropped the heartbeat indexes no query needs and added the one the cleanup
// deletes through.
const reworkHeartbeatIndexesMigration = "20261018120000"

// heartbeatIndexes returns the names of the indexes the migrations created on heartbeats.
func heartbeatIndexes(t *testing.T, db *bun.DB) []string {
	t.Helper()
	query := "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'heartbeats' AND name LIKE 'idx_%' ORDER BY name"
	if db.Dialect().Name() == dialect.PG {
		query = "SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'heartbeats' AND indexname LIKE 'idx_%' ORDER BY indexname"
	}
	var names []string
	require.NoError(t, db.NewRaw(query).Scan(context.Background(), &names))
	return names
}

func TestMigrationsKeepOnlyTheHeartbeatIndexesQueriesUse(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < reworkHeartbeatIndexesMigration {
					before.Add(m)
				}
			}
			old := migrate.NewMigrator(db, before)
			require.NoError(t, old.Init(ctx))
			_, err := old.Migrate(ctx)
			require.NoError(t, err)
			all := []string{
				"idx_heartbeats_important",
				"idx_heartbeats_monitor_important",
				"idx_heartbeats_monitor_time",
				"idx_heartbeats_monitor_time_important",
				"idx_heartbeats_status",
			}
			require.Equal(t, all, heartbeatIndexes(t, db))

			migrator := migrate.NewMigrator(db, migrations.Migrations)
			_, err = migrator.Migrate(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{"idx_heartbeats_monitor_important", "idx_heartbeats_monitor_probe_time", "idx_heartbeats_monitor_time_important", "idx_heartbeats_time"}, heartbeatIndexes(t, db))

			_, err = migrator.Rollback(ctx)
			require.NoError(t, err)
			assert.Equal(t, all, heartbeatIndexes(t, db))
		})
	}
}

// heartbeatRow writes heartbeats with a chosen time, which the repository's
// Create doesn't allow.
type heartbeatRow struct {
	bun.BaseModel `bun:"table:heartbeats"`

	ID        string    `bun:"id,pk"`
	MonitorID string    `bun:"monitor_id"`
	Status    int       `bun:"status"`
	Msg       string    `bun:"msg"`
	Time      time.Time `bun:"time"`
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

func TestDeleteOlderThanClearsABacklogLargerThanOneBatch(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			monitorA, monitorB := uuid.NewString(), uuid.NewString()
			for _, id := range []string{monitorA, monitorB} {
				_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
				require.NoError(t, err)
			}

			// Two monitors checked every minute for 9,000 minutes before a
			// 30-day cutoff, and twice after it, written in time order like the
			// ingester writes them.
			now := time.Now().UTC()
			cutoff := now.Add(-30 * 24 * time.Hour)
			var rows []heartbeatRow
			for minute := 9000; minute > 0; minute-- {
				at := cutoff.Add(-time.Duration(minute) * time.Minute)
				for _, id := range []string{monitorA, monitorB} {
					rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: id, Status: 1, Msg: "old", Time: at})
				}
			}
			for _, at := range []time.Time{cutoff.Add(time.Minute), now.Add(-time.Minute)} {
				for _, id := range []string{monitorA, monitorB} {
					rows = append(rows, heartbeatRow{ID: uuid.NewString(), MonitorID: id, Status: 1, Msg: "new", Time: at})
				}
			}
			for len(rows) > 0 {
				chunk := rows[:min(len(rows), 1000)]
				_, err := db.NewInsert().Model(&chunk).Exec(ctx)
				require.NoError(t, err)
				rows = rows[len(chunk):]
			}
			// A hole of 6,000 old heartbeats, wider than a batch, like the gap a
			// deleted monitor leaves.
			_, err = db.ExecContext(ctx, "DELETE FROM heartbeats WHERE time >= ? AND time < ?", cutoff.Add(-6000*time.Minute), cutoff.Add(-3000*time.Minute))
			require.NoError(t, err)

			deletes := &deleteCounter{}
			db.AddQueryHook(deletes)
			svc := heartbeat.NewService(heartbeat.NewSQLRepository(db), eventstest.NewBus(), zap.NewNop().Sugar())
			deleted, err := svc.DeleteOlderThan(ctx, cutoff)
			require.NoError(t, err)

			assert.Equal(t, int64(12_000), deleted)
			var total int64
			for _, n := range deletes.rows {
				assert.LessOrEqual(t, n, int64(5000), "heartbeats deleted by one statement")
				total += n
			}
			assert.Equal(t, int64(12_000), total)
			for _, monitorID := range []string{monitorA, monitorB} {
				left, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
				require.NoError(t, err)
				assert.Equal(t, []string{"new", "new"}, msgs(left))
			}

			// Once every heartbeat is older than the cutoff, one run deletes them all.
			deleted, err = svc.DeleteOlderThan(ctx, now.Add(time.Minute))
			require.NoError(t, err)
			assert.Equal(t, int64(4), deleted)
			for _, monitorID := range []string{monitorA, monitorB} {
				left, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
				require.NoError(t, err)
				assert.Empty(t, left)
			}
		})
	}
}
