package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeBus delivers events to its handlers in the publishing goroutine, as JSON
// like the Redis event bus does.
type fakeBus struct {
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
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		panic(err)
	}
	b.mu.Lock()
	handlers := slices.Clone(b.handlers[event.Type])
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(events.Event{Type: event.Type, Payload: json.RawMessage(payload)})
	}
}

func (b *fakeBus) Close() error { return nil }

// fakeMonitors is the monitor service over a map of Monitors. Any call but
// FindByID and FindActive panics on the nil embedded interface.
type fakeMonitors struct {
	monitor.Service
	mu       sync.Mutex
	monitors map[string]monitor.Model
	findErr  error
	calls    atomic.Int32
	// activeGate, when set before Start, holds FindActive until it's closed
	activeGate chan struct{}
}

func (f *fakeMonitors) set(m monitor.Model) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.monitors == nil {
		f.monitors = map[string]monitor.Model{}
	}
	f.monitors[m.ID] = m
}

func (f *fakeMonitors) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.monitors, id)
}

func (f *fakeMonitors) FindByID(_ context.Context, id string) (*monitor.Model, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.findErr != nil {
		return nil, f.findErr
	}
	m, ok := f.monitors[id]
	if !ok {
		return nil, nil
	}
	return &m, nil
}

func (f *fakeMonitors) FindActive(ctx context.Context) ([]*monitor.Model, error) {
	f.calls.Add(1)
	if f.activeGate != nil {
		select {
		case <-f.activeGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var active []*monitor.Model
	for _, m := range f.monitors {
		if m.Active {
			active = append(active, &m)
		}
	}
	return active, nil
}

// fakeHeartbeats is the heartbeat service over each Monitor's latest Heartbeat.
type fakeHeartbeats struct {
	heartbeat.Service
	mu     sync.Mutex
	latest map[string]heartbeat.Model
	calls  atomic.Int32
	// gate, when set before Start, holds FindByMonitorIDPaginated until it's closed
	gate chan struct{}
}

func (f *fakeHeartbeats) setLatest(hb heartbeat.Model) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.latest == nil {
		f.latest = map[string]heartbeat.Model{}
	}
	f.latest[hb.MonitorID] = hb
}

func (f *fakeHeartbeats) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	f.calls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	hb, ok := f.latest[monitorID]
	if !ok {
		return nil, nil
	}
	return []*heartbeat.Model{&hb}, nil
}

// rig is a Collector over fake services and a fake bus, scraped through its handler.
type rig struct {
	t          *testing.T
	monitors   *fakeMonitors
	heartbeats *fakeHeartbeats
	bus        *fakeBus
	collector  *Collector
	handler    http.HandlerFunc
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, monitors: &fakeMonitors{}, heartbeats: &fakeHeartbeats{}, bus: &fakeBus{}}
	r.collector = NewCollector(r.monitors, r.heartbeats, zap.NewNop().Sugar())
	r.handler = r.collector.Handler()
	return r
}

// start starts the collector on the rig's bus until the test ends.
func (r *rig) start() {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	r.collector.Start(ctx, r.bus)
}

// activeMonitor adds an active Monitor the services know.
func (r *rig) activeMonitor(id, name, monitorType string) monitor.Model {
	m := monitor.Model{ID: id, Name: name, Type: monitorType, Active: true}
	r.monitors.set(m)
	return m
}

func (r *rig) publishHeartbeat(monitorID string, status shared.MonitorStatus, pingMs int, at int64) {
	r.bus.Publish(events.Event{Type: events.HeartbeatEvent, Payload: &heartbeat.Model{
		MonitorID: monitorID, Status: status, Ping: pingMs, Time: time.Unix(at, 0),
	}})
}

func (r *rig) publishMonitorUpdated(m monitor.Model) {
	r.bus.Publish(events.Event{Type: events.MonitorUpdated, Payload: &m})
}

func (r *rig) publishMonitorDeleted(id string) {
	r.bus.Publish(events.Event{Type: events.MonitorDeleted, Payload: id})
}

// body scrapes the handler.
func (r *rig) body() string {
	r.t.Helper()
	w := httptest.NewRecorder()
	r.handler(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(r.t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.String()
}

// series parses the Monitor series out of an exposition, keyed by name and labels.
func series(t *testing.T, exposition string) map[string]float64 {
	t.Helper()
	parsed := map[string]float64{}
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, "peekaping_monitor_") {
			continue
		}
		cut := strings.LastIndex(line, " ")
		require.Positive(t, cut, line)
		value, err := strconv.ParseFloat(line[cut+1:], 64)
		require.NoError(t, err, line)
		parsed[line[:cut]] = value
	}
	return parsed
}

// requireSeries checks the scrape carries exactly the Monitor series in want,
// written as exposition lines.
func (r *rig) requireSeries(want string) {
	r.t.Helper()
	require.Equal(r.t, series(r.t, want), series(r.t, r.body()))
}

// eventuallySeries is requireSeries for series that appear on a background goroutine.
func (r *rig) eventuallySeries(want string) {
	r.t.Helper()
	wanted := series(r.t, want)
	if !assert.Eventually(r.t, func() bool {
		return assert.ObjectsAreEqual(wanted, series(r.t, r.body()))
	}, 2*time.Second, 5*time.Millisecond) {
		r.requireSeries(want)
	}
}

const (
	api   = `monitor_id="m1",monitor_name="API",monitor_type="http"`
	at100 = 1700000100
	at200 = 1700000200
)

func TestHeartbeatExportsStatusTimestampAndResponseSeconds(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.start()

	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)

	r.requireSeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
peekaping_monitor_response_seconds{` + api + `} 0.25
`)
}

func TestStatusIsExportedAsItsCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status shared.MonitorStatus
		code   string
	}{
		{"Down", shared.MonitorStatusDown, "0"},
		{"Pending", shared.MonitorStatusPending, "2"},
		{"Maintenance", shared.MonitorStatusMaintenance, "3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.activeMonitor("m1", "API", "http")
			r.start()

			r.publishHeartbeat("m1", tc.status, 250, at100)

			// A Heartbeat that isn't Up has no Response time to report
			r.requireSeries(`
peekaping_monitor_status{` + api + `} ` + tc.code + `
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
`)
		})
	}
}

func TestResponseSecondsLeaveWhenTheMonitorStopsBeingUp(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.start()
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)

	r.publishHeartbeat("m1", shared.MonitorStatusDown, 0, at200)
	r.requireSeries(`
peekaping_monitor_status{` + api + `} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000200
`)

	r.publishHeartbeat("m1", shared.MonitorStatusUp, 80, at200+60)
	r.requireSeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000260
peekaping_monitor_response_seconds{` + api + `} 0.08
`)
}

func TestHeartbeatOfAMonitorThatIsMissingOrPausedCreatesNoSeries(t *testing.T) {
	r := newRig(t)
	r.monitors.set(monitor.Model{ID: "paused", Name: "Paused", Type: "http", Active: false})
	r.start()

	r.publishHeartbeat("deleted", shared.MonitorStatusUp, 100, at100)
	r.publishHeartbeat("paused", shared.MonitorStatusUp, 100, at100)

	r.requireSeries("")
}

func TestHeartbeatOfAMonitorThatCannotBeReadCreatesNoSeries(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.monitors.findErr = errors.New("database is down")
	r.start()

	r.publishHeartbeat("m1", shared.MonitorStatusUp, 100, at100)

	r.requireSeries("")
}

func TestScrapeSurvivesAMonitorNameThatIsNotUTF8(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("bad", "Billing\xff", "http")
	r.activeMonitor("m1", "API", "http")
	r.start()

	r.publishHeartbeat("bad", shared.MonitorStatusUp, 100, at100)
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)

	r.requireSeries(`
peekaping_monitor_status{monitor_id="bad",monitor_name="Billing` + "�" + `",monitor_type="http"} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="bad",monitor_name="Billing` + "�" + `",monitor_type="http"} 1700000100
peekaping_monitor_response_seconds{monitor_id="bad",monitor_name="Billing` + "�" + `",monitor_type="http"} 0.1
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
peekaping_monitor_response_seconds{` + api + `} 0.25
`)
}

func TestHandlerServesBuildInfoAndRuntimeMetrics(t *testing.T) {
	r := newRig(t)

	body := r.body()

	assert.Contains(t, body, `peekaping_build_info{version="`+version.Version+`"} 1`)
	assert.Contains(t, body, "go_goroutines ")
	assert.Contains(t, body, "process_start_time_seconds ")
	assert.NotContains(t, body, "peekaping_monitor_up")
}
