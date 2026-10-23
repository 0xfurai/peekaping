package stale

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fakeMonitors is a monitor database: active ones are paged, any can be found by ID
type fakeMonitors struct {
	active []*shared.Monitor
	byID   map[string]*shared.Monitor
}

func (m *fakeMonitors) FindActivePaginated(_ context.Context, page, _ int) ([]*shared.Monitor, error) {
	if page == 0 {
		return m.active, nil
	}
	return nil, nil
}

func (m *fakeMonitors) FindByID(_ context.Context, id string) (*shared.Monitor, error) {
	return m.byID[id], nil
}

// fakeHeartbeats returns each monitor's latest heartbeat
type fakeHeartbeats struct {
	mu     sync.Mutex
	latest map[string]time.Time
}

func (s *fakeHeartbeats) setLatest(monitorID string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[monitorID] = t
}

func (s *fakeHeartbeats) FindByMonitorIDPaginated(_ context.Context, monitorID string, _, _ int, _ *bool, _ bool) ([]*shared.HeartBeatModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.latest[monitorID]
	if !ok {
		return []*shared.HeartBeatModel{}, nil
	}
	return []*shared.HeartBeatModel{{MonitorID: monitorID, Time: t}}, nil
}

// fakeRechecker records the monitors it was asked to check right away
type fakeRechecker struct {
	calls []string
	err   error
}

func (r *fakeRechecker) RunNow(_ context.Context, monitorID string) error {
	r.calls = append(r.calls, monitorID)
	return r.err
}

type watchdogFixture struct {
	w          *Watchdog
	rdb        *redis.Client // set when the store is Redis
	monitors   *fakeMonitors
	heartbeats *fakeHeartbeats
	bus        *eventstest.Bus
	rechecker  *fakeRechecker
	store      Store
	start      time.Time
	clock      time.Time
}

func newWatchdogFixture(t *testing.T, monitors ...*shared.Monitor) *watchdogFixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	f := newWatchdogFixtureOn(t, NewRedisStore(rdb), monitors...)
	f.rdb = rdb
	return f
}

func newWatchdogFixtureOn(t *testing.T, store Store, monitors ...*shared.Monitor) *watchdogFixture {
	t.Helper()
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := &watchdogFixture{
		monitors:   &fakeMonitors{active: monitors, byID: map[string]*shared.Monitor{}},
		heartbeats: &fakeHeartbeats{latest: map[string]time.Time{}},
		bus:        eventstest.NewBus(),
		rechecker:  &fakeRechecker{},
		store:      store,
		start:      start,
		clock:      start,
	}
	f.w = NewWatchdog(WatchdogConfig{
		Store:      f.store,
		Rechecker:  f.rechecker,
		Monitors:   f.monitors,
		Heartbeats: f.heartbeats,
		EventBus:   f.bus,
		// Stands in for the worker's task timeout: the check's timeout plus 10s
		CheckTimeout: func(timeoutSeconds int) time.Duration {
			return time.Duration(timeoutSeconds)*time.Second + 10*time.Second
		},
		Now:    func() time.Time { return f.clock },
		Logger: zap.NewNop().Sugar(),
	})
	return f
}

// at moves the clock to the given offset from the fixture's start
func (f *watchdogFixture) at(offset time.Duration) {
	f.clock = f.start.Add(offset)
}

func (f *watchdogFixture) run(t *testing.T) {
	t.Helper()
	require.NoError(t, f.w.checkStaleMonitors(context.Background()))
}

func (f *watchdogFixture) published(topic events.Topic[Event]) []Event {
	return eventstest.Published(f.bus, topic)
}

func testMonitor(id string, interval, timeout int, updatedAt time.Time) *shared.Monitor {
	return &shared.Monitor{ID: id, Name: "Monitor " + id, Type: "http", Interval: interval, Timeout: timeout, Active: true, UpdatedAt: updatedAt}
}

func TestThreshold(t *testing.T) {
	tests := []struct {
		name              string
		interval, timeout int
		want              time.Duration
	}{
		{"three intervals win", 60, 48, 180 * time.Second},
		{"interval plus timeout plus a minute win", 20, 48, 128 * time.Second},
		{"long interval", 300, 10, 900 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Threshold(tt.interval, tt.timeout))
		})
	}
}

func TestWatchdog_FindsMonitorsThatStoppedBeingChecked(t *testing.T) {
	longAgo := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	silent := testMonitor("silent", 60, 10, longAgo) // threshold 180s, last heartbeat 4 min ago
	fresh := testMonitor("fresh", 60, 10, longAgo)   // last heartbeat 1 min ago
	neverChecked := testMonitor("never", 60, 10, now.Add(-10*time.Minute))
	resumed := testMonitor("resumed", 60, 10, now.Add(-30*time.Second)) // old heartbeat, resumed 30s ago
	paused := testMonitor("paused", 60, 10, longAgo)
	paused.Active = false
	slow := testMonitor("slow", 20, 48, longAgo) // threshold 128s, last heartbeat 2 min ago
	push := testMonitor("push", 60, 10, longAgo)
	push.Type = "push"

	f := newWatchdogFixture(t, silent, fresh, neverChecked, resumed, paused, slow, push)
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))
	f.heartbeats.setLatest("fresh", now.Add(-time.Minute))
	f.heartbeats.setLatest("resumed", now.Add(-time.Hour))
	f.heartbeats.setLatest("paused", now.Add(-time.Hour))
	f.heartbeats.setLatest("slow", now.Add(-2*time.Minute))
	f.heartbeats.setLatest("push", now.Add(-5*time.Minute))

	f.run(t)

	entries, err := f.store.All(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"silent", "never", "push"}, keys(entries))
	assert.Equal(t, now.Add(-4*time.Minute), entries["silent"].Since.UTC())
	assert.Equal(t, now.Add(-10*time.Minute), entries["never"].Since.UTC(), "a monitor with no heartbeat is stale since it was saved")
	assert.Equal(t, now, entries["silent"].DetectedAt.UTC())
	assert.False(t, entries["silent"].Notified)

	// A stale monitor gets a check right away; one that's being checked doesn't
	assert.ElementsMatch(t, []string{"silent", "never", "push"}, f.rechecker.calls)

	assert.Empty(t, f.published(MonitorStale), "no notification before the check it asked for had its chance")

	ranAt, err := f.store.RanAt(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ranAt)
	assert.Equal(t, now, *ranAt)
}

func TestWatchdog_NotifiesOnceWhenStillStaleAfterTheRecheck(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)

	sent := f.published(MonitorStale)
	require.Len(t, sent, 1)
	assert.Equal(t, Event{MonitorID: "silent", MonitorName: "Monitor silent", Since: now.Add(-4 * time.Minute)}, normalize(sent[0]))

	f.at(2 * time.Minute)
	f.run(t)
	f.at(3 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(MonitorStale), 1, "one notification per stale period")
	assert.Equal(t, []string{"silent"}, f.rechecker.calls, "one recheck per stale period")
}

func TestWatchdog_WaitsForASlowRecheck(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	// The recheck can take up to 50s + 10s = 60s, plus the ingest
	f := newWatchdogFixture(t, testMonitor("slow", 60, 50, now.Add(-time.Hour)))
	f.heartbeats.setLatest("slow", now.Add(-5*time.Minute))

	f.run(t)
	f.at(50 * time.Second)
	f.run(t)
	assert.Empty(t, f.published(MonitorStale))

	f.at(2 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(MonitorStale), 1)
}

func TestWatchdog_ReportsRecoveryAfterANotification(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)
	require.Len(t, f.published(MonitorStale), 1)

	f.heartbeats.setLatest("silent", now.Add(90*time.Second))
	f.at(2 * time.Minute)
	f.run(t)

	recovered := f.published(MonitorStaleRecovered)
	require.Len(t, recovered, 1)
	assert.Equal(t, "silent", recovered[0].MonitorID)
	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestWatchdog_RecoveryBeforeANotificationIsQuiet(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.heartbeats.setLatest("silent", now.Add(5*time.Second)) // the recheck worked
	f.at(time.Minute)
	f.run(t)

	assert.Empty(t, f.published(MonitorStale))
	assert.Empty(t, f.published(MonitorStaleRecovered))
	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestWatchdog_ForgetsMonitorsThatWerePausedOrDeleted(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t)
	paused := testMonitor("gone", 60, 10, now.Add(-time.Hour))
	paused.Active = false
	f.monitors.byID["gone"] = paused
	require.NoError(t, f.store.Put(context.Background(), Entry{MonitorID: "gone", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute), Notified: true}))

	f.run(t)

	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Empty(t, f.published(MonitorStaleRecovered), "a paused monitor wasn't checked again")
}

func TestWatchdog_SavingANotifiedMonitorIsNotARecovery(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	mon := testMonitor("silent", 60, 10, now.Add(-time.Hour))
	f := newWatchdogFixture(t, mon)
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)
	require.Len(t, f.published(MonitorStale), 1)

	// Renamed while still broken: updated_at moves, no heartbeat arrives
	mon.UpdatedAt = now.Add(90 * time.Second)
	for _, offset := range []time.Duration{2 * time.Minute, 5 * time.Minute, 10 * time.Minute} {
		f.at(offset)
		f.run(t)
	}
	assert.Empty(t, f.published(MonitorStaleRecovered), "no check ran, so it isn't checked again")
	assert.Len(t, f.published(MonitorStale), 1, "still the same stale period")
	entries, err := f.store.All(context.Background())
	require.NoError(t, err)
	assert.Equal(t, now.Add(-4*time.Minute), entries["silent"].Since.UTC())

	f.heartbeats.setLatest("silent", now.Add(10*time.Minute+30*time.Second))
	f.at(11 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(MonitorStaleRecovered), 1)
}

func TestWatchdog_KeepsRecordsOfActiveMonitorsMissedByTheScan(t *testing.T) {
	// A monitor paused mid-scan shifts the pages, so an active monitor can be missed
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	missed := testMonitor("missed", 60, 10, now.Add(-time.Hour))
	f := newWatchdogFixture(t)
	f.monitors.byID["missed"] = missed
	ctx := context.Background()
	require.NoError(t, f.store.Put(ctx, Entry{MonitorID: "missed", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute), Notified: true}))
	require.NoError(t, f.store.Put(ctx, Entry{MonitorID: "deleted", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute)}))

	f.run(t)

	entries, err := f.store.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"missed"}, keys(entries))
	assert.True(t, entries["missed"].Notified)
}

func TestWatchdog_TriesAgainWhenTheRecheckFails(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))
	f.rechecker.err = errors.New("redis is down")

	f.run(t)

	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count, "a monitor that couldn't be rechecked isn't recorded as stale")

	f.rechecker.err = nil
	f.at(time.Minute)
	f.run(t)

	assert.Equal(t, []string{"silent", "silent"}, f.rechecker.calls)
	count, err = f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestWatchdog_RunLooksEveryIntervalUntilTheContextIsDone(t *testing.T) {
	f := newWatchdogFixture(t)
	f.w.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		f.w.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool {
		ranAt, _ := f.store.RanAt(context.Background())
		return ranAt != nil
	}, 2*time.Second, 5*time.Millisecond, "the watchdog never ran")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watchdog kept running after its context was done")
	}

	require.NoError(t, f.rdb.Del(context.Background(), RanAtKey).Err())
	time.Sleep(50 * time.Millisecond)
	ranAt, err := f.store.RanAt(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ranAt, "the watchdog stopped with its context")
}

// stopsOnRead is a monitor database and heartbeat store that fail with the context's error, as a
// driver does when the context is done during a query, the first time they are read: it makes the
// context done. A watchdog stopped by its context finds that in the middle of a run.
type stopsOnRead struct {
	fakeMonitors
	cancel         context.CancelFunc
	failOnMonitors bool // fail the page read; otherwise the heartbeat read
	heartbeatReads int
}

func (s *stopsOnRead) FindActivePaginated(ctx context.Context, page, limit int) ([]*shared.Monitor, error) {
	if s.failOnMonitors {
		s.cancel()
		return nil, ctx.Err()
	}
	return s.fakeMonitors.FindActivePaginated(ctx, page, limit)
}

func (s *stopsOnRead) FindByMonitorIDPaginated(ctx context.Context, _ string, _, _ int, _ *bool, _ bool) ([]*shared.HeartBeatModel, error) {
	s.heartbeatReads++
	s.cancel()
	return nil, ctx.Err()
}

func TestWatchdog_RunLogsNoErrorWhenItsContextIsDoneDuringARun(t *testing.T) {
	for name, failOnMonitors := range map[string]bool{
		"while it reads the monitors":  true,
		"while it checks each monitor": false,
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			f := newWatchdogFixture(t, testMonitor("a", 60, 10, now), testMonitor("b", 60, 10, now))
			core, logs := observer.New(zap.DebugLevel)
			f.w.logger = zap.New(core).Sugar()
			f.w.interval = 5 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			reads := &stopsOnRead{fakeMonitors: fakeMonitors{active: f.monitors.active}, cancel: cancel, failOnMonitors: failOnMonitors}
			f.w.monitors = reads
			f.w.heartbeats = reads

			done := make(chan struct{})
			go func() {
				f.w.Run(ctx)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("the watchdog kept running after its context was done")
			}

			assert.Empty(t, logs.FilterLevelExact(zap.ErrorLevel).All())
			if !failOnMonitors {
				assert.Equal(t, 1, reads.heartbeatReads, "the run ended with the context, not after checking each monitor")
			}
		})
	}
}

func TestWatchdog_KeepsItsStaleMonitorsInAMemoryStore(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	f := newWatchdogFixtureOn(t, store, testMonitor("silent", 60, 10, now.Add(-time.Hour)), testMonitor("fine", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))
	f.heartbeats.setLatest("fine", now.Add(-time.Minute))
	ctx := context.Background()

	f.run(t)

	entries, err := store.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"silent"}, keys(entries))
	assert.Equal(t, []string{"silent"}, f.rechecker.calls, "the stale monitor is asked to be checked again")
	ranAt, err := store.RanAt(ctx)
	require.NoError(t, err)
	require.NotNil(t, ranAt)
	assert.Equal(t, now, *ranAt)

	f.at(time.Minute)
	f.run(t)
	require.Len(t, f.published(MonitorStale), 1)
	entries, err = store.All(ctx)
	require.NoError(t, err)
	assert.True(t, entries["silent"].Notified)

	f.heartbeats.setLatest("silent", now.Add(90*time.Second))
	f.at(2 * time.Minute)
	f.run(t)

	assert.Len(t, f.published(MonitorStaleRecovered), 1)
	count, err := store.Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func keys(m map[string]Entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func normalize(e Event) Event {
	e.Since = e.Since.UTC()
	return e
}
