package stale

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

var errBoom = errors.New("boom")

// flakyStore is a store that fails the calls it is told to, and keeps the calls it gets
type flakyStore struct {
	Store
	allErr, setRanAtErr error
	// putErr and deleteErr fail a call that names the monitor
	putErr, deleteErr map[string]error
	deletes           [][]string
	ranAtSet          bool
}

func (s *flakyStore) All(ctx context.Context) (map[string]Entry, error) {
	if s.allErr != nil {
		return nil, s.allErr
	}
	return s.Store.All(ctx)
}

func (s *flakyStore) Put(ctx context.Context, e Entry) error {
	if err := s.putErr[e.MonitorID]; err != nil {
		return err
	}
	return s.Store.Put(ctx, e)
}

func (s *flakyStore) Delete(ctx context.Context, ids ...string) error {
	s.deletes = append(s.deletes, slices.Clone(ids))
	for _, id := range ids {
		if err := s.deleteErr[id]; err != nil {
			return err
		}
	}
	return s.Store.Delete(ctx, ids...)
}

func (s *flakyStore) SetRanAt(ctx context.Context, t time.Time) error {
	if s.setRanAtErr != nil {
		return s.setRanAtErr
	}
	s.ranAtSet = true
	return s.Store.SetRanAt(ctx, t)
}

// flakyMonitors pages the monitors it is given, one page each, and fails the reads it is told to
type flakyMonitors struct {
	pages   [][]*shared.Monitor
	byID    map[string]*shared.Monitor
	pageErr map[int]error
	idErr   map[string]error
	// onIDErr runs when a read by id fails
	onIDErr func()
	asked   []int
}

func (m *flakyMonitors) FindActivePaginatedAnyWorkspace(_ context.Context, page, _ int) ([]*shared.Monitor, error) {
	m.asked = append(m.asked, page)
	if err := m.pageErr[page]; err != nil {
		return nil, err
	}
	if page < len(m.pages) {
		return m.pages[page], nil
	}
	return nil, nil
}

func (m *flakyMonitors) FindByIDAnyWorkspace(_ context.Context, id string) (*shared.Monitor, error) {
	if err := m.idErr[id]; err != nil {
		if m.onIDErr != nil {
			m.onIDErr()
		}
		return nil, err
	}
	return m.byID[id], nil
}

// flakyHeartbeats fails the read of the heartbeats of the monitors it is told to
type flakyHeartbeats struct {
	*fakeHeartbeats
	errFor map[string]error
}

func (h *flakyHeartbeats) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*shared.HeartBeatModel, error) {
	if err := h.errFor[monitorID]; err != nil {
		return nil, err
	}
	return h.fakeHeartbeats.FindByMonitorIDPaginated(ctx, monitorID, limit, page, important, reverse)
}

// flakyFixture is a watchdog fixture over a flaky store, flaky monitors and a logger that keeps
// what it is told
type flakyFixture struct {
	*watchdogFixture
	flaky    *flakyStore
	monitors *flakyMonitors
	logs     *observer.ObservedLogs
}

func newFlakyFixture(t *testing.T, monitors ...*shared.Monitor) *flakyFixture {
	t.Helper()
	flaky := &flakyStore{Store: NewMemoryStore(), putErr: map[string]error{}, deleteErr: map[string]error{}}
	f := newWatchdogFixtureOn(t, flaky)
	fm := &flakyMonitors{pages: [][]*shared.Monitor{monitors}, byID: map[string]*shared.Monitor{}, pageErr: map[int]error{}, idErr: map[string]error{}}
	core, logs := observer.New(zap.DebugLevel)
	f.w.monitors = fm
	f.w.logger = zap.New(core).Sugar()
	return &flakyFixture{watchdogFixture: f, flaky: flaky, monitors: fm, logs: logs}
}

// errorsLogged are the messages logged at error level, with the error each carries
func (f *flakyFixture) errorsLogged() []string {
	var got []string
	for _, entry := range f.logs.FilterLevelExact(zap.ErrorLevel).All() {
		got = append(got, entry.Message+": "+entry.ContextMap()["error"].(string))
	}
	return got
}

func (f *flakyFixture) entries(t *testing.T) map[string]Entry {
	t.Helper()
	entries, err := f.flaky.Store.All(context.Background())
	require.NoError(t, err)
	return entries
}

func (f *flakyFixture) seed(t *testing.T, e Entry) {
	t.Helper()
	require.NoError(t, f.flaky.Store.Put(context.Background(), e))
}

var failuresNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// staleMonitor is a monitor that was saved long ago and last checked four minutes ago, which is
// past its three minute threshold
func staleMonitor(id string) *shared.Monitor {
	return testMonitor(id, 60, 10, failuresNow.Add(-time.Hour))
}

func TestWatchdog_ReturnsTheFailuresOfItsStore(t *testing.T) {
	t.Run("reading the stale monitors stops the run", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.flaky.allErr = errBoom

		err := f.w.checkStaleMonitors(context.Background())

		assert.Equal(t, errBoom, err)
		assert.Empty(t, f.monitors.asked, "no monitor is read")
		assert.Empty(t, f.flaky.deletes)
		assert.False(t, f.flaky.ranAtSet)
	})

	t.Run("forgetting the monitors that are gone stops the run before it is recorded", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.seed(t, Entry{MonitorID: "gone", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.flaky.deleteErr["gone"] = errBoom

		err := f.w.checkStaleMonitors(context.Background())

		assert.Equal(t, errBoom, err)
		assert.Equal(t, [][]string{{"gone"}}, f.flaky.deletes)
		assert.False(t, f.flaky.ranAtSet)
	})

	t.Run("recording the run", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.flaky.setRanAtErr = errBoom

		assert.Equal(t, errBoom, f.w.checkStaleMonitors(context.Background()))
	})

	t.Run("a run with nothing to forget still asks the store to delete none", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.heartbeats.setLatest("a", failuresNow)

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, [][]string{nil}, f.flaky.deletes)
		assert.True(t, f.flaky.ranAtSet)
	})
}

func TestWatchdog_ReadsTheActiveMonitorsPageByPage(t *testing.T) {
	t.Run("up to the first empty page", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.monitors.pages = [][]*shared.Monitor{{staleMonitor("a")}, {staleMonitor("b")}}
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.heartbeats.setLatest("b", failuresNow.Add(-4*time.Minute))

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []int{0, 1, 2}, f.monitors.asked)
		assert.Equal(t, []string{"a", "b"}, f.rechecker.calls)
	})

	t.Run("a page that can't be read stops the run, and says which", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.monitors.pages = [][]*shared.Monitor{{staleMonitor("a")}, {staleMonitor("b")}}
		f.monitors.pageErr[1] = errBoom
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))

		err := f.w.checkStaleMonitors(context.Background())

		require.Error(t, err)
		assert.Equal(t, "failed to find active monitors (page 1): boom", err.Error())
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, []string{"a"}, f.rechecker.calls)
		assert.False(t, f.flaky.ranAtSet)
	})

	t.Run("a monitor that is paused or has no interval is not checked", func(t *testing.T) {
		paused := staleMonitor("paused")
		paused.Active = false
		unscheduled := staleMonitor("unscheduled")
		unscheduled.Interval = 0
		negative := staleMonitor("negative")
		negative.Interval = -1
		f := newFlakyFixture(t, paused, unscheduled, negative, staleMonitor("checked"))
		for _, id := range []string{"paused", "unscheduled", "negative", "checked"} {
			f.heartbeats.setLatest(id, failuresNow.Add(-4*time.Minute))
		}

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"checked"}, f.rechecker.calls)
	})
}

func TestWatchdog_GoesOnAfterAMonitorItCouldNotCheck(t *testing.T) {
	f := newFlakyFixture(t, staleMonitor("bad"), staleMonitor("good"))
	f.heartbeats.setLatest("good", failuresNow.Add(-4*time.Minute))
	f.w.heartbeats = &flakyHeartbeats{fakeHeartbeats: f.heartbeats, errFor: map[string]error{"bad": errBoom}}

	require.NoError(t, f.w.checkStaleMonitors(context.Background()))

	assert.Equal(t, []string{"Failed to check whether monitor is stale: failed to find latest heartbeat: boom"}, f.errorsLogged())
	assert.Equal(t, "bad", f.logs.FilterLevelExact(zap.ErrorLevel).All()[0].ContextMap()["monitor_id"])
	assert.Equal(t, []string{"good"}, f.rechecker.calls)
	assert.True(t, f.flaky.ranAtSet)
}

func TestWatchdog_StopsWhenItsContextIsDoneDuringAMonitorCheck(t *testing.T) {
	f := newFlakyFixture(t, staleMonitor("bad"), staleMonitor("next"))
	f.heartbeats.setLatest("next", failuresNow.Add(-4*time.Minute))
	f.w.heartbeats = &flakyHeartbeats{fakeHeartbeats: f.heartbeats, errFor: map[string]error{"bad": errBoom}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.w.checkStaleMonitors(ctx)

	assert.ErrorIs(t, err, errBoom)
	assert.Empty(t, f.errorsLogged())
	assert.Empty(t, f.rechecker.calls, "the rest would fail the same way")
	assert.False(t, f.flaky.ranAtSet)
}

func TestWatchdog_ForgetsEveryMonitorThatIsNoLongerChecked(t *testing.T) {
	for name, change := range map[string]func(*shared.Monitor){
		"paused":              func(m *shared.Monitor) { m.Active = false },
		"workspace suspended": func(m *shared.Monitor) { m.WorkspaceSuspended = true },
		"type disabled":       func(m *shared.Monitor) { m.TypeDisabled = true },
		"over the limit":      func(m *shared.Monitor) { m.OverLimit = true },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFlakyFixture(t)
			held := staleMonitor("held")
			change(held)
			f.monitors.byID["held"] = held
			f.seed(t, Entry{MonitorID: "held", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute), Notified: true})

			require.NoError(t, f.w.checkStaleMonitors(context.Background()))

			assert.Empty(t, f.entries(t))
			assert.Equal(t, [][]string{{"held"}}, f.flaky.deletes)
		})
	}

	t.Run("deleted", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.seed(t, Entry{MonitorID: "deleted", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Empty(t, f.entries(t))
	})

	t.Run("an active monitor the scan missed is kept", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.monitors.byID["missed"] = staleMonitor("missed")
		f.seed(t, Entry{MonitorID: "missed", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Len(t, f.entries(t), 1)
		assert.Equal(t, [][]string{nil}, f.flaky.deletes)
	})
}

func TestWatchdog_KeepsAStaleMonitorItCouldNotFind(t *testing.T) {
	t.Run("and goes on", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.seed(t, Entry{MonitorID: "unreadable", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.seed(t, Entry{MonitorID: "gone", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.monitors.idErr["unreadable"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to find stale monitor: boom"}, f.errorsLogged())
		assert.Equal(t, "unreadable", f.logs.FilterLevelExact(zap.ErrorLevel).All()[0].ContextMap()["monitor_id"])
		assert.Len(t, f.entries(t), 1)
		assert.Contains(t, f.entries(t), "unreadable")
		assert.True(t, f.flaky.ranAtSet)
	})

	t.Run("unless its context is done", func(t *testing.T) {
		f := newFlakyFixture(t)
		f.seed(t, Entry{MonitorID: "unreadable", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.monitors.idErr["unreadable"] = errBoom
		ctx, cancel := context.WithCancel(context.Background())
		f.monitors.onIDErr = cancel

		err := f.w.checkStaleMonitors(ctx)

		assert.Equal(t, errBoom, err)
		assert.Empty(t, f.errorsLogged())
		assert.Empty(t, f.flaky.deletes)
		assert.False(t, f.flaky.ranAtSet)
	})
}

// A write or a publish that fails while a monitor is checked is logged and leaves the run going.
func TestWatchdog_LogsTheFailuresOfAMonitorCheck(t *testing.T) {
	t.Run("a saved monitor whose record can't be forgotten", func(t *testing.T) {
		// Saved a minute ago and not checked since: the wait starts over, before a notification
		m := testMonitor("saved", 60, 10, failuresNow.Add(-time.Minute))
		f := newFlakyFixture(t, m)
		f.seed(t, Entry{MonitorID: "saved", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.flaky.deleteErr["saved"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Len(t, f.entries(t), 1)
	})

	t.Run("a saved monitor whose record is forgotten", func(t *testing.T) {
		m := testMonitor("saved", 60, 10, failuresNow.Add(-time.Minute))
		f := newFlakyFixture(t, m)
		f.seed(t, Entry{MonitorID: "saved", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Empty(t, f.entries(t))
		assert.Empty(t, f.published(MonitorStaleRecovered))
		assert.Empty(t, f.errorsLogged())
	})

	t.Run("a monitor checked again whose record can't be forgotten", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("back"))
		f.heartbeats.setLatest("back", failuresNow.Add(-time.Minute))
		f.seed(t, Entry{MonitorID: "back", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-10 * time.Minute), Notified: true})
		f.flaky.deleteErr["back"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Empty(t, f.published(MonitorStaleRecovered), "no recovery is reported while the record stays")
		assert.Len(t, f.entries(t), 1)
	})

	t.Run("a monitor on an offline probe whose record can't be forgotten", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("away"))
		f.heartbeats.setLatest("away", failuresNow.Add(-4*time.Minute))
		f.seed(t, Entry{MonitorID: "away", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute)})
		f.flaky.deleteErr["away"] = errBoom
		f.w.probeOffline = func(context.Context, *shared.Monitor) (bool, error) { return true, nil }

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Empty(t, f.rechecker.calls)
	})

	t.Run("a monitor whose probe can't be looked up", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.w.probeOffline = func(context.Context, *shared.Monitor) (bool, error) { return false, errBoom }

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: failed to find out whether the monitor's probe is offline: boom"}, f.errorsLogged())
		assert.Empty(t, f.rechecker.calls)
	})

	t.Run("a new stale monitor that is checked but not recorded", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.flaky.putErr["a"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"a"}, f.rechecker.calls)
		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Empty(t, f.entries(t))
	})

	t.Run("a new stale monitor with no recheck to make that is not recorded", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.w.rechecker = nil
		f.flaky.putErr["a"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Empty(t, f.entries(t))
	})

	t.Run("a stale monitor whose notification can't be recorded", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"))
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.seed(t, Entry{MonitorID: "a", Since: failuresNow.Add(-4 * time.Minute), DetectedAt: failuresNow.Add(-time.Minute)})
		f.flaky.putErr["a"] = errBoom

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		assert.Equal(t, []string{"Failed to check whether monitor is stale: boom"}, f.errorsLogged())
		assert.Empty(t, f.published(MonitorStale), "nothing is notified that isn't recorded")
		assert.False(t, f.entries(t)["a"].Notified)
	})

	t.Run("events that can't be published", func(t *testing.T) {
		f := newFlakyFixture(t, staleMonitor("a"), staleMonitor("b"))
		f.heartbeats.setLatest("a", failuresNow.Add(-4*time.Minute))
		f.heartbeats.setLatest("b", failuresNow.Add(-time.Minute))
		f.seed(t, Entry{MonitorID: "a", Since: failuresNow.Add(-4 * time.Minute), DetectedAt: failuresNow.Add(-time.Minute)})
		f.seed(t, Entry{MonitorID: "b", Since: failuresNow.Add(-time.Hour), DetectedAt: failuresNow.Add(-time.Minute), Notified: true})
		require.NoError(t, f.bus.Close(context.Background()))

		require.NoError(t, f.w.checkStaleMonitors(context.Background()))

		entries := f.entries(t)
		assert.True(t, entries["a"].Notified, "the notification stays recorded")
		assert.NotContains(t, entries, "b")
		var errs []string
		for _, entry := range f.logs.FilterLevelExact(zap.ErrorLevel).All() {
			context := entry.ContextMap()
			errs = append(errs, entry.Message+" "+context["topic"].(string)+" "+context["monitor_id"].(string))
		}
		assert.Equal(t, []string{
			"Failed to publish stale monitor event monitor.stale a",
			"Failed to publish stale monitor event monitor.stale.recovered b",
		}, errs)
		assert.True(t, f.flaky.ranAtSet)
	})
}
