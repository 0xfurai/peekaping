package app

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// The scheduler, its source and the watchdog come with the real services over a migrated SQLite
// database, the in-memory event bus and the in-memory stale monitors, as cmd/peekaping runs them
func TestRegisterSingle_TheSchedulerChecksAMonitorAndStoresItsHeartbeatThroughTheRealServices(t *testing.T) {
	container := dig.New()
	cfg := singleConfig(t)
	cfg.CheckConcurrency = 4
	require.NoError(t, RegisterSingle(container, cfg))

	require.NoError(t, container.Invoke(func(
		s *scheduler.Scheduler,
		watchdog *stale.Watchdog,
		monitors monitor.Service,
		heartbeats heartbeat.Service,
		bus events.EventBus,
		db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))

		ctx := context.Background()
		require.NotNil(t, watchdog)
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
		deadline := time.Now().Add(5 * time.Second)
		for len(latest) != 1 {
			latest, err = heartbeats.FindByMonitorIDPaginated(ctx, mon.ID, 1, 0, nil, false)
			require.NoError(t, err)
			require.True(t, len(latest) == 1 || time.Now().Before(deadline), "the new monitor's check is stored as a heartbeat")
			time.Sleep(20 * time.Millisecond)
		}
		assert.Equal(t, shared.MonitorStatusDown, latest[0].Status)
		assert.Equal(t, "No push received yet", latest[0].Msg)
		assert.True(t, latest[0].Important, "a monitor's first heartbeat is important")
	}))
}
