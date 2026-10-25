package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// takeNotificationsFromTheQueue has the replica take the notification events from the queue and
// hand them to its listener, as cmd/api does at startup
func (a *app) takeNotificationsFromTheQueue(t *testing.T) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(consumer *notification_channel.QueueConsumer) error {
		if err := consumer.Start(); err != nil {
			return err
		}
		t.Cleanup(consumer.Stop)
		return nil
	}))
}

// AC5: with two api replicas, each notification is sent once. Redis pub/sub would give the event to
// both, and each would send it; the queue gives it to one.
func TestClusterReplicas_AMonitorGoingDownIsNotifiedOnce(t *testing.T) {
	blockExternalHTTP(t)
	a := newAppWithAdmin(t)
	b := newReplica(t, a)
	a.takeNotificationsFromTheQueue(t)
	b.takeNotificationsFromTheQueue(t)
	hook := startRequestReceiver(t)
	channel := secretChannelOfType(t, "webhook").withVisible(map[string]any{"webhook_url": hook.url + "/hook"})
	channelID := a.createSecretChannel(t, channel)
	monitorID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors",
		with(pushMonitorBody("Nightly backup", "backup-push-token"), "notification_ids", []string{channelID}), a.asAdmin), http.StatusCreated).ID

	// The first heartbeat is notified, and then the monitor is Up
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=up", nil).Code)
	require.Eventually(t, func() bool { return len(hook.received()) >= 1 }, 10*time.Second, 20*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	hook.forget()

	// It goes Down, on a push that reaches one replica
	require.Equal(t, http.StatusOK, b.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=down&msg=disk%20full", nil).Code)

	require.Eventually(t, func() bool { return len(hook.received()) >= 1 }, 10*time.Second, 20*time.Millisecond, "the notification is sent")
	// Long enough for the other replica to have sent it too, had it been given the event
	time.Sleep(time.Second)
	sent := hook.received()
	require.Len(t, sent, 1, "and sent once")
	assert.Contains(t, string(sent[0].body), "Nightly backup")
	_ = monitorID
}

// AC5: a change made on one replica wakes the assignment polls of the probe instances connected to
// the others. The monitors are dealt among the probe's instances, so a monitor changes the share of
// one instance; each replica answers the poll of the instance that holds it.
func TestClusterReplicas_AMonitorChangeWakesThePollsOnBothReplicas(t *testing.T) {
	a := localProbeApp(t, nil)
	b := newReplica(t, a, func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })
	onA, onB := uuid.NewString(), uuid.NewString()
	a.sayHello(t, localToken, onA)
	b.sayHello(t, localToken, onB)
	const count = 24
	for i := range count {
		a.createHTTPMonitor(t, "Monitor "+string(rune('A'+i)), "")
	}
	shareOfA := decodeAssignments(t, a.assignments(t, localToken, onA, ""))
	shareOfB := decodeAssignments(t, b.assignments(t, localToken, onB, ""))
	require.NotEmpty(t, shareOfA.Monitors, "each instance holds some of the monitors")
	require.NotEmpty(t, shareOfB.Monitors)
	require.Equal(t, count, len(shareOfA.Monitors)+len(shareOfB.Monitors), "and together all of them")

	t.Run("a change on the first replica reaches the poll held by the second", func(t *testing.T) {
		answered := b.startPoll(localToken, onB, shareOfB.Version)
		stillWaiting(t, answered)

		target := shareOfB.Monitors[0]
		w := a.request(t, http.MethodPut, "/api/v1/monitors/"+target.ID, httpMonitorBody("Renamed on the first replica", ""), a.asAdmin)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
		assert.NotEqual(t, shareOfB.Version, got.Version)
		names := map[string]string{}
		for _, m := range got.Monitors {
			names[m.ID] = m.Name
		}
		assert.Equal(t, "Renamed on the first replica", names[target.ID])
	})

	t.Run("a change on the second replica reaches the poll held by the first", func(t *testing.T) {
		// The version names the assignment of the whole probe, so the first change moved it for the
		// instance on this replica too
		shareOfA = decodeAssignments(t, a.assignments(t, localToken, onA, ""))
		answered := a.startPoll(localToken, onA, shareOfA.Version)
		stillWaiting(t, answered)

		target := shareOfA.Monitors[0]
		w := b.request(t, http.MethodPut, "/api/v1/monitors/"+target.ID, httpMonitorBody("Renamed on the second replica", ""), b.asAdmin)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
		assert.NotEqual(t, shareOfA.Version, got.Version)
		names := map[string]string{}
		for _, m := range got.Monitors {
			names[m.ID] = m.Name
		}
		assert.Equal(t, "Renamed on the second replica", names[target.ID])
	})
}

// AC4: the local probe's monitors are checked by peekaping-probe instances that use the local
// probe's token (PROBE_BOOTSTRAP_TOKEN on the api and on the probe): a monitor without a probe of
// its own is dealt to an instance of the local probe, and its results are stored on the replica that
// took them
func TestClusterLocalProbe_ItsMonitorsAreDealtToAnInstanceWithTheBootstrapToken(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()

	a.sayHello(t, localToken, instanceID)
	got := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))

	assert.Equal(t, []string{monitorID}, monitorIDs(got.Monitors), "the monitor runs on the local probe by default")
	require.Equal(t, []probeproto.ResultOutcome{{ID: "up", Accepted: true}}, a.outcomesOf(t, localToken, instanceID, checkResult("up", monitorID, checkedAt(0))))
	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 1)
	assert.Equal(t, shared.LocalProbeID, beats[0].ProbeID)
}

// AC4: in cluster mode the local probe is online while its instances are connected, and offline once
// they stopped, like a remote probe: the sweep the leader api replica runs sets it offline and the
// notification queue carries the probe offline notification. The watchdog leaves its monitors alone
// meanwhile, since the probe's own notification says why they are not checked.
func TestClusterLocalProbe_GoesOfflineWhenItsInstancesStopAndRaisesTheNotification(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	require.Equal(t, "online", a.getProbe(t, probe.LocalID).Status, "its first instance said hello")
	a.clearQueuedNotifications(t)
	var offlineFunc stale.ProbeOfflineFunc
	require.NoError(t, a.container.Invoke(func(f stale.ProbeOfflineFunc) { offlineFunc = f }))
	offline, err := offlineFunc(context.Background(), a.findMonitor(t, monitorID))
	require.NoError(t, err)
	require.False(t, offline, "the watchdog watches the monitor while an instance is connected")

	// The instance stopped without a word 10 minutes ago
	require.NoError(t, a.container.Invoke(func(probes probe.Service) error {
		_, err := probes.TouchInstance(context.Background(), probe.LocalID, instanceID, time.Now().Add(-10*time.Minute))
		return err
	}))
	a.runSweepUntil(t, func() bool { return a.getProbe(t, probe.LocalID).Status == "offline" })

	assert.Equal(t, []string{"probe.offline"}, a.queuedNotificationTopics(t), "the probe's channels are told once, through the notification queue")
	offline, err = offlineFunc(context.Background(), a.findMonitor(t, monitorID))
	require.NoError(t, err)
	assert.True(t, offline, "and the watchdog leaves the monitor alone while the probe is offline")
	assert.Equal(t, shared.LocalProbeIsItsInstances, a.localProbeLiveness(t))
}

// runSweepUntil runs the probe sweep as the leader replica does, on the replica's own bus and
// repository, until done. The sweep's grace period after a start is over: the process has been up
// for as long as the probe offline threshold.
func (a *app) runSweepUntil(t *testing.T, done func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	require.NoError(t, a.container.Invoke(func(repo probe.Repository, bus events.EventBus) {
		sweep := probe.NewSweep(probe.SweepConfig{
			Repo:         repo,
			EventBus:     bus,
			OfflineAfter: probe.MinOfflineAfter,
			Interval:     10 * time.Millisecond,
			StartedAt:    time.Now().Add(-time.Hour),
		})
		go func() { defer close(finished); sweep.Run(ctx) }()
	}))
	t.Cleanup(func() { cancel(); <-finished })
	require.Eventually(t, done, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-finished
}

// findMonitor reads the monitor from the replica's service
func (a *app) findMonitor(t *testing.T, id string) *monitor.Model {
	t.Helper()
	var found *monitor.Model
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		var err error
		found, err = monitors.FindByID(context.Background(), id)
		return err
	}))
	require.NotNil(t, found)
	return found
}

// localProbeLiveness is how the replica knows that the local probe is online
func (a *app) localProbeLiveness(t *testing.T) shared.LocalProbeLiveness {
	t.Helper()
	var liveness shared.LocalProbeLiveness
	require.NoError(t, a.container.Invoke(func(l shared.LocalProbeLiveness) { liveness = l }))
	return liveness
}

// forget drops the requests the receiver has been sent
func (r *requestReceiver) forget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = nil
}
