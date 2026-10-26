package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/stale"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// AC4: the api's watchdog waits for a stale monitor's new check as long as it did when the producer
// ran it (with the worker's task timeout): the check's timeout, plus the 2 seconds the executor may
// keep running after the check gave up, plus 8 seconds for its result to be stored. The expected
// values are worked out by hand from that formula (timeout + 2s + 8s), so a change to either grace
// shows here.
func TestClusterWatchdog_WaitsForANewCheckTheCheckTimeoutPlusTenSeconds(t *testing.T) {
	cfg := clusterWatchdogParams{Logger: zap.NewNop().Sugar()}.config()

	for timeoutSeconds, want := range map[int]time.Duration{
		0:   10 * time.Second,
		16:  26 * time.Second,
		48:  58 * time.Second,
		120: 130 * time.Second,
	} {
		assert.Equal(t, want, cfg.CheckTimeout(timeoutSeconds), "a check with a timeout of %d seconds", timeoutSeconds)
	}
}

// The watchdog reads the probes' supported types from the probe service, in single mode
func TestRegisterSingle_TheWatchdogReadsTheTypesOfRealProbes(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(lookup stale.ProbeLookup, probes probe.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))

		assertProbeLookupReadsRealProbes(t, lookup, probes)
	}))
}

// and in cluster mode, where the api replica that holds the leader lock runs the watchdog: its
// notification of a stale monitor names the probes that don't run the monitor's type, as single
// mode's does
func TestRegisterClusterAPI_TheLeadersWatchdogNamesTheProbesThatDoNotRunAStaleMonitorsType(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		params clusterWatchdogParams,
		probes probe.Service,
		monitors monitor.Service,
		bus events.EventBus,
		db *bun.DB,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))
		ctx := context.Background()

		// A monitor on a probe that reported the types it runs after the monitor was assigned, and
		// the monitor's is not among them
		frankfurt, err := probes.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		instance := &probe.Instance{ID: "instance-1", Hostname: "edge-1", LastSeenAt: time.Now().UTC(), SupportedTypes: []string{"http", "tcp"}}
		require.NoError(t, probes.RegisterInstance(ctx, frankfurt.ID, instance))
		created, err := monitors.Create(ctx, &monitor.CreateUpdateDto{
			Type: "http", Name: "Checkout", Interval: 60, Timeout: 16, RetryInterval: 60, Active: true, Config: "{}",
			ProbeIDs: []string{frankfurt.ID},
		})
		require.NoError(t, err)
		instance.SupportedTypes = []string{"tcp"}
		require.NoError(t, probes.RegisterInstance(ctx, frankfurt.ID, instance))
		// The probe is online: the watchdog leaves the monitors of an offline probe alone
		changed, err := probes.MarkOnline(ctx, frankfurt.ID, time.Now())
		require.NoError(t, err)
		require.True(t, changed)

		// The watchdog the api builds, on a clock the test moves and a bus of its own to listen on
		var skew atomic.Int64
		cfg := params.config()
		cfg.Now = func() time.Time { return time.Now().UTC().Add(time.Duration(skew.Load())) }
		cfg.Interval = 10 * time.Millisecond
		sink := events.NewMemoryBus()
		t.Cleanup(func() { assert.NoError(t, sink.Close(context.Background())) })
		notified := make(chan stale.Event, 1)
		events.Subscribe(events.NewSubscriber(sink, "test"), stale.MonitorStale, func(_ context.Context, event stale.Event) {
			notified <- event
		})
		cfg.EventBus = sink
		watchdog := stale.NewWatchdog(cfg)
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			watchdog.Run(runCtx)
		}()
		t.Cleanup(func() {
			stop()
			<-done
		})

		skew.Store(int64(2 * time.Hour)) // the monitor was last saved two hours ago
		require.Eventually(t, func() bool {
			all, err := params.Store.All(ctx)
			require.NoError(t, err)
			_, found := all[created.ID]
			return found
		}, 5*time.Second, 10*time.Millisecond, "the watchdog found the monitor stale")
		skew.Store(int64(4 * time.Hour)) // and the time it is given to be checked ran out

		select {
		case event := <-notified:
			assert.Equal(t, created.ID, event.MonitorID)
			assert.Equal(t, []string{"Frankfurt"}, event.UnsupportedProbes)
		case <-time.After(5 * time.Second):
			t.Fatal("the stale monitor was not notified")
		}
	}))
}

// assertProbeLookupReadsRealProbes checks the watchdog's lookup against the real probe service: a
// probe's name and the types it reported, none for a probe that has not reported, and nil for a
// probe that doesn't exist.
func assertProbeLookupReadsRealProbes(t *testing.T, lookup stale.ProbeLookup, probes probe.Service) {
	t.Helper()
	ctx := context.Background()
	remote, err := probes.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)

	found, err := lookup.FindProbe(ctx, remote.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Frankfurt", found.Name)
	assert.Empty(t, found.SupportedTypes, "a probe that has not reported its types runs every type")

	require.NoError(t, probes.RegisterInstance(ctx, remote.ID, &probe.Instance{ID: "instance-1", Hostname: "edge-1", LastSeenAt: time.Now().UTC(), SupportedTypes: []string{"http", "tcp"}}))
	found, err = lookup.FindProbe(ctx, remote.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, []string{"http", "tcp"}, found.SupportedTypes)

	missing, err := lookup.FindProbe(ctx, "no-such-probe")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
