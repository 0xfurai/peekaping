package heartbeat_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// addHeartbeatProbeMigration is the migration that added probe_id to heartbeats.
const addHeartbeatProbeMigration = "20261024120000"

// onEverySQLBackend runs fn on a database with every migration applied and a monitor in it.
func onEverySQLBackend(t *testing.T, fn func(t *testing.T, db *bun.DB, svc heartbeat.Service, bus *eventstest.Bus, monitorID string)) {
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
			monitorID := insertMonitor(t, db)
			bus := eventstest.NewBus()
			fn(t, db, heartbeat.NewService(heartbeat.NewSQLRepository(db), bus, zap.NewNop().Sugar()), bus, monitorID)
		})
	}
}

func insertMonitor(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
	require.NoError(t, err)
	return id
}

func TestSQLRepository_CreateRecordsTheProbeThatProducedTheHeartbeat(t *testing.T) {
	onEverySQLBackend(t, func(t *testing.T, _ *bun.DB, svc heartbeat.Service, bus *eventstest.Bus, monitorID string) {
		ctx := context.Background()
		at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		remote, err := svc.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, ProbeID: "probe-frankfurt", Status: shared.MonitorStatusUp, Time: at})
		require.NoError(t, err)
		unset, err := svc.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, Status: shared.MonitorStatusUp, Time: at.Add(time.Minute)})
		require.NoError(t, err)

		assert.Equal(t, "probe-frankfurt", remote.ProbeID, "Create returns the probe")
		assert.Equal(t, "local", unset.ProbeID, "a heartbeat with no probe came from the local probe")
		for _, tc := range []struct{ id, want string }{{remote.ID, "probe-frankfurt"}, {unset.ID, "local"}} {
			stored, err := svc.FindByID(ctx, tc.id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tc.want, stored.ProbeID, "the probe stored")
		}

		listed, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
		require.NoError(t, err)
		require.Len(t, listed, 2)
		assert.Equal(t, []string{"probe-frankfurt", "local"}, []string{listed[0].ProbeID, listed[1].ProbeID}, "the history lists the probes")

		published := eventstest.Published(bus, events.HeartbeatEvent)
		require.Len(t, published, 2)
		assert.Equal(t, "probe-frankfurt", published[0].ProbeID, "the heartbeat event carries the probe")
		assert.Equal(t, "local", published[1].ProbeID)
	})
}

// A heartbeat stored before the upgrade was made by the local probe: nothing else ran checks.
func TestMigrationsBackfillExistingHeartbeatsWithTheLocalProbe(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addHeartbeatProbeMigration {
					before.Add(m)
				}
			}
			old := migrate.NewMigrator(db, before)
			require.NoError(t, old.Init(ctx))
			_, err := old.Migrate(ctx)
			require.NoError(t, err)
			monitorID, heartbeatID := insertMonitor(t, db), uuid.NewString()
			_, err = db.Exec(`INSERT INTO heartbeats (id, monitor_id, status, msg, ping, time) VALUES (?, ?, 1, 'before the upgrade', 7, ?)`,
				heartbeatID, monitorID, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC))
			require.NoError(t, err)

			_, err = migrate.NewMigrator(db, migrations.Migrations).Migrate(ctx)
			require.NoError(t, err)

			svc := heartbeat.NewService(heartbeat.NewSQLRepository(db), eventstest.NewBus(), zap.NewNop().Sugar())
			stored, err := svc.FindByID(ctx, heartbeatID)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "before the upgrade", stored.Msg)
			assert.Equal(t, "local", stored.ProbeID)
		})
	}
}
