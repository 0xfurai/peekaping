package probe_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// clock is a hand-set clock for the sweep
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// sweepFixture is a sweep over a migrated SQLite database, a bus that records what it publishes,
// and a clock that stands still until the test moves it
type sweepFixture struct {
	db    *bun.DB
	repo  probe.Repository
	bus   *eventstest.Bus
	clock *clock
	sweep *probe.Sweep
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	db := sqliteDB(t)
	migrateDB(t, db)
	f := &sweepFixture{
		db:    db,
		repo:  probe.NewSQLRepository(db),
		bus:   eventstest.NewBus(),
		clock: &clock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
	}
	f.sweep = probe.NewSweep(probe.SweepConfig{
		Repo:         f.repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		// Up for a long time: the restart grace has run out
		StartedAt: f.clock.Now().Add(-24 * time.Hour),
		Now:       f.clock.Now,
		Logger:    zap.NewNop().Sugar(),
	})
	return f
}

// probe makes a probe that is in the given status
func (f *sweepFixture) probe(t *testing.T, name string, status probe.Status) *probe.Model {
	t.Helper()
	created, err := f.repo.Create(context.Background(), &probe.CreateModel{Name: name, TokenHash: "sha256$" + name, TokenPrefix: "pkp_" + name})
	require.NoError(t, err)
	if status == probe.StatusOnline {
		changed, err := f.repo.UpdateStatus(context.Background(), created.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		require.True(t, changed)
	}
	return created
}

// instance records an instance of the probe that was last seen the given time before now
func (f *sweepFixture) instance(t *testing.T, probeID, id string, seenAgo time.Duration) time.Time {
	t.Helper()
	seen := f.clock.Now().Add(-seenAgo)
	require.NoError(t, f.repo.UpsertInstance(context.Background(), probeID, &probe.Instance{ID: id, LastSeenAt: seen}))
	return seen
}

func (f *sweepFixture) run(t *testing.T) {
	t.Helper()
	require.NoError(t, f.sweep.SweepOnce(context.Background()))
}

func (f *sweepFixture) status(t *testing.T, probeID string) probe.Status {
	t.Helper()
	found, err := f.repo.FindByID(context.Background(), probeID)
	require.NoError(t, err)
	require.NotNil(t, found)
	return found.Status
}

func TestSweep_MarksOfflineAfterThreshold(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	seen := f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	f.instance(t, quiet.ID, "frankfurt-2", 5*time.Minute)
	talking := f.probe(t, "Paris", probe.StatusOnline)
	f.instance(t, talking.ID, "paris-1", 2*time.Minute)

	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, quiet.ID))
	assert.Equal(t, probe.StatusOnline, f.status(t, talking.ID), "an instance was seen within 3 minutes")
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.Equal(t, quiet.ID, offline[0].ProbeID)
	assert.Equal(t, "Frankfurt", offline[0].ProbeName)
	assert.True(t, seen.Equal(offline[0].Since), "since is the last time any instance was seen: %s", offline[0].Since)
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))

	f.run(t)
	assert.Len(t, eventstest.Published(f.bus, probe.ProbeOffline), 1, "a probe is announced offline once")
}

func TestSweep_AProbeSeenExactlyOfflineAfterAgoIsStillOnline(t *testing.T) {
	f := newSweepFixture(t)
	edge := f.probe(t, "Edge", probe.StatusOnline)
	f.instance(t, edge.ID, "edge-1", 3*time.Minute)

	f.run(t)
	assert.Equal(t, probe.StatusOnline, f.status(t, edge.ID), "within PROBE_OFFLINE_AFTER includes the limit itself")

	f.clock.Advance(time.Second)
	f.run(t)
	assert.Equal(t, probe.StatusOffline, f.status(t, edge.ID))
}

// A probe that restarts gracefully says bye and then hello again, with a new instance id, a few
// seconds later. Bye retires the instance and does not delete it, so the sweep, which runs in
// between, finds the probe's only instance last seen a minute ago: well inside OfflineAfter. The
// probe stays online and no one is told.
func TestSweep_GracefulRestartRaisesNoNotification(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	edge := f.probe(t, "Edge", probe.StatusOnline)
	f.instance(t, edge.ID, "edge-1", 0)
	f.run(t)
	bye := f.clock.Now()
	require.NoError(t, f.repo.RetireInstance(ctx, edge.ID, "edge-1", probe.RetiredAt(bye), bye))

	f.clock.Advance(20 * time.Second)
	f.run(t)

	assert.Equal(t, probe.StatusOnline, f.status(t, edge.ID), "20 seconds after bye is inside OfflineAfter")
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
	left, err := f.repo.FindInstances(ctx, edge.ID)
	require.NoError(t, err)
	require.Len(t, left, 1, "the retired instance stays until it has been silent for a day")

	// The probe never says hello again: the threshold counted from the retired time all along,
	// and the probe went offline at the bye
	f.clock.Advance(probe.DefaultOfflineAfter)
	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, edge.ID))
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.True(t, bye.Equal(offline[0].Since), "offline since the bye: %s", offline[0].Since)
}

func TestSweep_ARestartThatSaysHelloAgainIsNotAnOutage(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	edge := f.probe(t, "Edge", probe.StatusOnline)
	f.instance(t, edge.ID, "edge-1", 0)
	f.run(t)
	require.NoError(t, f.repo.RetireInstance(ctx, edge.ID, "edge-1", probe.RetiredAt(f.clock.Now()), f.clock.Now()))

	f.clock.Advance(5 * time.Second)
	f.instance(t, edge.ID, "edge-2", 0)
	f.clock.Advance(15 * time.Second)
	f.run(t)
	f.clock.Advance(2 * time.Minute)
	f.instance(t, edge.ID, "edge-2", 0)
	f.run(t)

	assert.Equal(t, probe.StatusOnline, f.status(t, edge.ID))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
}

// A probe is online with no instance only when its instances were pruned: the server was down for
// longer than InstanceRetention, and the sweep forgot them in its first pass. A bye does not do it.
func TestSweep_MarksAnOnlineProbeWithNoInstancesOffline(t *testing.T) {
	f := newSweepFixture(t)
	gone := f.probe(t, "Gone", probe.StatusOnline)

	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, gone.ID))
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.True(t, f.clock.Now().Equal(offline[0].Since), "with no instance to have seen, the probe has been offline since now: %s", offline[0].Since)
}

// After an outage longer than PROBE_OFFLINE_AFTER every instance looks quiet, but the probes could
// not reach the server: they get OfflineAfter, from the start, to reconnect
func TestSweep_WaitsOfflineAfterBeforeMarkingOffline(t *testing.T) {
	f := newSweepFixture(t)
	f.sweep = probe.NewSweep(probe.SweepConfig{
		Repo:         f.repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		StartedAt:    f.clock.Now().Add(-10 * time.Second),
		Now:          f.clock.Now,
		Logger:       zap.NewNop().Sugar(),
	})
	quiet := f.probe(t, "Frankfurt", probe.StatusOnline)
	seen := f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)

	f.run(t)

	assert.Equal(t, probe.StatusOnline, f.status(t, quiet.ID), "10 seconds after the start, the probe has had no time to reconnect")
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))

	f.clock.Advance(3*time.Minute - 11*time.Second)
	f.run(t)
	assert.Equal(t, probe.StatusOnline, f.status(t, quiet.ID), "just under OfflineAfter after the start")

	f.clock.Advance(time.Second)
	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, quiet.ID), "OfflineAfter after the start, the probe is still quiet")
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.True(t, seen.Equal(offline[0].Since), "since is still the last time an instance was seen: %s", offline[0].Since)
}

func TestSweep_TheRestartGraceLeavesTheOtherWorkToTheSweep(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	f.sweep = probe.NewSweep(probe.SweepConfig{
		Repo:         f.repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		StartedAt:    f.clock.Now().Add(-10 * time.Second),
		Now:          f.clock.Now,
		Logger:       zap.NewNop().Sugar(),
	})
	back := f.probe(t, "Back", probe.StatusOffline)
	f.instance(t, back.ID, "back-1", 10*time.Second)
	edge := f.probe(t, "Edge", probe.StatusOnline)
	f.instance(t, edge.ID, "edge-1", 0)
	f.instance(t, edge.ID, "edge-2", 0)
	old := f.probe(t, "Old", probe.StatusOffline)
	f.instance(t, old.ID, "old-1", 48*time.Hour)

	f.run(t)
	f.clock.Advance(61 * time.Second)
	for probeID, instanceID := range map[string]string{edge.ID: "edge-1", back.ID: "back-1"} {
		touched, err := f.repo.TouchInstance(ctx, probeID, instanceID, f.clock.Now())
		require.NoError(t, err)
		require.True(t, touched)
	}
	f.run(t)

	assert.Equal(t, probe.StatusOnline, f.status(t, back.ID), "the backstop works during the grace")
	assert.Len(t, eventstest.Published(f.bus, probe.ProbeOnline), 1)
	changed := eventstest.Published(f.bus, probe.InstancesChanged)
	require.Len(t, changed, 1, "so does the comparison of the online instances")
	assert.Equal(t, edge.ID, changed[0].ProbeID)
	left, err := f.repo.FindInstances(ctx, old.ID)
	require.NoError(t, err)
	assert.Empty(t, left, "and the pruning")
}

// An admin who disables a probe expects it to go quiet, so no one is told. Its token is refused
// from then on, so it can't come back online by a request either.
func TestSweep_DisabledProbeGoesOfflineWithoutAnEvent(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	disabled := f.probe(t, "Retired", probe.StatusOnline)
	f.instance(t, disabled.ID, "retired-1", 4*time.Minute)
	off := true
	_, err := f.repo.Update(ctx, disabled.ID, &probe.UpdateModel{Disabled: &off})
	require.NoError(t, err)
	enabled := f.probe(t, "Frankfurt", probe.StatusOnline)
	f.instance(t, enabled.ID, "frankfurt-1", 4*time.Minute)

	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, disabled.ID), "its status still says it is not connected")
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1, "an enabled probe in the same state is announced")
	assert.Equal(t, enabled.ID, offline[0].ProbeID)
}

func TestSweep_DisabledProbeIsNotBroughtOnlineByTheBackstop(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	disabled := f.probe(t, "Retired", probe.StatusOffline)
	f.instance(t, disabled.ID, "retired-1", 10*time.Second)
	off := true
	_, err := f.repo.Update(ctx, disabled.ID, &probe.UpdateModel{Disabled: &off})
	require.NoError(t, err)

	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, disabled.ID))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
}

func TestSweep_NeverOnlineNeverNotifies(t *testing.T) {
	f := newSweepFixture(t)
	fresh := f.probe(t, "Fresh", probe.StatusOffline)
	stale := f.probe(t, "Stale", probe.StatusOffline)
	f.instance(t, stale.ID, "stale-1", time.Hour)

	f.run(t)
	f.run(t)

	assert.Equal(t, probe.StatusOffline, f.status(t, fresh.ID))
	assert.Equal(t, probe.StatusOffline, f.status(t, stale.ID))
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline), "the local probe, a new probe and a long gone one are offline already")
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOnline))
}

func TestSweep_BackstopMarksOnline(t *testing.T) {
	f := newSweepFixture(t)
	back := f.probe(t, "Back", probe.StatusOffline)
	seen := f.instance(t, back.ID, "back-1", 10*time.Second)

	f.run(t)

	assert.Equal(t, probe.StatusOnline, f.status(t, back.ID), "a request flips the probe at once; the sweep catches one that was missed")
	online := eventstest.Published(f.bus, probe.ProbeOnline)
	require.Len(t, online, 1)
	assert.Equal(t, back.ID, online[0].ProbeID)
	assert.Equal(t, "Back", online[0].ProbeName)
	assert.True(t, seen.Equal(online[0].Since), "since is when the probe was last heard: %s", online[0].Since)
	assert.Empty(t, eventstest.Published(f.bus, probe.ProbeOffline))

	f.run(t)
	assert.Len(t, eventstest.Published(f.bus, probe.ProbeOnline), 1, "a probe is announced online once")
}

func TestSweep_InstanceDropOutPublishesInstancesChanged(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	edge := f.probe(t, "Edge", probe.StatusOnline)
	f.instance(t, edge.ID, "edge-1", 0)
	f.instance(t, edge.ID, "edge-2", 0)
	steady := f.probe(t, "Steady", probe.StatusOnline)
	f.instance(t, steady.ID, "steady-1", 0)

	f.run(t)
	assert.Empty(t, eventstest.Published(f.bus, probe.InstancesChanged), "the first pass only learns who is online")

	// edge-1 keeps talking, edge-2 goes quiet
	f.clock.Advance(61 * time.Second)
	touched, err := f.repo.TouchInstance(ctx, edge.ID, "edge-1", f.clock.Now())
	require.NoError(t, err)
	require.True(t, touched)
	touched, err = f.repo.TouchInstance(ctx, steady.ID, "steady-1", f.clock.Now())
	require.NoError(t, err)
	require.True(t, touched)
	f.run(t)

	changed := eventstest.Published(f.bus, probe.InstancesChanged)
	require.Len(t, changed, 1)
	assert.Equal(t, edge.ID, changed[0].ProbeID)
	assert.Equal(t, probe.StatusOnline, f.status(t, edge.ID), "one instance is still in")

	f.run(t)
	assert.Len(t, eventstest.Published(f.bus, probe.InstancesChanged), 1, "nothing changed since")

	// edge-2 comes back
	f.clock.Advance(time.Second)
	touched, err = f.repo.TouchInstance(ctx, edge.ID, "edge-2", f.clock.Now())
	require.NoError(t, err)
	require.True(t, touched)
	f.run(t)
	assert.Len(t, eventstest.Published(f.bus, probe.InstancesChanged), 2, "an instance that joins the online set changes it too")
}

func TestSweep_RemovesInstancesUnseenForADay(t *testing.T) {
	ctx := context.Background()
	f := newSweepFixture(t)
	edge := f.probe(t, "Edge", probe.StatusOffline)
	f.instance(t, edge.ID, "edge-last-week", 7*24*time.Hour)
	f.instance(t, edge.ID, "edge-yesterday", 25*time.Hour)
	f.instance(t, edge.ID, "edge-this-morning", 23*time.Hour)

	f.run(t)

	left, err := f.repo.FindInstances(ctx, edge.ID)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, "edge-this-morning", left[0].ID)
}

func TestSweep_RunSweepsEveryIntervalUntilItsContextIsDone(t *testing.T) {
	f := newSweepFixture(t)
	quiet := f.probe(t, "Quiet", probe.StatusOnline)
	f.instance(t, quiet.ID, "quiet-1", time.Hour)
	f.sweep = probe.NewSweep(probe.SweepConfig{
		Repo:         f.repo,
		EventBus:     f.bus,
		OfflineAfter: 3 * time.Minute,
		Interval:     5 * time.Millisecond,
		StartedAt:    f.clock.Now().Add(-24 * time.Hour),
		Now:          f.clock.Now,
		Logger:       zap.NewNop().Sugar(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.sweep.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return f.status(t, quiet.ID) == probe.StatusOffline
	}, 5*time.Second, 5*time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return once its context was done")
	}
}

// history is the probe's status changes since the start of 2026, written as "status time"
func (f *sweepFixture) history(t *testing.T, probeID string) []string {
	t.Helper()
	changes, err := f.repo.FindStatusChanges(context.Background(), probeID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return described(changes)
}

// startHistory makes the probe's history what these tests need: the probe is in the status it was
// made in, since the given time. Its real creation row is replaced, since the sweep's clock is not
// the machine's.
func (f *sweepFixture) startHistory(t *testing.T, p *probe.Model, status probe.Status, since time.Time) {
	t.Helper()
	_, err := f.db.Exec("DELETE FROM probe_status_changes WHERE probe_id = ?", p.ID)
	require.NoError(t, err)
	if status == probe.StatusOnline {
		changed, err := f.repo.UpdateStatus(context.Background(), p.ID, probe.StatusOffline, probe.StatusOnline, since)
		require.NoError(t, err)
		require.True(t, changed)
	}
}

func TestSweep_RecordsTheOfflineChangeAtTheTimeTheInstancesWereLastSeen(t *testing.T) {
	f := newSweepFixture(t) // the clock reads 12:00:00 on 2026-10-22
	quiet := f.probe(t, "Frankfurt", probe.StatusOffline)
	f.startHistory(t, quiet, probe.StatusOnline, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	f.instance(t, quiet.ID, "frankfurt-1", 4*time.Minute)
	f.instance(t, quiet.ID, "frankfurt-2", 5*time.Minute)

	f.run(t)

	assert.Equal(t, []string{"online 08:00:00", "offline 11:56:00"}, f.history(t, quiet.ID), "the newest instance was seen at 11:56:00; the sweep ran at 12:00:00")
}

func TestSweep_RecordsTheOfflineChangeAtTheTimeOfTheSweepWhenTheProbeHasNoInstance(t *testing.T) {
	f := newSweepFixture(t)
	bare := f.probe(t, "Paris", probe.StatusOffline)
	f.startHistory(t, bare, probe.StatusOnline, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))

	f.run(t)

	assert.Equal(t, []string{"online 08:00:00", "offline 12:00:00"}, f.history(t, bare.ID))
}

func TestSweep_RecordsTheOnlineChangeAtTheTimeTheInstanceWasLastSeen(t *testing.T) {
	f := newSweepFixture(t)
	back := f.probe(t, "Frankfurt", probe.StatusOffline)
	f.startHistory(t, back, probe.StatusOffline, time.Time{})
	f.instance(t, back.ID, "frankfurt-1", 30*time.Second)

	f.run(t)

	assert.Equal(t, []string{"online 11:59:30"}, f.history(t, back.ID))
}

func TestSweep_RecordsNothingForAProbeWhoseStatusDoesNotChange(t *testing.T) {
	f := newSweepFixture(t)
	steady := f.probe(t, "Paris", probe.StatusOffline)
	f.startHistory(t, steady, probe.StatusOnline, time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	f.instance(t, steady.ID, "paris-1", 2*time.Minute)

	f.run(t)
	f.run(t)

	assert.Equal(t, []string{"online 08:00:00"}, f.history(t, steady.ID))
}

func TestSweep_RecordsTheOfflineChangeAtTheByeOfAnInstanceThatRanLessThanAMinute(t *testing.T) {
	f := newSweepFixture(t) // the clock reads 12:00:00 on 2026-10-22
	brief := f.probe(t, "Frankfurt", probe.StatusOffline)
	f.startHistory(t, brief, probe.StatusOnline, time.Date(2026, 10, 22, 11, 50, 0, 0, time.UTC))
	f.instance(t, brief.ID, "frankfurt-1", 10*time.Minute)
	bye := time.Date(2026, 10, 22, 11, 50, 20, 0, time.UTC)
	// A bye stores a last seen time a minute before it, which is before the probe came online
	require.NoError(t, f.repo.RetireInstance(context.Background(), brief.ID, "frankfurt-1", probe.RetiredAt(bye), bye))

	f.run(t)

	assert.Equal(t, []string{"online 11:50:00", "offline 11:50:20"}, f.history(t, brief.ID), "it went offline at its bye, after the 20 seconds it was online")
	offline := eventstest.Published(f.bus, probe.ProbeOffline)
	require.Len(t, offline, 1)
	assert.Equal(t, bye, offline[0].Since, "the notification says the same")
}
