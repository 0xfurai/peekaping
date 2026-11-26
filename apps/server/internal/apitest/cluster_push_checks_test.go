package apitest

import (
	"net/http"
	"testing"
	"time"

	wiring "peekaping/internal/app"
	"peekaping/internal/leader"
	"peekaping/internal/modules/shared"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// startLeaderJobs has the replica run the jobs of the leader api replica, under the node id, with a
// lock that two replicas in one process can tell apart (cmd/api's node id is the host and process).
// The jobs stop when the test ends, or when the returned function is called.
func (a *app) startLeaderJobs(t *testing.T, nodeID string) (stop func()) {
	t.Helper()
	require.NoError(t, a.container.Decorate(func(client *redis.Client, logger *zap.SugaredLogger) *leader.Elector {
		return leader.New(client, nodeID, logger, leader.WithRenewalInterval(100*time.Millisecond))
	}))
	var jobs *wiring.LeaderJobs
	require.NoError(t, a.container.Invoke(func(j *wiring.LeaderJobs) { jobs = j }))
	jobs.Start()
	t.Cleanup(jobs.Stop)
	return jobs.Stop
}

// pushMonitorEverySecond is a push monitor whose interval is a second, which the API takes 20 s as
// the least of
func (a *app) pushMonitorEverySecond(t *testing.T, name, token string) string {
	t.Helper()
	id := a.createMonitor(t, name, token)
	a.checkEverySecond(t, id)
	return id
}

// In the cluster on SQLite and PostgreSQL nothing but the api runs the "no push arrived in time"
// check: the probes are never dealt a push monitor. The replica that holds the leader lock judges the
// push monitors, so a monitor that never gets a push goes Down, and goes Down once, not once for each
// replica.
func TestClusterPushChecks_OnlyTheLeaderJudgesAPushMonitorThatGetsNoPush(t *testing.T) {
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	monitorID := a.pushMonitorEverySecond(t, "Nightly backup", "backup-push-token")

	a.startLeaderJobs(t, "replica-a")
	require.Eventually(t, func() bool { return len(a.storedHeartbeats(t, monitorID)) >= 1 }, 15*time.Second, 50*time.Millisecond,
		"the leader finds that no push arrived")
	b.startLeaderJobs(t, "replica-b")
	time.Sleep(4 * time.Second)

	beats := a.storedHeartbeats(t, monitorID)
	require.GreaterOrEqual(t, len(beats), 3, "a check at each interval")
	var important int
	for i, beat := range beats {
		assert.Equal(t, shared.MonitorStatusDown, beat.Status)
		assert.Equal(t, shared.LocalProbeID, beat.ProbeID)
		if beat.Important {
			important++
		}
		// The first check is the one the leader makes the moment it starts, which the first timer
		// can follow within a moment: the aligned second is what comes next
		if i > 1 {
			assert.GreaterOrEqual(t, beat.Time.Sub(beats[i-1].Time), 500*time.Millisecond,
				"one heartbeat for each interval, not one for each replica: %s and %s", beats[i-1].Time, beat.Time)
		}
	}
	assert.Equal(t, 1, important, "the monitor goes Down once")
	assert.LessOrEqual(t, len(beats), 9, "and a second's interval is not checked twice over")
}

// A push keeps the monitor Up: the check of the leader finds it in time, and ingests nothing. The
// push reaches whichever replica the load balancer picks.
func TestClusterPushChecks_APushKeepsTheMonitorUp(t *testing.T) {
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	monitorID := a.pushMonitorEverySecond(t, "Nightly backup", "backup-push-token")
	require.Equal(t, http.StatusOK, b.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up", nil).Code)
	a.startLeaderJobs(t, "replica-a")
	b.startLeaderJobs(t, "replica-b")

	pushes := 0
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
		replica := a
		if pushes%2 == 1 {
			replica = b
		}
		require.Equal(t, http.StatusOK, replica.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up", nil).Code)
		pushes++
		time.Sleep(300 * time.Millisecond)
	}

	beats := a.storedHeartbeats(t, monitorID)
	assert.Len(t, beats, pushes+1, "the heartbeats are the pushes, and the leader added none")
	for _, beat := range beats {
		assert.Equal(t, shared.MonitorStatusUp, beat.Status)
	}
}

// When the leader stops, the replica that takes the lock goes on judging the monitor
func TestClusterPushChecks_AnotherReplicaGoesOnWhenTheLeaderStops(t *testing.T) {
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	monitorID := a.pushMonitorEverySecond(t, "Nightly backup", "backup-push-token")
	stopA := a.startLeaderJobs(t, "replica-a")
	require.Eventually(t, func() bool { return len(a.storedHeartbeats(t, monitorID)) >= 1 }, 15*time.Second, 50*time.Millisecond)
	b.startLeaderJobs(t, "replica-b")

	stopA()
	stoppedWith := len(b.storedHeartbeats(t, monitorID))

	require.Eventually(t, func() bool { return len(b.storedHeartbeats(t, monitorID)) >= stoppedWith+2 }, 20*time.Second, 100*time.Millisecond,
		"the other replica judges the monitor from then on")
	for _, beat := range b.storedHeartbeats(t, monitorID) {
		assert.Equal(t, shared.MonitorStatusDown, beat.Status)
	}
}
