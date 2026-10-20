package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
}

func newFakeDB() *fakeDB {
	return &fakeDB{byID: map[string]*shared.Monitor{}, lastHeartbeats: map[string]*shared.HeartBeatModel{}}
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
	defer db.mu.Unlock()
	m, ok := db.byID[id]
	if !ok {
		return nil, nil
	}
	copied := *m
	return &copied, nil
}

func (db *fakeDB) FindActivePaginated(_ context.Context, page, limit int) ([]*shared.Monitor, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var active []*shared.Monitor
	for _, m := range db.byID {
		if m.Active {
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
		return nil, check.ErrMonitorInactive
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

// fakeBus delivers published events to the subscribed handlers, in the publisher's goroutine
type fakeBus struct {
	events.EventBus
	mu       sync.Mutex
	handlers map[events.EventType][]events.EventHandler
}

func (b *fakeBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handlers == nil {
		b.handlers = map[events.EventType][]events.EventHandler{}
	}
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *fakeBus) Publish(event events.Event) {
	b.mu.Lock()
	handlers := slices.Clone(b.handlers[event.Type])
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(event)
	}
}

// harness is a scheduler over fakes
type harness struct {
	t      *testing.T
	db     *fakeDB
	exec   *fakeExecutor
	ingest *recordingIngester
	bus    *fakeBus
	s      *Scheduler
}

// newHarness builds a scheduler that is subscribed to a fake bus and not started. Its
// reconcile is off unless a test sets an interval.
func newHarness(t *testing.T, options ...func(*Config)) *harness {
	t.Helper()
	logger := zap.NewNop().Sugar()
	h := &harness{
		t:      t,
		db:     newFakeDB(),
		exec:   &fakeExecutor{},
		ingest: &recordingIngester{},
		bus:    &fakeBus{},
	}
	cfg := Config{
		Loader:            h.db,
		Monitors:          h.db,
		Executors:         h.exec,
		Supervisor:        healthcheck.NewHealthCheck(nil, nil, logger),
		Ingester:          h.ingest,
		Concurrency:       128,
		ReconcileInterval: time.Hour,
		Logger:            logger,
	}
	for _, option := range options {
		option(&cfg)
	}
	h.s = New(cfg)
	h.s.Subscribe(h.bus)
	return h
}

// start starts the scheduler and stops it when the test ends
func (h *harness) start() {
	h.t.Helper()
	require.NoError(h.t, h.s.Start(context.Background()))
	h.t.Cleanup(h.s.Stop)
}

// publishMonitor publishes the monitor's event with the monitor as it is in the database,
// as the monitor service does
func (h *harness) publishMonitor(eventType events.EventType, id string) {
	m := h.db.get(id)
	h.bus.Publish(events.Event{Type: eventType, Payload: &m})
}

func (h *harness) publishDeleted(id string) {
	h.bus.Publish(events.Event{Type: events.MonitorDeleted, Payload: id})
}

// monitorOption changes a test monitor
type monitorOption func(*shared.Monitor)

func withInterval(seconds int) monitorOption { return func(m *shared.Monitor) { m.Interval = seconds } }
func ofType(monitorType string) monitorOption {
	return func(m *shared.Monitor) { m.Type = monitorType }
}
func paused(m *shared.Monitor) { m.Active = false }

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

// rawJSON is a payload as the Redis event bus delivers it
func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return encoded
}
