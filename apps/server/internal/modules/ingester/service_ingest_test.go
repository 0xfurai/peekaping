package ingester

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	up          = shared.MonitorStatusUp
	down        = shared.MonitorStatusDown
	pending     = shared.MonitorStatusPending
	maintenance = shared.MonitorStatusMaintenance

	monitorID = "mon-1"
)

// fakeHeartbeatService keeps one monitor's heartbeats in memory; the service uses no other method
type fakeHeartbeatService struct {
	heartbeat.Service
	stored    []*heartbeat.Model
	findErr   error
	createErr error
}

// FindByMonitorIDAndProbeIDPaginated returns the latest heartbeat of the probe, which is all the
// service asks for. A heartbeat that names no probe is the local probe's.
func (s *fakeHeartbeatService) FindByMonitorIDAndProbeIDPaginated(_ context.Context, _, probeID string, _, _ int, _ *bool, _ bool) ([]*heartbeat.Model, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	for i := len(s.stored) - 1; i >= 0; i-- {
		if hb := s.stored[i]; hb.ProbeID == probeID || (hb.ProbeID == "" && probeID == shared.LocalProbeID) {
			return []*heartbeat.Model{hb}, nil
		}
	}
	return nil, nil
}

func (s *fakeHeartbeatService) Create(_ context.Context, dto *heartbeat.CreateUpdateDto) (*heartbeat.Model, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	hb := &heartbeat.Model{
		ID:        fmt.Sprintf("hb-%d", len(s.stored)+1),
		MonitorID: dto.MonitorID,
		ProbeID:   dto.ProbeID,
		Status:    dto.Status,
		Msg:       dto.Msg,
		Ping:      dto.Ping,
		Duration:  dto.Duration,
		DownCount: dto.DownCount,
		Retries:   dto.Retries,
		Important: dto.Important,
		Time:      dto.Time,
		EndTime:   dto.EndTime,
		Notified:  dto.Notified,
	}
	s.stored = append(s.stored, hb)
	return hb, nil
}

func (s *fakeHeartbeatService) latest() *heartbeat.Model {
	if len(s.stored) == 0 {
		return nil
	}
	return s.stored[len(s.stored)-1]
}

// fakeCertificateService records the monitors whose TLS info was updated and whose certificate expiry was checked
type fakeCertificateService struct {
	certificate.Service
	err     error
	updated []string
	checked []string
}

func (s *fakeCertificateService) UpdateTLSInfo(_ context.Context, id string, _ *certificate.TLSInfo) error {
	s.updated = append(s.updated, id)
	return s.err
}

func (s *fakeCertificateService) CheckCertificateExpiry(_ context.Context, _ *certificate.TLSInfo, id string, _ string) error {
	s.checked = append(s.checked, id)
	return s.err
}

// fakeLocations keeps the state of one monitor's probe rows in memory, as the monitor repository
// keeps it in monitor_probes
type fakeLocations struct {
	// probes are the probes the monitor runs on, in id order
	probes []string
	// offline are the probes that have no instance connected
	offline map[string]bool
	states  map[string]shared.LocationState
	err     error
	// records are the states recorded, oldest first
	records []shared.LocationState
	// recorded is the status the monitor last added up to, nil until a result is recorded
	recorded *shared.MonitorStatus
}

func (f *fakeLocations) rows() []shared.MonitorLocation {
	rows := make([]shared.MonitorLocation, len(f.probes))
	for i, probeID := range f.probes {
		state := f.states[probeID]
		rows[i] = shared.MonitorLocation{
			ProbeID: probeID, ProbeName: "Probe " + probeID,
			Online: probeID == shared.LocalProbeID || !f.offline[probeID],
			Status: state.Status, LastPing: state.LastPing, LastHeartbeatAt: state.LastHeartbeatAt,
		}
	}
	return rows
}

func (f *fakeLocations) RecordLocation(_ context.Context, _ string, state shared.LocationState, judge func(shared.LocationJudgement) *shared.MonitorStatus) error {
	if f.err != nil {
		return f.err
	}
	judgement := shared.LocationJudgement{Recorded: f.recorded, Before: f.rows()}
	f.records = append(f.records, state)
	if slices.Contains(f.probes, state.ProbeID) {
		f.states[state.ProbeID] = state
	}
	judgement.After = f.rows()
	f.recorded = judge(judgement)
	return nil
}

// setOffline takes the probe's instances away
func (f *fakeLocations) setOffline(probeID string) { f.offline[probeID] = true }

type testIngester struct {
	heartbeats *fakeHeartbeatService
	certs      *fakeCertificateService
	locations  *fakeLocations
	bus        *eventstest.Bus
	service    Service
	// statusEvents are the status changes published so far, which a step of locations_test.go
	// collects before it takes the topics
	statusEvents []shared.HeartBeatModel
}

// runningOn sets the probes the test monitor runs on. The default is the local probe alone.
func runningOn(probeIDs ...string) func(*fakeLocations) {
	return func(f *fakeLocations) { f.probes = slices.Sorted(slices.Values(probeIDs)) }
}

func newTestIngester(options ...func(*fakeLocations)) *testIngester {
	heartbeats := &fakeHeartbeatService{}
	certs := &fakeCertificateService{}
	locations := &fakeLocations{probes: []string{shared.LocalProbeID}, offline: map[string]bool{}, states: map[string]shared.LocationState{}}
	for _, option := range options {
		option(locations)
	}
	bus := eventstest.NewBus()
	service := NewService(heartbeats, certs, locations, bus, zap.NewNop().Sugar())
	return &testIngester{
		heartbeats: heartbeats,
		certs:      certs,
		locations:  locations,
		bus:        bus,
		service:    service,
	}
}

// monitorSettings are the monitor fields the state machine reads
type monitorSettings struct {
	maxRetries     int
	resendInterval int
	// locationRule and locationThreshold are the monitor's location rule: empty means any
	locationRule      shared.LocationRule
	locationThreshold int
}

func checkResult(m monitorSettings, status shared.MonitorStatus) Result {
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	return Result{
		MonitorID:         monitorID,
		MonitorName:       "Test",
		MonitorType:       "http",
		MonitorInterval:   60,
		MonitorTimeout:    48,
		MonitorMaxRetries: m.maxRetries,
		MonitorRetryInt:   60,
		MonitorResendInt:  m.resendInterval,

		MonitorLocationRule:      m.locationRule,
		MonitorLocationThreshold: m.locationThreshold,
		Status:                   status,
		Message:                  "check message",
		PingMs:                   12,
		StartTime:                start,
		EndTime:                  start.Add(12 * time.Millisecond),
	}
}

func (ti *testIngester) ingest(t *testing.T, payload Result) error {
	t.Helper()
	return ti.service.Ingest(context.Background(), &payload)
}

// previousHeartbeat is a stored heartbeat for seeding the monitor's history
func previousHeartbeat(status shared.MonitorStatus, retries int) *heartbeat.Model {
	return &heartbeat.Model{ID: "hb-0", MonitorID: monitorID, Status: status, Retries: retries}
}

// step is one check result and the heartbeat it should produce
type step struct {
	// settings, when set, are the monitor's settings from this step on, as when the monitor is edited
	settings  *monitorSettings
	check     shared.MonitorStatus
	want      shared.MonitorStatus
	retries   int
	downCount int
	important bool
	notified  bool
}

// runSteps seeds the monitor's previous heartbeat (nil: none yet), sends each check result and checks the stored
// heartbeat and the published events. MonitorStatusChanged is expected when the status differs from the previous
// heartbeat's, and ImportantHeartbeat when the heartbeat is notified.
func runSteps(t *testing.T, previous *heartbeat.Model, m monitorSettings, steps []step) {
	t.Helper()
	ti := newTestIngester()
	first := previous == nil
	var previousStatus shared.MonitorStatus
	if !first {
		ti.heartbeats.stored = []*heartbeat.Model{previous}
		previousStatus = previous.Status
	}

	for i, s := range steps {
		if s.settings != nil {
			m = *s.settings
		}
		require.NoError(t, ti.ingest(t, checkResult(m, s.check)), "step %d", i+1)

		got := ti.heartbeats.latest()
		assert.Equal(t, s.want, got.Status, "step %d: status", i+1)
		assert.Equal(t, s.retries, got.Retries, "step %d: retries", i+1)
		assert.Equal(t, s.downCount, got.DownCount, "step %d: down count", i+1)
		assert.Equal(t, s.important, got.Important, "step %d: important", i+1)
		assert.Equal(t, s.notified, got.Notified, "step %d: notified", i+1)

		wantEvents := []string{}
		if first || previousStatus != s.want {
			wantEvents = append(wantEvents, events.MonitorStatusChanged.Name())
		}
		if s.notified {
			wantEvents = append(wantEvents, events.ImportantHeartbeat.Name())
		}
		assert.Equal(t, wantEvents, ti.bus.TakeTopics(), "step %d: events", i+1)

		first, previousStatus = false, s.want
	}
}

// sequence is a run of check results for a monitor with no heartbeats yet
type sequence struct {
	name    string
	monitor monitorSettings
	steps   []step
}

func runSequences(t *testing.T, sequences []sequence) {
	for _, seq := range sequences {
		t.Run(seq.name, func(t *testing.T) {
			runSteps(t, nil, seq.monitor, seq.steps)
		})
	}
}

func TestIngester_StatusTransitions(t *testing.T) {
	tests := []struct {
		name     string
		previous *heartbeat.Model
		monitor  monitorSettings
		step     step
	}{
		{
			name: "first heartbeat Up is important and notifies",
			step: step{check: up, want: up, important: true, notified: true},
		},
		{
			name: "first heartbeat Down is important and notifies",
			step: step{check: down, want: down, retries: 1, important: true, notified: true},
		},
		{
			// PP-135 changes this: GLOSSARY.md says a failed check with retries left is Pending
			name:    "first heartbeat Down skips Pending even with retries left",
			monitor: monitorSettings{maxRetries: 3},
			step:    step{check: down, want: down, retries: 1, important: true, notified: true},
		},
		{
			// PP-134 changes this: GLOSSARY.md says notifications are sent when a monitor goes Down or comes back Up
			name: "first heartbeat Pending is important and notifies",
			step: step{check: pending, want: pending, important: true, notified: true},
		},
		{
			// PP-134 changes this: GLOSSARY.md says a maintenance window raises no notifications
			name: "first heartbeat Maintenance is important and notifies",
			step: step{check: maintenance, want: maintenance, important: true, notified: true},
		},

		{
			name:     "Up to Up is not important",
			previous: previousHeartbeat(up, 0),
			step:     step{check: up, want: up},
		},
		{
			name:     "Up to Down without max retries notifies",
			previous: previousHeartbeat(up, 0),
			step:     step{check: down, want: down, retries: 1, important: true, notified: true},
		},
		{
			name:     "Up to Down with retries left goes Pending",
			previous: previousHeartbeat(up, 0),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: down, want: pending, retries: 1},
		},
		{
			name:     "Up to Maintenance is not important",
			previous: previousHeartbeat(up, 0),
			step:     step{check: maintenance, want: maintenance},
		},
		{
			name:     "Up to a Pending check result is not important",
			previous: previousHeartbeat(up, 0),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: pending, want: pending},
		},

		{
			name:     "Down to Down is not important",
			previous: previousHeartbeat(down, 1),
			step:     step{check: down, want: down, retries: 2},
		},
		{
			name:     "Down to Up notifies",
			previous: previousHeartbeat(down, 1),
			step:     step{check: up, want: up, important: true, notified: true},
		},
		{
			name:     "Down to Maintenance is not important",
			previous: previousHeartbeat(down, 1),
			step:     step{check: maintenance, want: maintenance},
		},
		{
			name:     "Down stays Down when max retries is raised above the failed checks so far",
			previous: previousHeartbeat(down, 1),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: down, want: down, retries: 2},
		},
		{
			name:     "Down to a Pending check result stays Down and counts as a failed check",
			previous: previousHeartbeat(down, 1),
			step:     step{check: pending, want: down, retries: 2},
		},

		{
			name:     "Pending to Down with retries left stays Pending",
			previous: previousHeartbeat(pending, 1),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: down, want: pending, retries: 2},
		},
		{
			name:     "Pending to Down with retries used up notifies",
			previous: previousHeartbeat(pending, 3),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: down, want: down, retries: 4, important: true, notified: true},
		},
		{
			name:     "Pending to Down after max retries is lowered to 0 notifies",
			previous: previousHeartbeat(pending, 1),
			step:     step{check: down, want: down, retries: 2, important: true, notified: true},
		},
		{
			name:     "Pending to Up is important but doesn't notify",
			previous: previousHeartbeat(pending, 1),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: up, want: up, important: true},
		},
		{
			name:     "Pending to Maintenance is not important",
			previous: previousHeartbeat(pending, 1),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: maintenance, want: maintenance},
		},
		{
			name:     "Pending to a Pending check result resets the failed checks",
			previous: previousHeartbeat(pending, 1),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: pending, want: pending},
		},

		{
			name:     "Maintenance to Maintenance is not important",
			previous: previousHeartbeat(maintenance, 0),
			step:     step{check: maintenance, want: maintenance},
		},
		{
			// PP-131 changes this: GLOSSARY.md calls a heartbeat that moves the monitor to Up from another status important
			name:     "Maintenance to Up is not important",
			previous: previousHeartbeat(maintenance, 0),
			step:     step{check: up, want: up},
		},
		{
			// PP-131 changes this: GLOSSARY.md says a monitor going Down notifies
			name:     "Maintenance to Down is not important and doesn't notify",
			previous: previousHeartbeat(maintenance, 0),
			step:     step{check: down, want: down, retries: 1},
		},
		{
			name:     "Maintenance to Down with retries left goes Pending",
			previous: previousHeartbeat(maintenance, 0),
			monitor:  monitorSettings{maxRetries: 3},
			step:     step{check: down, want: pending, retries: 1},
		},
		{
			name:     "Maintenance to a Pending check result is not important",
			previous: previousHeartbeat(maintenance, 0),
			step:     step{check: pending, want: pending},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runSteps(t, tc.previous, tc.monitor, []step{tc.step})
		})
	}
}

func TestIngester_MaxRetries(t *testing.T) {
	runSequences(t, []sequence{
		{
			name:    "max retries 0: the first failed check is Down",
			monitor: monitorSettings{maxRetries: 0},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: down, want: down, retries: 2},
			},
		},
		{
			name:    "max retries 3: Pending for each retry, then Down",
			monitor: monitorSettings{maxRetries: 3},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
				{check: down, want: pending, retries: 2},
				{check: down, want: pending, retries: 3},
				{check: down, want: down, retries: 4, important: true, notified: true},
				{check: down, want: down, retries: 5},
			},
		},
		{
			name:    "Up resets the failed checks",
			monitor: monitorSettings{maxRetries: 3},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
				{check: down, want: pending, retries: 2},
				{check: up, want: up, important: true},
				{check: down, want: pending, retries: 1},
			},
		},
		{
			name:    "Up after Down resets the failed checks",
			monitor: monitorSettings{maxRetries: 1},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
				{check: down, want: down, retries: 2, important: true, notified: true},
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
			},
		},
	})
}

// A Down monitor leaves Down only for an Up result, so the Down notification is followed by an Up one.
func TestIngester_ADownMonitorStaysDownUntilItIsUp(t *testing.T) {
	runSequences(t, []sequence{
		{
			// A Docker monitor reports a starting container as a Pending check result
			name:    "a container that restarts: one Down notification, then one Up notification",
			monitor: monitorSettings{maxRetries: 0},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: pending, want: down, retries: 2},
				{check: pending, want: down, retries: 3},
				{check: up, want: up, important: true, notified: true},
			},
		},
		{
			name:    "raising max retries while Down sends no second Down notification",
			monitor: monitorSettings{maxRetries: 0},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{settings: &monitorSettings{maxRetries: 3}, check: down, want: down, retries: 2},
				{check: down, want: down, retries: 3},
				{check: down, want: down, retries: 4},
				{check: down, want: down, retries: 5},
				{check: up, want: up, important: true, notified: true},
			},
		},
		{
			// A Pending check result of a Down monitor is stored as Down, so it counts toward the
			// resend interval like any other Down heartbeat
			name:    "a container that keeps starting while Down resends on the resend interval",
			monitor: monitorSettings{maxRetries: 0, resendInterval: 2},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: pending, want: down, retries: 2, downCount: 1},
				{check: pending, want: down, retries: 3, notified: true},
				{check: pending, want: down, retries: 4, downCount: 1},
				{check: pending, want: down, retries: 5, notified: true},
				{check: up, want: up, important: true, notified: true},
			},
		},
	})
}

func TestIngester_ResendInterval(t *testing.T) {
	runSequences(t, []sequence{
		{
			name:    "resend interval 2 notifies again on every second Down heartbeat",
			monitor: monitorSettings{resendInterval: 2},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: down, want: down, retries: 2, downCount: 1},
				{check: down, want: down, retries: 3, notified: true},
				{check: down, want: down, retries: 4, downCount: 1},
				{check: down, want: down, retries: 5, notified: true},
			},
		},
		{
			name:    "resend interval 0 never notifies again",
			monitor: monitorSettings{resendInterval: 0},
			steps: []step{
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: down, want: down, retries: 2},
				{check: down, want: down, retries: 3},
				{check: down, want: down, retries: 4},
			},
		},
		{
			name:    "Up restarts the count toward the resend interval",
			monitor: monitorSettings{resendInterval: 3},
			steps: []step{
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: down, want: down, retries: 2, downCount: 1},
				{check: down, want: down, retries: 3, downCount: 2},
				{check: up, want: up, important: true, notified: true},
				{check: down, want: down, retries: 1, important: true, notified: true},
				{check: down, want: down, retries: 2, downCount: 1},
			},
		},
		{
			name:    "Up heartbeats never notify again",
			monitor: monitorSettings{resendInterval: 1},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: up, want: up},
				{check: up, want: up},
			},
		},
		{
			name:    "Pending heartbeats don't count toward the resend interval",
			monitor: monitorSettings{maxRetries: 3, resendInterval: 2},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
				{check: down, want: pending, retries: 2},
				{check: down, want: pending, retries: 3},
				{check: down, want: down, retries: 4, important: true, notified: true},
			},
		},
		{
			name:    "once Down, the resend interval counts the Down heartbeats after the one that took it Down",
			monitor: monitorSettings{maxRetries: 3, resendInterval: 2},
			steps: []step{
				{check: up, want: up, important: true, notified: true},
				{check: down, want: pending, retries: 1},
				{check: down, want: pending, retries: 2},
				{check: down, want: pending, retries: 3},
				{check: down, want: down, retries: 4, important: true, notified: true},
				{check: down, want: down, retries: 5, downCount: 1},
				{check: down, want: down, retries: 6, notified: true},
				{check: down, want: down, retries: 7, downCount: 1},
				{check: down, want: down, retries: 8, notified: true},
			},
		},
	})
}

func TestIngester_PublishesEvents(t *testing.T) {
	statusChanged, importantHeartbeat := events.MonitorStatusChanged.Name(), events.ImportantHeartbeat.Name()

	t.Run("MonitorStatusChanged only when the status changes, ImportantHeartbeat only when a notification is due", func(t *testing.T) {
		ti := newTestIngester()
		m := monitorSettings{maxRetries: 1, resendInterval: 2}

		for i, s := range []struct {
			check shared.MonitorStatus
			want  []string
		}{
			{up, []string{statusChanged, importantHeartbeat}}, // first heartbeat
			{up, []string{}},                                    // still Up
			{down, []string{statusChanged}},                     // Up to Pending
			{down, []string{statusChanged, importantHeartbeat}}, // Pending to Down
			{down, []string{}},                                  // still Down, down count 1
			{down, []string{importantHeartbeat}},                // resend
			{up, []string{statusChanged, importantHeartbeat}},   // Down to Up
			{down, []string{statusChanged}},                     // Up to Pending
			{up, []string{statusChanged}},                       // Pending to Up
			{maintenance, []string{statusChanged}},              // Up to Maintenance
			{maintenance, []string{}},                           // still Maintenance
		} {
			require.NoError(t, ti.ingest(t, checkResult(m, s.check)))
			assert.Equal(t, s.want, ti.bus.TakeTopics(), "heartbeat %d", i+1)
		}
	})

	t.Run("both events carry the stored heartbeat", func(t *testing.T) {
		ti := newTestIngester()

		require.NoError(t, ti.ingest(t, checkResult(monitorSettings{}, down)))

		stored := ti.heartbeats.latest()
		assert.Equal(t, []shared.HeartBeatModel{*stored}, eventstest.Published(ti.bus, events.MonitorStatusChanged))
		assert.Equal(t, []shared.HeartBeatModel{*stored}, eventstest.Published(ti.bus, events.ImportantHeartbeat))
	})
}

func TestIngester_HandlesFailures(t *testing.T) {
	t.Run("a failed write returns the error and publishes nothing", func(t *testing.T) {
		ti := newTestIngester()
		ti.heartbeats.createErr = errors.New("db down")

		err := ti.ingest(t, checkResult(monitorSettings{}, down))
		assert.ErrorIs(t, err, ti.heartbeats.createErr)
		assert.Empty(t, ti.bus.TakeTopics())
	})

	// PP-66 changes this: the ingest should fail, and send no notification
	t.Run("a failed read of the previous heartbeat is handled as the monitor's first", func(t *testing.T) {
		ti := newTestIngester()
		ti.heartbeats.stored = []*heartbeat.Model{previousHeartbeat(down, 1)}
		ti.heartbeats.findErr = errors.New("db down")

		require.NoError(t, ti.ingest(t, checkResult(monitorSettings{}, down)))

		got := ti.heartbeats.latest()
		assert.Equal(t, down, got.Status)
		assert.Equal(t, 1, got.Retries)
		assert.True(t, got.Important)
		assert.True(t, got.Notified)
		assert.Equal(t, []string{events.MonitorStatusChanged.Name(), events.ImportantHeartbeat.Name()}, ti.bus.TakeTopics())
	})
}

func TestIngester_TLSInfo(t *testing.T) {
	tlsInfo := &certificate.TLSInfo{Valid: true}
	monitorIDs := []string{monitorID}

	tests := []struct {
		name        string
		monitorType string
		tlsInfo     *certificate.TLSInfo
		checkExpiry bool
		certErr     error
		wantUpdated []string
		wantChecked []string
	}{
		{
			name:        "HTTP monitor updates TLS info and checks certificate expiry",
			monitorType: "http",
			tlsInfo:     tlsInfo,
			checkExpiry: true,
			wantUpdated: monitorIDs,
			wantChecked: monitorIDs,
		},
		{
			name:        "HTTP keyword monitor counts as HTTP",
			monitorType: "http-keyword",
			tlsInfo:     tlsInfo,
			checkExpiry: true,
			wantUpdated: monitorIDs,
			wantChecked: monitorIDs,
		},
		{
			name:        "expiry checks off only updates TLS info",
			monitorType: "http",
			tlsInfo:     tlsInfo,
			wantUpdated: monitorIDs,
		},
		{
			name:        "no TLS info skips the certificate service",
			monitorType: "http",
			checkExpiry: true,
		},
		{
			name:        "non-HTTP monitor ignores TLS info",
			monitorType: "tcp",
			tlsInfo:     tlsInfo,
			checkExpiry: true,
		},
		{
			name:        "certificate errors don't stop the heartbeat",
			monitorType: "http",
			tlsInfo:     tlsInfo,
			checkExpiry: true,
			certErr:     errors.New("tls store down"),
			wantUpdated: monitorIDs,
			wantChecked: monitorIDs,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ti := newTestIngester()
			ti.certs.err = tc.certErr
			payload := checkResult(monitorSettings{}, up)
			payload.MonitorType = tc.monitorType
			payload.TLSInfo = tc.tlsInfo
			payload.CheckCertExpiry = tc.checkExpiry

			require.NoError(t, ti.ingest(t, payload))
			assert.Len(t, ti.heartbeats.stored, 1)
			assert.Equal(t, tc.wantUpdated, ti.certs.updated)
			assert.Equal(t, tc.wantChecked, ti.certs.checked)
		})
	}
}
