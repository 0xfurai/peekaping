package probe_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

var errSweepBoom = errors.New("boom")

// sweepFailingRepo is the SQL repository whose status updates fail or change nothing, and whose read
// of the connections fails
type sweepFailingRepo struct {
	probe.Repository
	updateErr      error
	unchanged      bool
	connectionsErr error
}

func (r sweepFailingRepo) UpdateStatus(ctx context.Context, id string, from, to probe.Status, at time.Time) (bool, error) {
	if r.updateErr != nil {
		return false, r.updateErr
	}
	if r.unchanged {
		return false, nil
	}
	return r.Repository.UpdateStatus(ctx, id, from, to, at)
}

func (r sweepFailingRepo) FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*probe.InstanceConnection, error) {
	if r.connectionsErr != nil {
		return nil, r.connectionsErr
	}
	return r.Repository.FindInstanceConnections(ctx, probeID, since)
}

// fakeAnnouncements is the record of the probes reported offline, with calls that fail on demand
type fakeAnnouncements struct {
	announced                                  bool
	first                                      bool
	announcedErr, announceErr, forgetErr       error
	announcedCalls, announceCalls, forgetCalls int
	mu                                         sync.Mutex
}

func (a *fakeAnnouncements) Announced(context.Context, string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.announcedCalls++
	return a.announced, a.announcedErr
}

func (a *fakeAnnouncements) Announce(context.Context, string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.announceCalls++
	return a.first, a.announceErr
}

func (a *fakeAnnouncements) Forget(context.Context, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.forgetCalls++
	return a.forgetErr
}

// failingSweep is a sweep over the fixture's database with a repository that fails, and the log it
// writes at info level
func (f *sweepFixture) failingSweep(t *testing.T, repo probe.Repository, silent *probe.SilentWatch, begin bool) (*probe.Sweep, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	sweep := probe.NewSweep(probe.SweepConfig{
		Repo:         repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		StartedAt:    f.clock.Now().Add(-24 * time.Hour),
		Now:          f.clock.Now,
		Silent:       silent,
		Logger:       zap.New(core).Sugar(),
	})
	if begin {
		sweep.BeginTerm()
	}
	return sweep, logs
}

// localOnline puts the local probe, which the migration made, online
func (f *sweepFixture) localOnline(t *testing.T) {
	t.Helper()
	changed, err := f.repo.UpdateStatus(context.Background(), probe.LocalID, probe.StatusOffline, probe.StatusOnline, time.Now())
	require.NoError(t, err)
	require.True(t, changed)
}

func fieldOf(entry observer.LoggedEntry, key string) any { return entry.ContextMap()[key] }

func TestSweep_AFailureToSetAProbeOfflineIsLoggedAndTheSweepGoesOn(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	sweep, logs := f.failingSweep(t, sweepFailingRepo{Repository: f.repo, updateErr: errSweepBoom}, nil, false)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, probe.StatusOnline, f.status(t, quiet.ID))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))
	failed := logs.FilterMessage("Failed to set a probe offline").All()
	require.Len(t, failed, 1)
	assert.Equal(t, quiet.ID, fieldOf(failed[0], "probeId"))
	assert.Equal(t, "boom", fieldOf(failed[0], "error"))
	assert.Empty(t, logs.FilterMessage("Probe is offline").All())
}

func TestSweep_AProbeThatAnotherSweepSetOfflineIsNeitherLoggedNorAnnounced(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	sweep, logs := f.failingSweep(t, sweepFailingRepo{Repository: f.repo, unchanged: true}, nil, false)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))
	assert.Empty(t, logs.All())
}

func TestSweep_AFailureToSetAProbeOnlineIsLoggedAndTheSweepGoesOn(t *testing.T) {
	f := newSweepFixture(t)
	back := f.probe(t, "Paris", probe.StatusOffline)
	f.instance(t, back.ID, "paris-1", time.Minute)
	sweep, logs := f.failingSweep(t, sweepFailingRepo{Repository: f.repo, updateErr: errSweepBoom}, nil, false)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, probe.StatusOffline, f.status(t, back.ID))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
	failed := logs.FilterMessage("Failed to set a probe online").All()
	require.Len(t, failed, 1)
	assert.Equal(t, back.ID, fieldOf(failed[0], "probeId"))
	assert.Equal(t, "boom", fieldOf(failed[0], "error"))
}

func TestSweep_AProbeThatARequestAlreadyBroughtOnlineIsNeitherLoggedNorAnnounced(t *testing.T) {
	f := newSweepFixture(t)
	back := f.probe(t, "Paris", probe.StatusOffline)
	f.instance(t, back.ID, "paris-1", time.Minute)
	sweep, logs := f.failingSweep(t, sweepFailingRepo{Repository: f.repo, unchanged: true}, nil, false)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
	assert.Empty(t, logs.All())
}

func TestSweep_AProbeWhoseConnectionsCannotBeReadWentOfflineWhenItsInstancesWereLastSeen(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	seen := f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	sweep, logs := f.failingSweep(t, sweepFailingRepo{Repository: f.repo, connectionsErr: errSweepBoom}, nil, false)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, probe.StatusOffline, f.status(t, quiet.ID))
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.True(t, seen.Equal(offline[0].Since), "since is the instances' last seen time: %s", offline[0].Since)
	warned := logs.FilterMessage("Failed to read the connections of a probe, it went offline when its instances were last seen").All()
	require.Len(t, warned, 1)
	assert.Equal(t, quiet.ID, fieldOf(warned[0], "probeId"))
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
	assert.Len(t, logs.FilterMessage("Probe is offline").All(), 1)
}

func TestSweep_AFailureToPublishIsLoggedAndTheStatusStaysChanged(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	sweep, logs := f.failingSweep(t, f.repo, nil, false)
	require.NoError(t, f.bus.Close(context.Background()))

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, probe.StatusOffline, f.status(t, quiet.ID))
	failed := logs.FilterMessage("Failed to publish a probe event").All()
	require.Len(t, failed, 1)
	assert.Equal(t, probe.ProbeOffline.Name(), fieldOf(failed[0], "topic"))
	assert.Contains(t, fieldOf(failed[0], "error"), "closed")
}

// The words the sweep says when a step of reporting the local probe fails
const (
	forgetFailed   = "Failed to forget that the local probe was reported offline"
	announcedAsked = "Failed to find out whether the local probe was reported offline"
	countFailed    = "Failed to count the monitors of the local probe"
	announceFailed = "Failed to record that the local probe was reported offline"
)

// silentSweep is the sweep of a leader that watches for the local probe, with monitors to run,
// and the log it writes at info level
func (f *sweepFixture) silentSweep(t *testing.T, announced *fakeAnnouncements, monitors func(context.Context) (int, error), begin bool) (*probe.Sweep, *observer.ObservedLogs) {
	t.Helper()
	return f.failingSweep(t, f.repo, &probe.SilentWatch{Monitors: monitors, Announced: announced}, begin)
}

func threeMonitors(context.Context) (int, error) { return 3, nil }

func TestSweep_AFailureToForgetTheReportOfAnOnlineLocalProbeIsLogged(t *testing.T) {
	f := newSweepFixture(t)
	f.localOnline(t)
	f.instance(t, probe.LocalID, "probe-1", 0)
	announced := &fakeAnnouncements{forgetErr: errSweepBoom}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, 1, announced.forgetCalls)
	warned := logs.FilterMessage(forgetFailed).All()
	require.Len(t, warned, 1)
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
	assert.Equal(t, probe.StatusOnline, f.status(t, probe.LocalID))
}

func TestSweep_AFailureToLearnWhetherTheLocalProbeWasReportedLeavesItUnreported(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{announcedErr: errSweepBoom, first: true}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)
	f.clock.Advance(3 * time.Minute)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline())
	assert.Zero(t, announced.announceCalls)
	warned := logs.FilterMessage(announcedAsked).All()
	require.Len(t, warned, 1)
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
}

func TestSweep_AnAlreadyReportedLocalProbeIsNotCountedOrReportedAgain(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{announced: true, first: true}
	counted := 0
	sweep, logs := f.silentSweep(t, announced, func(context.Context) (int, error) { counted++; return 3, nil }, true)
	f.clock.Advance(3 * time.Minute)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline())
	assert.Zero(t, counted)
	assert.Zero(t, announced.announceCalls)
	assert.Empty(t, logs.All())
}

func TestSweep_AFailureToCountTheLocalProbesMonitorsLeavesItUnreported(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{first: true}
	sweep, logs := f.silentSweep(t, announced, func(context.Context) (int, error) { return 3, errSweepBoom }, true)
	f.clock.Advance(3 * time.Minute)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline())
	assert.Zero(t, announced.announceCalls)
	warned := logs.FilterMessage(countFailed).All()
	require.Len(t, warned, 1)
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
}

func TestSweep_AFailureToRecordTheReportLeavesTheLocalProbeUnreported(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{announceErr: errSweepBoom}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)
	f.clock.Advance(3 * time.Minute)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline(), "unrecorded, so not reported: the next pass tries again")
	assert.Equal(t, 1, announced.announceCalls)
	warned := logs.FilterMessage(announceFailed).All()
	require.Len(t, warned, 1)
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
	assert.Empty(t, logs.FilterMessageSnippet("deploy peekaping-probe").All())
}

func TestSweep_TheReplicaThatLostTheRaceToReportTheLocalProbeSaysNothing(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{first: false}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)
	f.clock.Advance(3 * time.Minute)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline())
	assert.Equal(t, 1, announced.announceCalls)
	assert.Empty(t, logs.All())
}

func TestSweep_AFailureToRecordTheReportOfAProbeThatWentQuietIsLoggedAndItIsStillAnnounced(t *testing.T) {
	f := newSweepFixture(t)
	f.localOnline(t)
	f.instance(t, probe.LocalID, "probe-1", 4*time.Minute)
	announced := &fakeAnnouncements{announceErr: errSweepBoom}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Equal(t, probe.StatusOffline, f.status(t, probe.LocalID))
	assert.Len(t, f.eventsOffline(), 1, "the probe went quiet, whatever the record says")
	warned := logs.FilterMessage(announceFailed).All()
	require.Len(t, warned, 1)
	assert.Equal(t, "boom", fieldOf(warned[0], "error"))
}

func TestSweep_AProbeOtherThanTheLocalOneIsNotWatchedForHavingNoInstance(t *testing.T) {
	f := newSweepFixture(t)
	f.probe(t, "Frankfurt", probe.StatusOffline)
	announced := &fakeAnnouncements{first: true}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, true)
	f.clock.Advance(time.Hour)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	offline := f.eventsOffline()
	require.Len(t, offline, 1, "the local probe is reported, and no other")
	assert.Equal(t, probe.LocalID, offline[0].ProbeID)
	assert.Equal(t, 1, announced.announceCalls, "once, for the local probe")
	assert.Len(t, logs.FilterMessageSnippet("deploy peekaping-probe").All(), 1)
}

func TestSweep_ALeaderThatHasNotBegunATermReportsNothing(t *testing.T) {
	f := newSweepFixture(t)
	announced := &fakeAnnouncements{first: true}
	sweep, logs := f.silentSweep(t, announced, threeMonitors, false)
	f.clock.Advance(time.Hour)

	require.NoError(t, sweep.SweepOnce(context.Background()))

	assert.Empty(t, f.eventsOffline())
	assert.Zero(t, announced.announcedCalls)
	assert.Empty(t, logs.All())
}

func (f *sweepFixture) eventsOffline() []probe.Event {
	return eventstest.Published(f.bus, probe.ProbeOffline)
}
