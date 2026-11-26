package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// observed gives the scheduler a logger whose entries the test can read
func observed() (func(*Config), *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return func(cfg *Config) { cfg.Logger = zap.New(core).Sugar() }, logs
}

// faultySource is a source whose reads fail when the test says so
type faultySource struct {
	Source
	mu      sync.Mutex
	listErr func(page int) error
	findErr map[string]error
	finds   int
}

func (s *faultySource) ListActive(ctx context.Context, page, limit int) ([]*shared.Monitor, error) {
	s.mu.Lock()
	fail := s.listErr
	s.mu.Unlock()
	if fail != nil {
		if err := fail(page); err != nil {
			return nil, err
		}
	}
	return s.Source.ListActive(ctx, page, limit)
}

func (s *faultySource) Find(ctx context.Context, id string) (*shared.Monitor, error) {
	s.mu.Lock()
	s.finds++
	err := s.findErr[id]
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.Source.Find(ctx, id)
}

func (s *faultySource) setListErr(fail func(page int) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listErr = fail
}

func (s *faultySource) setFindErr(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findErr == nil {
		s.findErr = map[string]error{}
	}
	s.findErr[id] = err
}

func (s *faultySource) findCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finds
}

// withFaultySource makes the scheduler read its monitors through the source that fails on demand
func withFaultySource() (func(*Config), *faultySource) {
	faulty := &faultySource{}
	return func(cfg *Config) {
		faulty.Source = cfg.Source
		cfg.Source = faulty
	}, faulty
}

// scheduled is the IDs of the monitors that have a timer
func scheduled(h *harness) map[string]bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	out := map[string]bool{}
	for id := range h.s.schedules {
		out[id] = true
	}
	return out
}

func TestScheduler_StartFailsWhenTheSourceCannotListItsMonitors(t *testing.T) {
	t.Parallel()
	faulty, source := withFaultySource()
	h := newHarness(t, faulty)
	h.db.put(monitorWith("mon", withInterval(day)))
	source.setListErr(func(page int) error { return errors.New("source down") })

	err := h.s.Start(context.Background())

	require.Error(t, err)
	assert.EqualError(t, err, "failed to find active monitors (page 0): source down")
	assert.Empty(t, scheduled(h))
	assert.Zero(t, h.exec.count())
}

func TestScheduler_ReconcileFailsOnTheLaterPageThatCannotBeRead(t *testing.T) {
	t.Parallel()
	faulty, source := withFaultySource()
	h := newHarness(t, faulty)
	for i := 0; i < pageSize; i++ {
		h.db.put(monitorWith(fmt.Sprintf("mon-%03d", i), withInterval(day)))
	}
	h.start()
	require.Len(t, scheduled(h), pageSize)
	h.db.put(monitorWith("mon-new", withInterval(day))) // the 101st, on page 1
	source.setListErr(func(page int) error {
		if page == 1 {
			return errors.New("source down")
		}
		return nil
	})

	err := h.s.reconcile(context.Background())

	assert.EqualError(t, err, "failed to find active monitors (page 1): source down")
	assert.Len(t, scheduled(h), pageSize, "the monitors read stay as they were, and the one not read is not added")

	source.setListErr(nil)
	require.NoError(t, h.s.reconcile(context.Background()), "the lock is released")
	assert.Len(t, scheduled(h), pageSize+1)
}

func TestScheduler_ReconcileSkipsAnActiveMonitorWithoutACheckInterval(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("no-interval", withInterval(0)))
	h.db.put(monitorWith("fine", withInterval(day)))
	h.start()

	require.NoError(t, h.s.reconcile(context.Background()))

	assert.Equal(t, map[string]bool{"fine": true}, scheduled(h), "an active monitor with no check interval gets no timer")
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("fine") == 1 }, "the other is checked")
	assert.Zero(t, h.exec.countFor("no-interval"))
}

func TestScheduler_ReconcileLeavesAMonitorItCannotReadWhereItIs(t *testing.T) {
	t.Parallel()
	logger, logs := observed()
	faulty, source := withFaultySource()
	h := newHarness(t, logger, faulty)
	h.db.put(monitorWith("mon", withInterval(day)))
	h.start()
	logs.TakeAll() // what the start said
	h.db.unlist("mon")
	source.setFindErr("mon", errors.New("source down"))

	require.NoError(t, h.s.reconcile(context.Background()))

	assert.Equal(t, map[string]bool{"mon": true}, scheduled(h), "it keeps its timer")
	entries := logs.FilterMessage("Failed to find monitor").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Equal(t, "mon", entries[0].ContextMap()["monitor_id"])
	assert.Equal(t, "source down", entries[0].ContextMap()["error"])
	assert.Zero(t, logs.FilterMessage("Reconciled the schedule with the source").Len(), "nothing changed")
}

func TestScheduler_ReconcileStopsAtOnceWhenItsContextEndsBeforeItAsksAboutAMissedMonitor(t *testing.T) {
	t.Parallel()
	faulty, source := withFaultySource()
	h := newHarness(t, faulty)
	h.db.put(monitorWith("mon", withInterval(day)))
	h.start()
	h.db.unlist("mon")
	asked := source.findCalls()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := h.s.reconcile(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, asked, source.findCalls(), "the monitor is not asked about")
	assert.Equal(t, map[string]bool{"mon": true}, scheduled(h))
}

func TestScheduler_ReconcileCountsWhatItAddedRescheduledAndRemoved(t *testing.T) {
	t.Parallel()
	logger, logs := observed()
	h := newHarness(t, logger)
	h.db.put(monitorWith("kept", withInterval(day)))
	h.db.put(monitorWith("slowed", withInterval(day)))
	h.db.put(monitorWith("gone", withInterval(day)))
	h.start()
	logs.TakeAll() // what the start said

	h.db.put(monitorWith("new", withInterval(day)))
	h.db.put(monitorWith("slowed", withInterval(2*day)))
	h.db.remove("gone")
	require.NoError(t, h.s.reconcile(context.Background()))

	entries := logs.FilterMessage("Reconciled the schedule with the source").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
	assert.EqualValues(t, 1, entries[0].ContextMap()["added"])
	assert.EqualValues(t, 1, entries[0].ContextMap()["rescheduled"])
	assert.EqualValues(t, 1, entries[0].ContextMap()["removed"])
	assert.Equal(t, map[string]bool{"kept": true, "slowed": true, "new": true}, scheduled(h))

	require.NoError(t, h.s.reconcile(context.Background()))
	assert.Equal(t, 1, logs.FilterMessage("Reconciled the schedule with the source").Len(), "a reconcile that changes nothing says nothing")
}

func TestScheduler_ReconcileReschedulesAMissedMonitorWhoseIntervalChangedAndRemovesOneThatIsNoLongerSchedulable(t *testing.T) {
	t.Parallel()
	logger, logs := observed()
	h := newHarness(t, logger)
	h.db.put(monitorWith("slowed", withInterval(day)))
	h.db.put(monitorWith("stopped", withInterval(day)))
	h.db.put(monitorWith("untimed", withInterval(day)))
	h.start()
	logs.TakeAll() // what the start said

	// None is listed. The first is still active with a new interval, the second is paused, and the
	// third is active with no interval
	h.db.put(monitorWith("slowed", withInterval(2*day)))
	h.db.put(monitorWith("stopped", withInterval(day), paused))
	h.db.put(monitorWith("untimed", withInterval(0)))
	h.db.unlist("slowed")
	h.db.unlist("stopped")
	h.db.unlist("untimed")
	require.NoError(t, h.s.reconcile(context.Background()))

	assert.Equal(t, map[string]bool{"slowed": true}, scheduled(h))
	entries := logs.FilterMessage("Reconciled the schedule with the source").All()
	require.Len(t, entries, 1)
	assert.EqualValues(t, 0, entries[0].ContextMap()["added"])
	assert.EqualValues(t, 1, entries[0].ContextMap()["rescheduled"])
	assert.EqualValues(t, 2, entries[0].ContextMap()["removed"])
}

// The refusals of a check's load are logged each as what it is, at the level it deserves, and the
// check goes no further
func TestScheduler_RunCheckSaysWhyALoadRefusedTheCheck(t *testing.T) {
	t.Parallel()
	generic := errors.New("database is gone")
	tests := []struct {
		name    string
		err     error
		level   zapcore.Level
		message string
		fields  map[string]any
	}{
		{"a monitor that is gone", check.ErrMonitorNotFound, zapcore.WarnLevel, "Monitor not found, skipping its check", nil},
		{"a paused monitor", check.ErrMonitorPaused, zapcore.InfoLevel, "Monitor is paused, skipping its check", nil},
		{"a suspended workspace", check.ErrWorkspaceSuspended, zapcore.InfoLevel, "Monitor's workspace is suspended, skipping its check", nil},
		{"a type turned off", check.ErrMonitorTypeDisabled, zapcore.InfoLevel, "Monitor's type is turned off on this install, skipping its check", nil},
		{"a monitor over its limit", check.ErrMonitorOverLimit, zapcore.InfoLevel, "Monitor is past its workspace's limit of active monitors, skipping its check", nil},
		{"a wrapped refusal", fmt.Errorf("loading: %w", check.ErrMonitorPaused), zapcore.InfoLevel, "Monitor is paused, skipping its check", nil},
		{"any other error", generic, zapcore.ErrorLevel, "Failed to load the monitor's check", map[string]any{"error": "database is gone"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logger, logs := observed()
			h := newHarness(t, logger, withLoader(loaderFunc(func(context.Context, string) (*check.Job, error) { return nil, tc.err })))

			h.s.runCheck(context.Background(), "mon")

			entries := logs.FilterMessage(tc.message).All()
			require.Len(t, entries, 1)
			assert.Equal(t, tc.level, entries[0].Level)
			assert.Equal(t, "mon", entries[0].ContextMap()["monitor_id"])
			for key, value := range tc.fields {
				assert.Equal(t, value, entries[0].ContextMap()[key])
			}
			assert.Zero(t, h.exec.count())
			assert.Empty(t, h.ingest.ingested())
		})
	}
}

// A refusal is logged as one even when the check was cancelled while it loaded; only an error that
// is no refusal is taken for the cancel
func TestScheduler_RunCheckReadsTheRefusalBeforeTheCancel(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("a refusal", func(t *testing.T) {
		t.Parallel()
		logger, logs := observed()
		h := newHarness(t, logger, withLoader(loaderFunc(func(context.Context, string) (*check.Job, error) { return nil, check.ErrMonitorPaused })))

		h.s.runCheck(cancelled, "mon")

		assert.Equal(t, 1, logs.FilterMessage("Monitor is paused, skipping its check").Len())
		assert.Zero(t, logs.FilterMessage("Check was cancelled while it loaded").Len())
	})
	t.Run("any other error", func(t *testing.T) {
		t.Parallel()
		logger, logs := observed()
		h := newHarness(t, logger, withLoader(loaderFunc(func(context.Context, string) (*check.Job, error) { return nil, errors.New("interrupted") })))

		h.s.runCheck(cancelled, "mon")

		entries := logs.FilterMessage("Check was cancelled while it loaded").All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
		assert.Zero(t, logs.FilterMessage("Failed to load the monitor's check").Len())
	})
}

func TestScheduler_RunCheckSkipsAMonitorWhoseTypeHasNoExecutor(t *testing.T) {
	t.Parallel()
	logger, logs := observed()
	h := newHarness(t, logger, func(cfg *Config) { cfg.Executors = executorsByType{} })
	h.db.put(monitorWith("mon", withInterval(day), ofType("carrier-pigeon")))

	h.s.runCheck(context.Background(), "mon")

	entries := logs.FilterMessage("Executor not found for monitor type").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Equal(t, "mon", entries[0].ContextMap()["monitor_id"])
	assert.Equal(t, "carrier-pigeon", entries[0].ContextMap()["monitor_type"])
	assert.Empty(t, h.ingest.ingested())
}
