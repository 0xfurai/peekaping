package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/tag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// newSQLiteContainer registers the scheduler with the real services over a migrated SQLite
// database, as a single-mode binary would, with the given config: the event bus and the stale
// monitors are in memory.
func newSQLiteContainer(t testing.TB, cfg *config.Config, logger *zap.SugaredLogger) *dig.Container {
	t.Helper()
	cfg.DBType = "sqlite"
	cfg.DBName = filepath.Join(t.TempDir(), "scheduler.db")

	container := dig.New()
	provide := func(constructor any) {
		t.Helper()
		require.NoError(t, container.Provide(constructor))
	}
	provide(func() *config.Config { return cfg })
	provide(func() *zap.SugaredLogger { return logger })
	provide(infra.ProvideSQLDB)
	provide(func() events.EventBus { return newBus(t) })
	provide(stale.ProvideMemoryStore)
	// The probe module provides the real one. The scheduler's checks don't depend on probes.
	provide(func() stale.ProbeOfflineFunc { return nil })

	require.NoError(t, errors.Join(
		heartbeat.RegisterDependencies(container, cfg),
		healthcheck.RegisterDependencies(container),
		tag.RegisterDependencies(container, cfg),
		monitor_tag.RegisterDependencies(container, cfg),
		monitor.RegisterDependencies(container, cfg),
		proxy.RegisterDependencies(container, cfg),
		maintenance.RegisterDependencies(container, cfg),
		monitor_maintenance.RegisterDependencies(container, cfg),
		monitor_notification.RegisterDependencies(container, cfg),
		setting.RegisterDependencies(container, cfg),
		notification_sent_history.RegisterDependencies(container, cfg),
		monitor_tls_info.RegisterDependencies(container, cfg),
		certificate.RegisterDependencies(container),
		stats.RegisterDependencies(container, cfg),
		check.RegisterDependencies(container),
		ingester.RegisterDependencies(container),
		RegisterDependencies(container),
	))

	require.NoError(t, container.Invoke(func(db *bun.DB) error {
		t.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		if err := migrator.Init(ctx); err != nil {
			return err
		}
		_, err := migrator.Migrate(ctx)
		return err
	}))
	return container
}

func TestRegisterDependencies_ChecksAMonitorAndStoresItsHeartbeatThroughTheRealServices(t *testing.T) {
	t.Parallel()
	container := newSQLiteContainer(t, &config.Config{CheckConcurrency: 4}, zap.NewNop().Sugar())

	require.NoError(t, container.Invoke(func(
		s *Scheduler,
		watchdog *stale.Watchdog,
		monitors monitor.Service,
		heartbeats heartbeat.Service,
		bus events.EventBus,
	) {
		ctx := context.Background()
		require.NotNil(t, watchdog)
		s.Subscribe(bus)
		require.NoError(t, s.Start(ctx))
		t.Cleanup(s.Stop)

		// A push monitor that has had no push is Down when it is checked
		mon, err := monitors.Create(ctx, &monitor.CreateUpdateDto{
			Type:     "push",
			Name:     "Push",
			Interval: 60,
			Timeout:  16,
			Active:   true,
			Config:   `{"pushToken":"token"}`,
		})
		require.NoError(t, err)

		var latest []*shared.HeartBeatModel
		eventually(t, 5*time.Second, func() bool {
			latest, err = heartbeats.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
			require.NoError(t, err)
			return len(latest) == 1
		}, "the new monitor's check is stored as a heartbeat")
		assert.Equal(t, shared.MonitorStatusDown, latest[0].Status)
		assert.Equal(t, "No push received yet", latest[0].Msg)
		assert.True(t, latest[0].Important, "a monitor's first heartbeat is important")
	}))
}
