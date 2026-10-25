package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	wiring "peekaping/internal/app"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countStatsOnTheLocalBus has the replica aggregate the stats of the heartbeats it stored, as cmd/api
// does at startup
func (a *app) countStatsOnTheLocalBus(t *testing.T) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(s stats.Service, local *wiring.LocalBus) { s.RegisterEventHandlers(local) }))
}

// countedUp is how many Up checks the stats hold for the monitor, of every probe
func (a *app) countedUp(t *testing.T, monitorID string) int {
	t.Helper()
	var up int
	require.NoError(t, a.container.Invoke(func(s stats.Service) error {
		points, err := s.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, "", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), stats.StatMinutely)
		for _, point := range points {
			up += point.Up
		}
		return err
	}))
	return up
}

// AC3: the stats count a heartbeat once, with two replicas, however many replicas hear of it. The
// replica that stored it counts it, from its own bus.
func TestClusterIngest_AHeartbeatIsCountedOnceByTheReplicaThatStoredIt(t *testing.T) {
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	a.countStatsOnTheLocalBus(t)
	b.countStatsOnTheLocalBus(t)
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up&ping=12", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool { return a.countedUp(t, monitorID) >= 1 }, 5*time.Second, 10*time.Millisecond, "the replica that stored it counts it")
	// Long enough for the other replica to have heard of the heartbeat, over Redis, and counted it
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, a.countedUp(t, monitorID), "and the other replica does not")

	w = b.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up&ping=12", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool { return a.countedUp(t, monitorID) >= 2 }, 5*time.Second, 10*time.Millisecond, "a heartbeat the other replica stored is counted there")
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, a.countedUp(t, monitorID), "once")
}

// The premise of the local bus: on the cluster's bus, Redis pub/sub hands every heartbeat to every
// replica, so stats that listened there would count it once for each replica
func TestClusterIngest_OnTheClusterBusEveryReplicaWouldCountAHeartbeat(t *testing.T) {
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	for _, replica := range []*app{a, b} {
		require.NoError(t, replica.container.Invoke(func(s stats.Service, bus events.EventBus) { s.RegisterEventHandlers(bus) }))
	}
	monitorID := a.createMonitor(t, "Nightly backup", "backup-push-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up&ping=12", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool { return a.countedUp(t, monitorID) >= 2 }, 5*time.Second, 10*time.Millisecond)
}
