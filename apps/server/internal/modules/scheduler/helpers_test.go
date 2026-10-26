package scheduler

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// A day is a check interval that leaves the timer out of a test: the next check is far away
const day = 24 * 60 * 60

// fakeDB is the monitor database. It pages the active monitors, finds any by ID, and loads
// a monitor's check as check.Loader does.
type fakeDB struct {
	mu         sync.Mutex
	byID       map[string]*shared.Monitor
	maintained bool
	certExpiry bool
	// lastHeartbeats are the push monitors' latest heartbeats
	lastHeartbeats map[string]*shared.HeartBeatModel
	// unlisted monitors are left out of the pages of active monitors, as a monitor paused
	// while the pages are read shifts the next one out of them
	unlisted map[string]bool
	// held monitors' next FindByID waits to be released
	held map[string]*findHold
}

// findHold is a FindByID made to wait: started is closed when it begins, and it returns once
// released is closed
type findHold struct {
	started, released chan struct{}
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		byID:           map[string]*shared.Monitor{},
		lastHeartbeats: map[string]*shared.HeartBeatModel{},
		unlisted:       map[string]bool{},
		held:           map[string]*findHold{},
	}
}

// holdFind makes the monitor's next FindByID wait until release is called. reading is closed
// once that FindByID has begun.
func (db *fakeDB) holdFind(id string) (reading <-chan struct{}, release func()) {
	hold := &findHold{started: make(chan struct{}), released: make(chan struct{})}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.held[id] = hold
	return hold.started, sync.OnceFunc(func() { close(hold.released) })
}

func (db *fakeDB) unlist(id string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.unlisted[id] = true
}

func (db *fakeDB) setLastHeartbeat(hb shared.HeartBeatModel) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.lastHeartbeats[hb.MonitorID] = &hb
}

func (db *fakeDB) put(m shared.Monitor) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.byID[m.ID] = &m
}

func (db *fakeDB) remove(id string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.byID, id)
}

func (db *fakeDB) get(id string) shared.Monitor {
	db.mu.Lock()
	defer db.mu.Unlock()
	return *db.byID[id]
}

func (db *fakeDB) FindByID(_ context.Context, id string) (*shared.Monitor, error) {
	db.mu.Lock()
	hold := db.held[id]
	delete(db.held, id)
	db.mu.Unlock()
	if hold != nil {
		close(hold.started)
		<-hold.released
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	m, ok := db.byID[id]
	if !ok {
		return nil, nil
	}
	copied := *m
	return &copied, nil
}

// FindActivePaginated pages every active monitor, whichever probe runs it, as the stale-monitor
// watchdog reads them
func (db *fakeDB) FindActivePaginated(ctx context.Context, page, limit int) ([]*shared.Monitor, error) {
	return db.pageActive(page, limit, func(*shared.Monitor) bool { return true })
}

// FindActiveByProbe pages the active monitors the probe runs
func (db *fakeDB) FindActiveByProbe(_ context.Context, probeID string, page, limit int) ([]*shared.Monitor, error) {
	return db.pageActive(page, limit, func(m *shared.Monitor) bool { return m.RunsOn(probeID) })
}

func (db *fakeDB) pageActive(page, limit int, keep func(*shared.Monitor) bool) ([]*shared.Monitor, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var active []*shared.Monitor
	for _, m := range db.byID {
		if m.Active && keep(m) && !db.unlisted[m.ID] {
			copied := *m
			active = append(active, &copied)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	from := min(page*limit, len(active))
	to := min(from+limit, len(active))
	return active[from:to], nil
}

func (db *fakeDB) Load(ctx context.Context, id string) (*check.Job, error) {
	m, _ := db.FindByID(ctx, id)
	if m == nil {
		return nil, check.ErrMonitorNotFound
	}
	if !m.Active {
		return nil, check.ErrMonitorPaused
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	job := &check.Job{Monitor: m, IsUnderMaintenance: db.maintained, CheckCertExpiry: db.certExpiry}
	if m.Type == "push" {
		job.LastHeartbeat = db.lastHeartbeats[id]
	}
	return job, nil
}

// checkCall is one run of the executor
type checkCall struct {
	monitorID string
	// n is the call's number among the monitor's calls, starting at 1
	n  int
	at time.Time
}

// fakeExecutor stands in for every monitor type's executor. It records its calls and
// decides each result with run, which may block.
type fakeExecutor struct {
	mu      sync.Mutex
	calls   []checkCall
	running int
	peak    int
	// run returns a check's result. It is Up when it is nil.
	run func(ctx context.Context, call checkCall) *executor.Result
}

func (e *fakeExecutor) Execute(ctx context.Context, m *executor.Monitor, _ *executor.Proxy) *executor.Result {
	e.mu.Lock()
	call := checkCall{monitorID: m.ID, n: 1, at: time.Now()}
	for _, c := range e.calls {
		if c.monitorID == m.ID {
			call.n++
		}
	}
	e.calls = append(e.calls, call)
	e.running++
	e.peak = max(e.peak, e.running)
	run := e.run
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.running--
		e.mu.Unlock()
	}()

	if run != nil {
		return run(ctx, call)
	}
	return up(fmt.Sprintf("check %d", call.n))
}

func (e *fakeExecutor) Validate(string) error                        { return nil }
func (e *fakeExecutor) Unmarshal(string) (any, error)                { return nil, nil }
func (e *fakeExecutor) GetExecutor(string) (executor.Executor, bool) { return e, true }

// executorsByType finds the executor for a monitor type in a map, as executor.ExecutorRegistry does
type executorsByType map[string]executor.Executor

func (e executorsByType) GetExecutor(monitorType string) (executor.Executor, bool) {
	exec, ok := e[monitorType]
	return exec, ok
}

func up(message string) *executor.Result {
	now := time.Now()
	return &executor.Result{Status: shared.MonitorStatusUp, Message: message, StartTime: now, EndTime: now}
}

func down(message string) *executor.Result {
	now := time.Now()
	return &executor.Result{Status: shared.MonitorStatusDown, Message: message, StartTime: now, EndTime: now}
}

func (e *fakeExecutor) callsFor(monitorID string) []checkCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	var calls []checkCall
	for _, c := range e.calls {
		if c.monitorID == monitorID {
			calls = append(calls, c)
		}
	}
	return calls
}

func (e *fakeExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func (e *fakeExecutor) countFor(monitorID string) int {
	return len(e.callsFor(monitorID))
}

func (e *fakeExecutor) monitorIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids := make([]string, 0, len(e.calls))
	for _, c := range e.calls {
		ids = append(ids, c.monitorID)
	}
	slices.Sort(ids)
	return ids
}

func (e *fakeExecutor) runningNow() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *fakeExecutor) mostRunning() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak
}

// recordingIngester records the results it is given
type recordingIngester struct {
	mu      sync.Mutex
	results []*ingester.Result
}

func (r *recordingIngester) Ingest(_ context.Context, result *ingester.Result) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
	return nil
}

func (r *recordingIngester) ingested() []*ingester.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.results)
}

func (r *recordingIngester) messages() []string {
	var messages []string
	for _, result := range r.ingested() {
		messages = append(messages, result.Message)
	}
	return messages
}

// ingestedFor counts the monitor's ingested results
func (h *harness) ingestedFor(monitorID string) int {
	return len(h.ingestedMessagesFor(monitorID))
}

// ingestedMessagesFor is what the monitor's ingested results say, oldest first
func (h *harness) ingestedMessagesFor(monitorID string) []string {
	var messages []string
	for _, r := range h.ingest.ingested() {
		if r.MonitorID == monitorID {
			messages = append(messages, r.Message)
		}
	}
	return messages
}

// harness is a scheduler over fakes
type harness struct {
	t      *testing.T
	db     *fakeDB
	exec   *fakeExecutor
	ingest *recordingIngester
	bus    *events.Bus
	s      *Scheduler
}

// newHarness builds a scheduler on the database source, over the fake database and an in-memory
// event bus, and does not start it. Its reconcile is off unless a test sets an interval.
func newHarness(t *testing.T, options ...func(*Config)) *harness {
	t.Helper()
	logger := zap.NewNop().Sugar()
	h := &harness{
		t:      t,
		db:     newFakeDB(),
		exec:   &fakeExecutor{},
		ingest: &recordingIngester{},
		bus:    newBus(t),
	}
	cfg := Config{
		Source:            NewDatabaseSource(h.db, h.db, h.bus),
		Executors:         h.exec,
		Supervisor:        supervisor.NewHealthCheck(nil, nil, logger),
		Ingester:          h.ingest,
		Concurrency:       128,
		ReconcileInterval: time.Hour,
		Logger:            logger,
	}
	for _, option := range options {
		option(&cfg)
	}
	h.s = New(cfg)
	return h
}

// loadingWith is a Source whose checks are loaded by the loader, and which is the source
// it wraps in all else
type loadingWith struct {
	Source
	loader Loader
}

func (s loadingWith) Load(ctx context.Context, monitorID string) (*check.Job, error) {
	return s.loader.Load(ctx, monitorID)
}

// withLoader makes the scheduler's source load its checks with the loader
func withLoader(loader Loader) func(*Config) {
	return func(cfg *Config) { cfg.Source = loadingWith{Source: cfg.Source, loader: loader} }
}

// start starts the scheduler and stops it when the test ends
func (h *harness) start() {
	h.t.Helper()
	require.NoError(h.t, h.s.Start(context.Background()))
	h.t.Cleanup(h.s.Stop)
}

// newBus is single mode's in-memory event bus, closed when the test ends: after the
// scheduler stops, since it was made first. The test fails if an event handler panicked, got a
// payload it couldn't read, or was still running 5 seconds into the close.
func newBus(t testing.TB) *events.Bus {
	t.Helper()
	core, logs := observer.New(zap.ErrorLevel)
	bus := events.NewMemoryBus(events.WithLogger(zap.New(core).Sugar()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, bus.Close(ctx), "the event handlers return")
		for _, entry := range logs.All() {
			t.Errorf("event bus: %s %v", entry.Message, entry.ContextMap())
		}
	})
	return bus
}

// publishMonitor publishes the monitor's event with the monitor as it is in the database,
// as the monitor service does. The bus hands it to the scheduler on a goroutine of its own.
func (h *harness) publishMonitor(topic events.Topic[shared.Monitor], id string) {
	h.t.Helper()
	require.NoError(h.t, events.Publish(context.Background(), h.bus, topic, h.db.get(id)))
}

func (h *harness) publishDeleted(id string) {
	h.t.Helper()
	require.NoError(h.t, events.Publish(context.Background(), h.bus, events.MonitorDeleted, id))
}

// monitorOption changes a test monitor
type monitorOption func(*shared.Monitor)

func withInterval(seconds int) monitorOption { return func(m *shared.Monitor) { m.Interval = seconds } }
func ofType(monitorType string) monitorOption {
	return func(m *shared.Monitor) { m.Type = monitorType }
}
func paused(m *shared.Monitor) { m.Active = false }

// onProbes sets the probes that run the monitor. A monitor without any runs on the local probe.
func onProbes(ids ...string) monitorOption { return func(m *shared.Monitor) { m.ProbeIDs = ids } }

// monitorWith is an active HTTP monitor that is checked every second
func monitorWith(id string, options ...monitorOption) shared.Monitor {
	m := shared.Monitor{ID: id, Name: "Monitor " + id, Type: "http", Interval: 1, Timeout: 5, Active: true}
	for _, option := range options {
		option(&m)
	}
	return m
}

// sleepUntilSecondFraction waits for the wall clock to be the given time into a second
func sleepUntilSecondFraction(fraction time.Duration) {
	now := time.Now()
	target := now.Truncate(time.Second).Add(fraction)
	if !target.After(now) {
		target = target.Add(time.Second)
	}
	time.Sleep(time.Until(target))
}

// intoSecond is how far into its second the time is
func intoSecond(t time.Time) time.Duration {
	return time.Duration(t.Nanosecond())
}

// A timer fires a little after its time. This is how late the tests let a check be.
const lateness = 300 * time.Millisecond

func eventually(t *testing.T, timeout time.Duration, condition func() bool, message string, args ...any) {
	t.Helper()
	require.Eventually(t, condition, timeout, 5*time.Millisecond, append([]any{message}, args...)...)
}
