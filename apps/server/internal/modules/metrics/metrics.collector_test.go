package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeMonitors is the monitor service over a map of Monitors. Any call but
// FindByID and FindActive panics on the nil embedded interface.
type fakeMonitors struct {
	monitor.Service
	mu       sync.Mutex
	monitors map[string]monitor.Model
	findErr  error
	// findGate, when set before Start, holds FindByID until the context is done
	findGate chan struct{}
	calls    atomic.Int32
	// activeCalls counts the FindActive calls that returned
	activeCalls atomic.Int32
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

func (f *fakeMonitors) FindByIDAnyWorkspace(ctx context.Context, id string) (*monitor.Model, error) {
	f.calls.Add(1)
	if f.findGate != nil {
		select {
		case <-f.findGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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

func (f *fakeMonitors) FindActiveAnyWorkspace(ctx context.Context) ([]*monitor.Model, error) {
	defer f.activeCalls.Add(1)
	// The result is as of the call, however long the gate holds it
	f.mu.Lock()
	var active []*monitor.Model
	for _, m := range f.monitors {
		if m.Active {
			active = append(active, &m)
		}
	}
	f.mu.Unlock()
	f.calls.Add(1)
	if f.activeGate != nil {
		select {
		case <-f.activeGate:
		case <-ctx.Done():
			return nil, ctx.Err()
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
	bus        *eventstest.Bus
	collector  *Collector
	handler    http.HandlerFunc
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, monitors: &fakeMonitors{}, heartbeats: &fakeHeartbeats{}, bus: eventstest.NewBus()}
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

func (r *rig) publishHeartbeat(monitorID string, status shared.MonitorStatus, responseMs int, at int64) {
	require.NoError(r.t, events.Publish(context.Background(), r.bus, events.HeartbeatEvent, heartbeat.Model{
		MonitorID: monitorID, Status: status, Ping: responseMs, Time: time.Unix(at, 0),
	}))
}

func (r *rig) publishMonitorUpdated(m monitor.Model) {
	require.NoError(r.t, events.Publish(context.Background(), r.bus, events.MonitorUpdated, m))
}

func (r *rig) publishMonitorDeleted(id string) {
	require.NoError(r.t, events.Publish(context.Background(), r.bus, events.MonitorDeleted, id))
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
	for line := range strings.SplitSeq(exposition, "\n") {
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

func TestHeartbeatGivesUpOnAMonitorLookupThatHangs(t *testing.T) {
	r := newRig(t)
	r.collector.lookupTimeout = 20 * time.Millisecond
	r.activeMonitor("m1", "API", "http")
	r.monitors.findGate = make(chan struct{}) // never opens
	r.start()

	handled := make(chan struct{})
	go func() {
		r.publishHeartbeat("m1", shared.MonitorStatusUp, 100, at100)
		close(handled)
	}()

	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("the heartbeat handler is stuck on the monitor lookup")
	}
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
}

func TestPausedMonitorLeavesUntilItsNextHeartbeat(t *testing.T) {
	r := newRig(t)
	m := r.activeMonitor("m1", "API", "http")
	r.activeMonitor("m2", "DB", "tcp")
	r.start()
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)
	r.publishHeartbeat("m2", shared.MonitorStatusDown, 0, at100)

	m.Active = false
	r.monitors.set(m)
	r.publishMonitorUpdated(m)

	// Only the paused Monitor's series are gone
	r.requireSeries(`
peekaping_monitor_status{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 1700000100
`)

	// Resuming brings nothing back until the Monitor is checked
	m.Active = true
	r.monitors.set(m)
	r.publishMonitorUpdated(m)
	r.requireSeries(`
peekaping_monitor_status{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 1700000100
`)

	r.publishHeartbeat("m1", shared.MonitorStatusUp, 120, at200)
	r.requireSeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000200
peekaping_monitor_response_seconds{` + api + `} 0.12
peekaping_monitor_status{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 1700000100
`)
}

func TestDeletedMonitorLeaves(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.activeMonitor("m2", "DB", "tcp")
	r.start()
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)
	r.publishHeartbeat("m2", shared.MonitorStatusDown, 0, at100)

	r.monitors.remove("m1")
	r.publishMonitorDeleted("m1")

	r.requireSeries(`
peekaping_monitor_status{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 1700000100
`)
}

func TestRenamedMonitorMovesToItsNewLabelsWithTheSameValues(t *testing.T) {
	r := newRig(t)
	m := r.activeMonitor("m1", "API", "http")
	r.start()
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)

	m.Name = "Checkout API"
	m.Type = "tcp"
	r.monitors.set(m)
	r.publishMonitorUpdated(m)

	const renamed = `monitor_id="m1",monitor_name="Checkout API",monitor_type="tcp"`
	r.requireSeries(`
peekaping_monitor_status{` + renamed + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + renamed + `} 1700000100
peekaping_monitor_response_seconds{` + renamed + `} 0.25
`)
}

func TestHeartbeatDeliveredOutOfOrderDoesNotOverwriteANewerOne(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.start()

	r.publishHeartbeat("m1", shared.MonitorStatusUp, 120, at200)
	r.publishHeartbeat("m1", shared.MonitorStatusDown, 0, at100)

	r.requireSeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000200
peekaping_monitor_response_seconds{` + api + `} 0.12
`)
}

func TestUpdateOfAMonitorWithoutAHeartbeatCreatesNoSeries(t *testing.T) {
	r := newRig(t)
	m := r.activeMonitor("m1", "API", "http")
	r.start()

	r.publishMonitorUpdated(m)

	r.requireSeries("")
}

func (r *rig) setLatest(monitorID string, status shared.MonitorStatus, responseMs int, at int64) {
	r.heartbeats.setLatest(heartbeat.Model{MonitorID: monitorID, Status: status, Ping: responseMs, Time: time.Unix(at, 0)})
}

func TestStartupLoadsTheLatestHeartbeatOfActiveMonitorsInTheBackground(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.activeMonitor("m2", "DB", "tcp")
	r.activeMonitor("no-heartbeat", "New", "http")
	r.monitors.set(monitor.Model{ID: "paused", Name: "Paused", Type: "http", Active: false})
	r.setLatest("m1", shared.MonitorStatusUp, 250, at100)
	r.setLatest("m2", shared.MonitorStatusDown, 0, at200)
	r.setLatest("paused", shared.MonitorStatusUp, 100, at100)
	gate := make(chan struct{})
	r.monitors.activeGate = gate

	// Start returns while the active Monitors are still being read
	r.start()
	r.requireSeries("")

	close(gate)
	r.eventuallySeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
peekaping_monitor_response_seconds{` + api + `} 0.25
peekaping_monitor_status{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="m2",monitor_name="DB",monitor_type="tcp"} 1700000200
`)
}

func TestStartupLoadDoesNotOverwriteAHeartbeatThatArrivedMeanwhile(t *testing.T) {
	r := newRig(t)
	r.collector.syncInterval = 10 * time.Millisecond
	r.activeMonitor("m1", "API", "http")
	r.setLatest("m1", shared.MonitorStatusUp, 250, at100)
	gate := make(chan struct{})
	r.heartbeats.gate = gate
	r.start()
	require.Eventually(t, func() bool { return r.heartbeats.calls.Load() == 1 }, 2*time.Second, time.Millisecond)

	// A newer Heartbeat arrives while the older one is being read from the database
	r.publishHeartbeat("m1", shared.MonitorStatusDown, 0, at200)
	close(gate)

	// Once the next pass has read the Monitors, the first has recorded what it read
	require.Eventually(t, func() bool { return r.monitors.activeCalls.Load() >= 2 }, 2*time.Second, time.Millisecond)
	r.requireSeries(`
peekaping_monitor_status{` + api + `} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000200
`)
}

func TestSyncKeepsAMonitorThatGotItsFirstHeartbeatWhileTheActiveOnesWereRead(t *testing.T) {
	r := newRig(t)
	r.collector.syncInterval = 10 * time.Millisecond
	gate := make(chan struct{})
	r.monitors.activeGate = gate
	r.start()
	require.Eventually(t, func() bool { return r.monitors.calls.Load() == 1 }, 2*time.Second, time.Millisecond)

	// The Monitor is created and checked after the first pass read the active ones
	r.activeMonitor("m1", "API", "http")
	r.publishHeartbeat("m1", shared.MonitorStatusUp, 250, at100)
	close(gate)

	// Once the next pass has read the Monitors, the first has finished
	require.Eventually(t, func() bool { return r.monitors.activeCalls.Load() >= 2 }, 2*time.Second, time.Millisecond)
	r.requireSeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
peekaping_monitor_response_seconds{` + api + `} 0.25
`)
}

func TestSyncRepairsMissedEvents(t *testing.T) {
	r := newRig(t)
	r.collector.syncInterval = 10 * time.Millisecond
	paused := r.activeMonitor("paused", "Paused", "http")
	renamed := r.activeMonitor("renamed", "DB", "tcp")
	r.activeMonitor("deleted", "Cache", "tcp")
	r.activeMonitor("unchanged", "API", "http")
	r.start()
	r.publishHeartbeat("paused", shared.MonitorStatusUp, 100, at100)
	r.publishHeartbeat("renamed", shared.MonitorStatusDown, 0, at100)
	r.publishHeartbeat("deleted", shared.MonitorStatusUp, 100, at100)
	r.publishHeartbeat("unchanged", shared.MonitorStatusUp, 250, at100)

	// None of these publish an event. "new" gets a Heartbeat no event announced.
	paused.Active = false
	r.monitors.set(paused)
	renamed.Name = "DB primary"
	renamed.Type = "postgres"
	r.monitors.set(renamed)
	r.monitors.remove("deleted")
	r.activeMonitor("new", "Search", "http")
	r.setLatest("new", shared.MonitorStatusPending, 0, at200)

	r.eventuallySeries(`
peekaping_monitor_status{monitor_id="renamed",monitor_name="DB primary",monitor_type="postgres"} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="renamed",monitor_name="DB primary",monitor_type="postgres"} 1700000100
peekaping_monitor_status{monitor_id="unchanged",monitor_name="API",monitor_type="http"} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="unchanged",monitor_name="API",monitor_type="http"} 1700000100
peekaping_monitor_response_seconds{monitor_id="unchanged",monitor_name="API",monitor_type="http"} 0.25
peekaping_monitor_status{monitor_id="new",monitor_name="Search",monitor_type="http"} 2
peekaping_monitor_last_heartbeat_timestamp_seconds{monitor_id="new",monitor_name="Search",monitor_type="http"} 1700000200
`)
}

func TestSyncStopsWhenTheContextIsCancelled(t *testing.T) {
	r := newRig(t)
	r.collector.syncInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.collector.Start(ctx, r.bus)
	require.Eventually(t, func() bool { return r.monitors.activeCalls.Load() >= 3 }, 2*time.Second, time.Millisecond)

	cancel()
	time.Sleep(50 * time.Millisecond) // a pass under way may still finish
	stopped := r.monitors.activeCalls.Load()
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, stopped, r.monitors.activeCalls.Load())
}

func TestStartReportsWhenTheSyncHasStopped(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.heartbeats.gate = make(chan struct{}) // holds the sync's read until ctx is done
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := r.collector.Start(ctx, r.bus)
	require.Eventually(t, func() bool { return r.heartbeats.calls.Load() == 1 }, 2*time.Second, time.Millisecond)

	select {
	case <-stopped:
		require.FailNow(t, "reported stopped while the sync was reading")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the sync didn't stop once ctx was done")
	}
	calls := r.monitors.calls.Load() + r.heartbeats.calls.Load()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, calls, r.monitors.calls.Load()+r.heartbeats.calls.Load(), "no reads once it reported stopped")
}

func TestScrapeReadsMemoryOnly(t *testing.T) {
	r := newRig(t)
	r.activeMonitor("m1", "API", "http")
	r.setLatest("m1", shared.MonitorStatusUp, 250, at100)
	r.start()
	// Once the startup sync has loaded the Monitor it has no more service calls to make
	r.eventuallySeries(`
peekaping_monitor_status{` + api + `} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{` + api + `} 1700000100
peekaping_monitor_response_seconds{` + api + `} 0.25
`)
	monitorCalls, heartbeatCalls := r.monitors.calls.Load(), r.heartbeats.calls.Load()

	for range 5 {
		r.body()
	}

	assert.Equal(t, monitorCalls, r.monitors.calls.Load())
	assert.Equal(t, heartbeatCalls, r.heartbeats.calls.Load())
}
