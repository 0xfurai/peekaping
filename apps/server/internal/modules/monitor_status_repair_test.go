package modules_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A monitor's stored status has to follow its latest heartbeat on every backend, since databases
// from before the fix hold monitors whose stored status says otherwise.
func TestRepairStoredStatusesFollowsTheLatestHeartbeat(t *testing.T) {
	onEveryMonitorAndHeartbeatBackend(t, func(t *testing.T, monitors monitor.MonitorRepository, heartbeats heartbeat.Repository) {
		ctx := context.Background()
		bus := eventstest.NewBus()
		logger := zap.NewNop().Sugar()
		service := monitor.NewMonitorService(monitors, nil, heartbeat.NewService(heartbeats, bus, logger), bus, nil, nil, nil, nil, nil, logger)

		create := func(name string, stored shared.MonitorStatus) string {
			created, err := monitors.Create(ctx, &monitor.Model{Type: "http", Name: name, Interval: 60, Timeout: 10, Active: true, Config: "{}", Status: stored})
			require.NoError(t, err)
			return created.ID
		}
		// SQL repositories time a heartbeat when they save it, so record the older one first
		recordHeartbeat := func(monitorID string, status shared.MonitorStatus, ago time.Duration) {
			_, err := heartbeats.Create(ctx, &heartbeat.Model{MonitorID: monitorID, Status: status, Time: time.Now().UTC().Add(-ago)})
			require.NoError(t, err)
		}

		stale := create("Stale", shared.MonitorStatusUp)
		recordHeartbeat(stale, shared.MonitorStatusUp, 2*time.Minute)
		recordHeartbeat(stale, shared.MonitorStatusDown, time.Minute)
		matching := create("Matching", shared.MonitorStatusUp)
		recordHeartbeat(matching, shared.MonitorStatusUp, time.Minute)
		silent := create("Silent", shared.MonitorStatusDown)
		saved := func(id string) *monitor.Model {
			m, err := monitors.FindByID(ctx, id)
			require.NoError(t, err)
			require.NotNil(t, m)
			return m
		}
		matchingBefore, silentBefore := saved(matching), saved(silent)

		repaired, err := service.RepairStoredStatuses(ctx)

		require.NoError(t, err)
		assert.Equal(t, 1, repaired)
		assert.Equal(t, shared.MonitorStatusDown, saved(stale).Status)
		assert.Equal(t, shared.MonitorStatusUp, saved(matching).Status)
		assert.Equal(t, shared.MonitorStatusDown, saved(silent).Status)
		assert.True(t, saved(matching).UpdatedAt.Equal(matchingBefore.UpdatedAt), "a monitor that was right should not be saved again")
		assert.True(t, saved(silent).UpdatedAt.Equal(silentBefore.UpdatedAt), "a monitor without heartbeats should not be saved")
		assert.Empty(t, bus.Topics(), "the repair is not a status change, so it announces nothing")
	})
}
