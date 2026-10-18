package heartbeat_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// recordingBus is an event bus that keeps every published event.
type recordingBus struct {
	mu        sync.Mutex
	published []events.Event
}

func (b *recordingBus) Subscribe(events.EventType, events.EventHandler) {}

func (b *recordingBus) Publish(event events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, event)
}

func (b *recordingBus) Close() error { return nil }

func (b *recordingBus) events() []events.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]events.Event(nil), b.published...)
}

type backend struct {
	svc      heartbeat.Service
	bus      *recordingBus
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
			bus := &recordingBus{}
			fn(t, backend{heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, zap.NewNop().Sugar()), bus, monitorA, monitorB})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
	t.Run("Mongo", func(t *testing.T) {
		uri := os.Getenv("PEE_TEST_MONGO_URI")
		if uri == "" {
			t.Skip("set PEE_TEST_MONGO_URI to run the MongoDB tests")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		require.NoError(t, err)
		require.NoError(t, client.Ping(ctx, nil))
		cfg := &config.Config{DBName: "heartbeat_" + primitive.NewObjectID().Hex()}
		t.Cleanup(func() {
			require.NoError(t, client.Database(cfg.DBName).Drop(context.Background()))
			require.NoError(t, client.Disconnect(context.Background()))
		})
		bus := &recordingBus{}
		fn(t, backend{heartbeat.NewService(heartbeat.NewMongoRepository(client, cfg), bus, zap.NewNop().Sugar()), bus, primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()})
	})
}

// heartbeat records a heartbeat now. The SQL repository stamps heartbeats with
// the insert time and Mongo keeps the given one, so they can't be backdated;
// the pause keeps their times apart on every backend.
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

		published := b.bus.events()
		require.Len(t, published, 1)
		assert.Equal(t, events.HeartbeatEvent, published[0].Type)
		assert.Equal(t, created, published[0].Payload)
	})
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
