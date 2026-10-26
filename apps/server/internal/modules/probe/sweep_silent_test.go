package probe_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/probe"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// silentFixture is the sweeps of two api replicas over one database, one Redis and one clock, with
// the local probe as the migration made it: offline, with no instance. The monitors the local probe
// has to run are the fixture's to set.
type silentFixture struct {
	*sweepFixture
	redis    *miniredis.Miniredis
	monitors atomic.Int32 // how many active monitors need an instance of the local probe
	logs     *observer.ObservedLogs
}

func newSilentFixture(t *testing.T, monitors int) *silentFixture {
	t.Helper()
	f := &silentFixture{sweepFixture: newSweepFixture(t), redis: miniredis.RunT(t)}
	f.monitors.Store(int32(monitors))
	return f
}

// replica is the sweep one api replica runs when it holds the leader lock, and its log. Its term as
// the leader begins now.
func (f *silentFixture) replica(t *testing.T) *probe.Sweep {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: f.redis.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	core, logs := observer.New(zap.WarnLevel)
	f.logs = logs
	sweep := probe.NewSweep(probe.SweepConfig{
		Repo:         f.repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		StartedAt:    f.clock.Now().Add(-24 * time.Hour),
		Now:          f.clock.Now,
		Silent: &probe.SilentWatch{
			Monitors:  func(context.Context) (int, error) { return int(f.monitors.Load()), nil },
			Announced: probe.NewRedisAnnouncements(client),
		},
		Logger: zap.New(core).Sugar(),
	})
	sweep.BeginTerm()
	return sweep
}

func (f *silentFixture) offlineEvents() []probe.Event {
	return eventstest.Published(f.bus, probe.ProbeOffline)
}

// A cluster on SQLite or PostgreSQL that was upgraded without deploying probes has nothing that
// runs its checks, and its local probe was never online, so nothing would ever say so. The leader
// reports the local probe offline once, when it has monitors to run and no instance has said hello
// within PROBE_OFFLINE_AFTER of the leader taking the lock.
func TestSweep_ALocalProbeWithMonitorsAndNoInstanceIsReportedOfflineOnce(t *testing.T) {
	f := newSilentFixture(t, 3)
	first := f.replica(t)

	require.NoError(t, first.SweepOnce(context.Background()))
	f.clock.Advance(3*time.Minute - time.Second)
	require.NoError(t, first.SweepOnce(context.Background()))
	assert.Empty(t, f.offlineEvents(), "the instances get PROBE_OFFLINE_AFTER from the lock to say hello")

	f.clock.Advance(time.Second)
	require.NoError(t, first.SweepOnce(context.Background()))

	offline := f.offlineEvents()
	require.Len(t, offline, 1)
	assert.Equal(t, probe.LocalID, offline[0].ProbeID)
	assert.Equal(t, "Local", offline[0].ProbeName)
	assert.True(t, f.clock.Now().Equal(offline[0].Since), "offline since it was found so: %s", offline[0].Since)
	assert.Equal(t, probe.StatusOffline, f.status(t, probe.LocalID), "and its status shows offline")
	warnings := f.logs.FilterMessageSnippet("the local probe has 3 active monitors and no instance").All()
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN")

	require.NoError(t, first.SweepOnce(context.Background()))
	assert.Len(t, f.offlineEvents(), 1, "once, not on every pass")
}

// The report is once for the install, not for each leader: when the leader stops and another replica
// takes the lock, the local probe is still the one already reported
func TestSweep_TheLocalProbeIsNotReportedAgainWhenAnotherReplicaTakesTheLock(t *testing.T) {
	f := newSilentFixture(t, 3)
	first := f.replica(t)
	f.clock.Advance(3 * time.Minute)
	require.NoError(t, first.SweepOnce(context.Background()))
	require.Len(t, f.offlineEvents(), 1)

	f.clock.Advance(time.Hour)
	second := f.replica(t)
	f.clock.Advance(3 * time.Minute)
	require.NoError(t, second.SweepOnce(context.Background()))

	assert.Len(t, f.offlineEvents(), 1, "the second replica finds it reported")
	assert.Empty(t, f.logs.All(), "and says nothing more")
}

// A leader that takes the lock gives the instances PROBE_OFFLINE_AFTER from then to say hello, not
// from the start of the process: they were not asked until then
func TestSweep_TheGraceForTheLocalProbeCountsFromTheLeaderTakingTheLock(t *testing.T) {
	f := newSilentFixture(t, 3)
	f.clock.Advance(time.Hour) // the process has been up for an hour, as a follower
	leader := f.replica(t)

	require.NoError(t, leader.SweepOnce(context.Background()))
	f.clock.Advance(2 * time.Minute)
	require.NoError(t, leader.SweepOnce(context.Background()))

	assert.Empty(t, f.offlineEvents(), "2 minutes after taking the lock")
}

func TestSweep_TheLocalProbeWithNoMonitorsToRunIsNotReported(t *testing.T) {
	f := newSilentFixture(t, 0)
	leader := f.replica(t)

	f.clock.Advance(time.Hour)
	require.NoError(t, leader.SweepOnce(context.Background()))

	assert.Empty(t, f.offlineEvents(), "nothing needs an instance")

	f.monitors.Store(1)
	require.NoError(t, leader.SweepOnce(context.Background()))
	assert.Len(t, f.offlineEvents(), 1, "until a monitor does")
}

// When an instance says hello the usual online notification follows, and the local probe is reported
// again when its instances stop, as any probe is
func TestSweep_TheLocalProbeComesBackAndGoesOfflineAsAnyProbeDoes(t *testing.T) {
	f := newSilentFixture(t, 3)
	leader := f.replica(t)
	f.clock.Advance(3 * time.Minute)
	require.NoError(t, leader.SweepOnce(context.Background()))
	require.Len(t, f.offlineEvents(), 1)

	f.instance(t, probe.LocalID, "probe-1", 0)
	require.NoError(t, leader.SweepOnce(context.Background()))
	assert.Equal(t, probe.StatusOnline, f.status(t, probe.LocalID))
	online := eventstest.Published(f.bus, probe.ProbeOnline)
	require.Len(t, online, 1)
	assert.Equal(t, probe.LocalID, online[0].ProbeID)

	f.clock.Advance(4 * time.Minute)
	require.NoError(t, leader.SweepOnce(context.Background()))
	assert.Equal(t, probe.StatusOffline, f.status(t, probe.LocalID))
	assert.Len(t, f.offlineEvents(), 2, "its instances stopped: reported as any probe is")

	// Its instance is forgotten after a day. The probe was reported when its instances stopped, and
	// is not reported again for having none.
	require.NoError(t, leader.SweepOnce(context.Background()))
	removed, err := f.repo.DeleteInstancesUnseenSince(context.Background(), f.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	f.clock.Advance(3 * time.Minute)
	require.NoError(t, leader.SweepOnce(context.Background()))
	assert.Len(t, f.offlineEvents(), 2)
}

// Single mode is the process itself: nothing is watched
func TestSweep_WithNoSilentWatchTheLocalProbeIsLeftAlone(t *testing.T) {
	f := newSweepFixture(t)
	f.clock.Advance(time.Hour)

	f.run(t)

	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))
}

// What the api says at startup: the same words as the sweep, so that an operator reads them in the
// log of the replica they just started
func TestSilentWatch_WarnsAtStartupOfALocalProbeWithMonitorsAndNoInstance(t *testing.T) {
	f := newSilentFixture(t, 3)
	f.replica(t)
	core, logs := observer.New(zap.WarnLevel)
	watch := &probe.SilentWatch{
		Monitors:  func(context.Context) (int, error) { return 3, nil },
		Announced: probe.NewRedisAnnouncements(redis.NewClient(&redis.Options{Addr: f.redis.Addr()})),
	}

	silent, err := watch.WarnIfSilent(context.Background(), f.repo, zap.New(core).Sugar())

	require.NoError(t, err)
	assert.True(t, silent)
	warnings := logs.All()
	require.Len(t, warnings, 1)
	assert.Equal(t, "the local probe has 3 active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN", warnings[0].Message)

	t.Run("an instance has said hello", func(t *testing.T) {
		f.instance(t, probe.LocalID, "probe-1", 0)

		silent, err := watch.WarnIfSilent(context.Background(), f.repo, zap.New(core).Sugar())

		require.NoError(t, err)
		assert.False(t, silent)
		assert.Len(t, logs.All(), 1, "nothing more is said")
	})
}
