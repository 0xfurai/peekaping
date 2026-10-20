package producer

import (
	"context"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/stale"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeHeartbeatService returns each monitor's latest heartbeat
type fakeHeartbeatService struct {
	heartbeat.Service
	mu     sync.Mutex
	latest map[string]time.Time
}

func (s *fakeHeartbeatService) setLatest(monitorID string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[monitorID] = t
}

func (s *fakeHeartbeatService) FindByMonitorIDPaginated(_ context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.latest[monitorID]
	if !ok {
		return []*heartbeat.Model{}, nil
	}
	return []*heartbeat.Model{{MonitorID: monitorID, Time: t}}, nil
}

type watchdogFixture struct {
	p          *Producer
	mr         *miniredis.Miniredis
	rdb        *redis.Client
	monitors   *MockMonitorService
	heartbeats *fakeHeartbeatService
	bus        *eventstest.Bus
	store      *stale.Store
	now        time.Time
}

func newWatchdogFixture(t *testing.T, monitors ...*monitor.Model) *watchdogFixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	f := &watchdogFixture{
		mr:         mr,
		rdb:        rdb,
		monitors:   new(MockMonitorService),
		heartbeats: &fakeHeartbeatService{latest: map[string]time.Time{}},
		bus:        eventstest.NewBus(),
		store:      stale.NewStore(rdb),
		now:        time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	}
	f.monitors.On("FindActivePaginated", mock.Anything, 0, mock.Anything).Return(monitors, nil)
	f.monitors.On("FindActivePaginated", mock.Anything, mock.Anything, mock.Anything).Return([]*monitor.Model{}, nil)
	f.mr.SetTime(f.now)

	f.p = &Producer{
		rdb:              rdb,
		monitorService:   f.monitors,
		heartbeatService: f.heartbeats,
		eventBus:         f.bus,
		staleStore:       f.store,
		logger:           zap.NewNop().Sugar(),
		ctx:              context.Background(),
		monitorIntervals: map[string]int{},
		watchdogInterval: time.Minute,
	}
	return f
}

// at moves the clock to the given offset from the fixture's start
func (f *watchdogFixture) at(offset time.Duration) {
	f.mr.SetTime(f.now.Add(offset))
}

func (f *watchdogFixture) run(t *testing.T) {
	t.Helper()
	require.NoError(t, f.p.checkStaleMonitors(context.Background()))
}

func (f *watchdogFixture) published(topic events.Topic[stale.Event]) []stale.Event {
	return eventstest.Published(f.bus, topic)
}

func testMonitor(id string, interval, timeout int, updatedAt time.Time) *monitor.Model {
	return &monitor.Model{ID: id, Name: "Monitor " + id, Type: "http", Interval: interval, Timeout: timeout, Active: true, UpdatedAt: updatedAt}
}

func TestStaleThreshold(t *testing.T) {
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
			assert.Equal(t, tt.want, stale.Threshold(tt.interval, tt.timeout))
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

	// A stale monitor gets a check right away
	for _, id := range []string{"silent", "never", "push"} {
		score, err := f.rdb.ZScore(context.Background(), SchedDueKey, id).Result()
		require.NoError(t, err, id)
		assert.Equal(t, float64(now.UnixMilli()), score, id)
	}
	_, err = f.rdb.ZScore(context.Background(), SchedDueKey, "fresh").Result()
	assert.ErrorIs(t, err, redis.Nil, "a monitor that's being checked isn't re-enqueued")

	assert.Empty(t, f.published(stale.MonitorStale), "no notification before the re-enqueued check had its chance")

	ranAt, err := f.store.RanAt(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ranAt)
	assert.Equal(t, now, *ranAt)
}

func TestWatchdog_NotifiesOnceWhenStillStaleAfterTheReenqueue(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)

	sent := f.published(stale.MonitorStale)
	require.Len(t, sent, 1)
	assert.Equal(t, stale.Event{MonitorID: "silent", MonitorName: "Monitor silent", Since: now.Add(-4 * time.Minute)}, normalize(sent[0]))

	f.at(2 * time.Minute)
	f.run(t)
	f.at(3 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(stale.MonitorStale), 1, "one notification per stale period")
}

func TestWatchdog_WaitsForASlowReenqueuedCheck(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	// The re-enqueued check can take up to 50s + 2s + 8s = 60s, plus the ingest
	f := newWatchdogFixture(t, testMonitor("slow", 60, 50, now.Add(-time.Hour)))
	f.heartbeats.setLatest("slow", now.Add(-5*time.Minute))

	f.run(t)
	f.at(50 * time.Second)
	f.run(t)
	assert.Empty(t, f.published(stale.MonitorStale))

	f.at(2 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(stale.MonitorStale), 1)
}

func TestWatchdog_ReportsRecoveryAfterANotification(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t, testMonitor("silent", 60, 10, now.Add(-time.Hour)))
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)
	require.Len(t, f.published(stale.MonitorStale), 1)

	f.heartbeats.setLatest("silent", now.Add(90*time.Second))
	f.at(2 * time.Minute)
	f.run(t)

	recovered := f.published(stale.MonitorStaleRecovered)
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
	f.heartbeats.setLatest("silent", now.Add(5*time.Second)) // the re-enqueued check worked
	f.at(time.Minute)
	f.run(t)

	assert.Empty(t, f.published(stale.MonitorStale))
	assert.Empty(t, f.published(stale.MonitorStaleRecovered))
	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestWatchdog_ForgetsMonitorsThatWerePausedOrDeleted(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f := newWatchdogFixture(t)
	paused := testMonitor("gone", 60, 10, now.Add(-time.Hour))
	paused.Active = false
	f.monitors.On("FindByID", mock.Anything, "gone").Return(paused, nil)
	require.NoError(t, f.store.Put(context.Background(), stale.Entry{MonitorID: "gone", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute), Notified: true}))

	f.run(t)

	count, err := f.store.Count(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Empty(t, f.published(stale.MonitorStaleRecovered), "a paused monitor wasn't checked again")
}

func TestWatchdog_SavingANotifiedMonitorIsNotARecovery(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	mon := testMonitor("silent", 60, 10, now.Add(-time.Hour))
	f := newWatchdogFixture(t, mon)
	f.heartbeats.setLatest("silent", now.Add(-4*time.Minute))

	f.run(t)
	f.at(time.Minute)
	f.run(t)
	require.Len(t, f.published(stale.MonitorStale), 1)

	// Renamed while still broken: updated_at moves, no heartbeat arrives
	mon.UpdatedAt = now.Add(90 * time.Second)
	for _, offset := range []time.Duration{2 * time.Minute, 5 * time.Minute, 10 * time.Minute} {
		f.at(offset)
		f.run(t)
	}
	assert.Empty(t, f.published(stale.MonitorStaleRecovered), "no check ran, so it isn't checked again")
	assert.Len(t, f.published(stale.MonitorStale), 1, "still the same stale period")
	entries, err := f.store.All(context.Background())
	require.NoError(t, err)
	assert.Equal(t, now.Add(-4*time.Minute), entries["silent"].Since.UTC())

	f.heartbeats.setLatest("silent", now.Add(10*time.Minute+30*time.Second))
	f.at(11 * time.Minute)
	f.run(t)
	assert.Len(t, f.published(stale.MonitorStaleRecovered), 1)
}

func TestWatchdog_KeepsRecordsOfActiveMonitorsMissedByTheScan(t *testing.T) {
	// A monitor paused mid-scan shifts the pages, so an active monitor can be missed
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	missed := testMonitor("missed", 60, 10, now.Add(-time.Hour))
	f := newWatchdogFixture(t)
	f.monitors.On("FindByID", mock.Anything, "missed").Return(missed, nil)
	f.monitors.On("FindByID", mock.Anything, "deleted").Return(nil, nil)
	ctx := context.Background()
	require.NoError(t, f.store.Put(ctx, stale.Entry{MonitorID: "missed", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute), Notified: true}))
	require.NoError(t, f.store.Put(ctx, stale.Entry{MonitorID: "deleted", Since: now.Add(-time.Hour), DetectedAt: now.Add(-time.Minute)}))

	f.run(t)

	entries, err := f.store.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"missed"}, keys(entries))
	assert.True(t, entries["missed"].Notified)
}

func TestWatchdog_RunsOnlyWhileLeader(t *testing.T) {
	f := newWatchdogFixture(t)
	f.p.watchdogInterval = 20 * time.Millisecond
	f.p.ctx, f.p.cancel = context.WithCancel(context.Background())
	defer f.p.cancel()
	f.p.scheduleRefreshInterval = time.Hour

	require.NoError(t, f.p.startMonitorSyncing())
	require.Eventually(t, func() bool {
		ranAt, _ := f.store.RanAt(context.Background())
		return ranAt != nil
	}, 2*time.Second, 10*time.Millisecond)

	f.p.stopMonitorSyncing()
	f.p.wg.Wait()
	require.NoError(t, f.rdb.Del(context.Background(), stale.RanAtKey).Err())
	time.Sleep(100 * time.Millisecond)
	ranAt, err := f.store.RanAt(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ranAt, "the watchdog stopped with leadership")
}

func keys(m map[string]stale.Entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func normalize(e stale.Event) stale.Event {
	e.Since = e.Since.UTC()
	return e
}
