package heartbeat_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestSQLRepository_FindByMonitorIDAndProbeIDPaginatedFiltersByProbe(t *testing.T) {
	onEverySQLBackend(t, func(t *testing.T, db *bun.DB, svc heartbeat.Service, _ *eventstest.Bus, monitorID string) {
		ctx := context.Background()
		otherMonitorID := insertMonitor(t, db)
		at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		beat := func(monitor, probe, msg string, minute int, important bool) {
			_, err := svc.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitor, ProbeID: probe, Status: shared.MonitorStatusUp, Msg: msg, Important: important, Time: at.Add(time.Duration(minute) * time.Minute)})
			require.NoError(t, err)
		}
		beat(monitorID, "local", "local 1", 0, true)
		beat(monitorID, "probe-frankfurt", "frankfurt 1", 1, true)
		beat(monitorID, "local", "local 2", 2, false)
		beat(monitorID, "probe-frankfurt", "frankfurt 2", 3, false)
		beat(monitorID, "probe-frankfurt", "frankfurt 3", 4, false)
		beat(otherMonitorID, "probe-frankfurt", "other monitor", 5, false)

		find := func(probeID string, limit, page int, important *bool) []string {
			t.Helper()
			found, err := svc.FindByMonitorIDAndProbeIDPaginated(ctx, monitorID, probeID, limit, page, important, true)
			require.NoError(t, err)
			return msgs(found)
		}
		yes := true

		assert.Equal(t, []string{"local 1", "frankfurt 1", "local 2", "frankfurt 2", "frankfurt 3"}, find("", 10, 0, nil), "no probe: every location")
		assert.Equal(t, []string{"local 1", "local 2"}, find("local", 10, 0, nil))
		assert.Equal(t, []string{"frankfurt 1", "frankfurt 2", "frankfurt 3"}, find("probe-frankfurt", 10, 0, nil))
		assert.Equal(t, []string{"frankfurt 2", "frankfurt 3"}, find("probe-frankfurt", 2, 0, nil), "the newest page")
		assert.Equal(t, []string{"frankfurt 1"}, find("probe-frankfurt", 2, 1, nil), "the next page")
		assert.Equal(t, []string{"frankfurt 1"}, find("probe-frankfurt", 10, 0, &yes), "with the important filter")
		assert.Empty(t, find("probe-oslo", 10, 0, nil), "a probe that ran no checks of the monitor")

		// The plain list still covers every probe
		all, err := svc.FindByMonitorIDPaginated(ctx, monitorID, 10, 0, nil, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"local 1", "frankfurt 1", "local 2", "frankfurt 2", "frankfurt 3"}, msgs(all))
	})
}
