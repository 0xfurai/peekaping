package protocol_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const resultsPath = "/probe/v1/results"

// The ids of the tests' monitors. Monitor ids are UUIDs, and a result naming anything else is
// rejected before the monitor is looked up.
const (
	monCheckout  = "0a1b2c3d-0001-4000-8000-000000000001"
	monSearch    = "0a1b2c3d-0001-4000-8000-000000000002"
	monNightly   = "0a1b2c3d-0001-4000-8000-000000000003"
	monDNS       = "0a1b2c3d-0001-4000-8000-000000000004"
	monPaused    = "0a1b2c3d-0001-4000-8000-000000000005"
	monElsewhere = "0a1b2c3d-0001-4000-8000-000000000006"
	monGone      = "0a1b2c3d-0001-4000-8000-000000000007"
	monSkipped   = "0a1b2c3d-0001-4000-8000-000000000008"
)

// recordingIngester is the ingester.Service of a test: it keeps what it was asked to store, and
// fails the results that failOn names
type recordingIngester struct {
	attempts []*ingester.Result
	stored   []*ingester.Result
	failOn   func(*ingester.Result) error
	// hold, when set, runs as a result is handed over and before it is recorded, to keep a request
	// in the middle of storing
	hold func(*ingester.Result)
	// blockOn, when set, makes the ingester wait for the results it returns true for until the
	// context it was given ends, and fail with the context's error: a database that does not answer
	blockOn func(*ingester.Result) bool

	mu sync.Mutex
}

func (r *recordingIngester) Ingest(ctx context.Context, result *ingester.Result) error {
	if r.hold != nil {
		r.hold(result)
	}
	if r.blockOn != nil && r.blockOn(result) {
		// A server that never ends a store would hang the test: give up after a while
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
			return errors.New("the test's database never answered")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = append(r.attempts, result)
	if r.failOn != nil {
		if err := r.failOn(result); err != nil {
			return err
		}
	}
	r.stored = append(r.stored, result)
	return nil
}

// storedFor are the results that were stored for the monitor, in the order they were stored
func (r *recordingIngester) storedFor(monitorID string) []*ingester.Result {
	var got []*ingester.Result
	for _, result := range r.stored {
		if result.MonitorID == monitorID {
			got = append(got, result)
		}
	}
	return got
}

// fakeSource is the monitor source of probe-1: the active monitors it owns
type fakeSource struct {
	active []*monitor.Model
	err    error
	calls  int
	mu     sync.Mutex
}

func (s *fakeSource) FindActiveByProbe(_ context.Context, probeID string) ([]*monitor.Model, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if probeID != "probe-1" {
		return []*monitor.Model{}, nil
	}
	return s.active, s.err
}

// fakeMonitors is the monitor service that finds a monitor by its id, whoever's it is
type fakeMonitors struct {
	monitor.Service
	byID  map[string]*monitor.Model
	err   error
	finds []string
}

func (m *fakeMonitors) FindByID(_ context.Context, id string) (*monitor.Model, error) {
	m.finds = append(m.finds, id)
	return m.byID[id], m.err
}

// fakeMaintenance is the maintenance service of monitors that are, or are not, in a window
type fakeMaintenance struct {
	maintenance.Service
	under map[string]bool
}

func (f *fakeMaintenance) GetMaintenancesByMonitorID(_ context.Context, monitorID string) ([]*maintenance.Model, error) {
	if f.under[monitorID] {
		return []*maintenance.Model{{ID: "window-of-" + monitorID}}, nil
	}
	return nil, nil
}

func (f *fakeMaintenance) IsUnderMaintenance(context.Context, *maintenance.Model) (bool, error) {
	return true, nil
}

// resultsServer is the probe protocol's results route over fakes, with the probe-1 of the auth tests
type resultsServer struct {
	probes      *stubProbes
	router      *httpx.Router
	ingest      *recordingIngester
	source      *fakeSource
	monitors    *fakeMonitors
	maintenance *fakeMaintenance
}

// The monitors of the tests. probe-1 owns the active ones, and the source lists them. The others it
// doesn't list: a paused one, another probe's, and one that a listing paged past.
func resultsMonitors() (owned []*monitor.Model, others []*monitor.Model) {
	checkout := &monitor.Model{
		ID: monCheckout, Name: "Checkout", Type: "http", Active: true,
		Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 30, ResendInterval: 10,
		Config:   `{"url":"https://example.com/health","check_cert_expiry":true}`,
		ProbeIDs: []string{"probe-1"},
	}
	search := &monitor.Model{ID: monSearch, Name: "Search", Type: "tcp", Active: true, Interval: 30, Timeout: 10, Config: `{"host":"search.example.com","port":9200}`, ProbeIDs: []string{"probe-1"}}
	nightly := &monitor.Model{ID: monNightly, Name: "Nightly", Type: "push", Active: true, Config: `{"pushToken":"t"}`, ProbeIDs: []string{"probe-1"}}
	dns := &monitor.Model{ID: monDNS, Name: "DNS", Type: "dns", Active: true, ProbeIDs: []string{"probe-1"}}
	paused := &monitor.Model{ID: monPaused, Name: "Paused", Type: "http", Active: false, ProbeIDs: []string{"probe-1"}}
	elsewhere := &monitor.Model{ID: monElsewhere, Name: "Elsewhere", Type: "http", Active: true, ProbeIDs: []string{"probe-2"}}
	skipped := &monitor.Model{ID: monSkipped, Name: "Skipped", Type: "http", Active: true, Interval: 60, Timeout: 16, Config: `{"url":"https://example.com/skipped"}`, ProbeIDs: []string{"probe-1"}}
	// The source lists the active monitors of the probe, push monitors and types the probe can't run included
	return []*monitor.Model{checkout, search, nightly, dns}, []*monitor.Model{paused, elsewhere, skipped}
}

func newResultsServer(t *testing.T) *resultsServer {
	t.Helper()
	return newResultsServerWith(t, nil)
}

// newResultsServerWith is newResultsServer with a window of its own, instead of the memory window
func newResultsServerWith(t *testing.T, window protocol.Window) *resultsServer {
	t.Helper()
	return newResultsServerTimed(t, window, protocol.Timing{})
}

// newResultsServerTimed is newResultsServerWith with the server's timing, of which a zero value
// leaves the default
func newResultsServerTimed(t *testing.T, window protocol.Window, timing protocol.Timing) *resultsServer {
	t.Helper()
	owned, others := resultsMonitors()
	byID := map[string]*monitor.Model{}
	for _, m := range append(append([]*monitor.Model{}, owned...), others...) {
		byID[m.ID] = m
	}
	s := &resultsServer{
		ingest:      &recordingIngester{},
		source:      &fakeSource{active: owned},
		monitors:    &fakeMonitors{byID: byID},
		maintenance: &fakeMaintenance{under: map[string]bool{}},
	}
	probes := newStub()
	s.probes = probes
	// The probe's own list takes in dns, the build of its instance does not: what an instance may
	// send results for is decided by its own types
	probes.probes[goodToken].SupportedTypes = []string{"http", "tcp", "dns", "push"}
	probes.instanceTypes = []string{"http", "tcp", "push"}
	logger := zap.NewNop().Sugar()
	controller := protocol.NewController(protocol.ControllerParams{
		Probes:      probes,
		Source:      s.source,
		Monitors:    s.monitors,
		Ingester:    s.ingest,
		Maintenance: s.maintenance,
		Window:      window,
		Timing:      timing,
		Logger:      logger,
	})
	s.router = httpx.NewRouter()
	protocol.NewRoute(protocol.NewAuth(probes, nil, logger, shared.LocalProbeIsTheProcess), protocol.NewRateLimiter()).ConnectRoute(s.router.Group("/probe/v1"), controller)
	return s
}

// post sends the body as probe-1's instance
func (s *resultsServer) post(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	default:
		var err error
		raw, err = json.Marshal(b)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(http.MethodPost, resultsPath, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range credentials(goodToken, instanceID) {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

// results sends the results and returns the outcomes, which the server must have answered 200
func (s *resultsServer) results(t *testing.T, sent ...probeproto.Result) []probeproto.ResultOutcome {
	t.Helper()
	w := s.post(t, probeproto.ResultsRequest{Results: sent})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var reply probeproto.ResultsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply), w.Body.String())
	return reply.Results
}

// checkResult is an Up result of the monitor that started at start
func checkResult(id, monitorID string, start time.Time) probeproto.Result {
	return probeproto.Result{
		ID: id, MonitorID: monitorID, Status: probeproto.StatusUp, Message: "200 - OK", PingMs: 12,
		StartTime: start, EndTime: start.Add(12 * time.Millisecond),
	}
}

func accepted(id string) probeproto.ResultOutcome {
	return probeproto.ResultOutcome{ID: id, Accepted: true}
}

func rejected(id, reason string) probeproto.ResultOutcome {
	return probeproto.ResultOutcome{ID: id, Reason: reason}
}

func errorMessageOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Message
}

func TestResults_RejectsUnknownNotAssignedTooOldDuplicateInvalid(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-time.Minute)
	for _, tc := range []struct {
		name   string
		result func(r *probeproto.Result)
		// stored is a result the probe sent before, which the one under test repeats
		stored bool
		want   string
	}{
		{name: "a monitor that doesn't exist", result: func(r *probeproto.Result) { r.MonitorID = monGone }, want: probeproto.ReasonUnknownMonitor},
		{name: "a push monitor", result: func(r *probeproto.Result) { r.MonitorID = monNightly }, want: probeproto.ReasonNotAssigned},
		{name: "another probe's monitor", result: func(r *probeproto.Result) { r.MonitorID = monElsewhere }, want: probeproto.ReasonNotAssigned},
		{name: "a paused monitor of the probe", result: func(r *probeproto.Result) { r.MonitorID = monPaused }, want: probeproto.ReasonNotAssigned},
		{name: "a type the instance's build can't run, though its probe lists it", result: func(r *probeproto.Result) { r.MonitorID = monDNS }, want: probeproto.ReasonNotAssigned},
		{name: "a start more than an hour ago", result: func(r *probeproto.Result) {
			r.StartTime = now.Add(-time.Hour - time.Minute)
			r.EndTime = r.StartTime.Add(time.Second)
		}, want: probeproto.ReasonTooOld},
		{name: "an id the probe had stored", stored: true, result: func(r *probeproto.Result) {}, want: probeproto.ReasonDuplicate},
		{name: "a start more than a minute ahead", result: func(r *probeproto.Result) {
			r.StartTime = now.Add(2 * time.Minute)
			r.EndTime = r.StartTime.Add(time.Second)
		}, want: probeproto.ReasonInvalid},
		{name: "no id", result: func(r *probeproto.Result) { r.ID = "" }, want: probeproto.ReasonInvalid},
		{name: "no monitor", result: func(r *probeproto.Result) { r.MonitorID = "" }, want: probeproto.ReasonInvalid},
		{name: "a status that is not one of the four words", result: func(r *probeproto.Result) { r.Status = "great" }, want: probeproto.ReasonInvalid},
		{name: "no status", result: func(r *probeproto.Result) { r.Status = "" }, want: probeproto.ReasonInvalid},
		{name: "a status in capitals", result: func(r *probeproto.Result) { r.Status = "UP" }, want: probeproto.ReasonInvalid},
		{name: "no start", result: func(r *probeproto.Result) { r.StartTime = time.Time{} }, want: probeproto.ReasonInvalid},
		{name: "an end before the start", result: func(r *probeproto.Result) { r.EndTime = r.StartTime.Add(-time.Second) }, want: probeproto.ReasonInvalid},
		{name: "a negative response time", result: func(r *probeproto.Result) { r.PingMs = -1 }, want: probeproto.ReasonInvalid},
		{name: "a response time past the column", result: func(r *probeproto.Result) { r.PingMs = 3_000_000_000 }, want: probeproto.ReasonInvalid},
		{name: "TLS information that isn't an object", result: func(r *probeproto.Result) { r.TLSInfo = json.RawMessage(`"expires soon"`) }, want: probeproto.ReasonInvalid},
		{name: "a malformed item beats an unknown monitor", result: func(r *probeproto.Result) { r.ID, r.MonitorID = "", monGone }, want: probeproto.ReasonInvalid},
		{name: "an old result of an unknown monitor is too old", result: func(r *probeproto.Result) {
			r.MonitorID = monGone
			r.StartTime = now.Add(-2 * time.Hour)
			r.EndTime = r.StartTime.Add(time.Second)
		}, want: probeproto.ReasonTooOld},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newResultsServer(t)
			subject := checkResult("result-under-test", monCheckout, recent)
			tc.result(&subject)
			if tc.stored {
				require.Equal(t, []probeproto.ResultOutcome{accepted(subject.ID)}, s.results(t, subject))
				s.ingest.stored, s.ingest.attempts = nil, nil
			}
			control := checkResult("control", monCheckout, recent.Add(time.Second))

			got := s.results(t, subject, control)

			assert.Equal(t, []probeproto.ResultOutcome{rejected(subject.ID, tc.want), accepted("control")}, got,
				"one outcome for each result, in the order sent, and the rest of the batch is not held back")
			require.Len(t, s.ingest.stored, 1, "only the control is stored")
			assert.Equal(t, recent.Add(time.Second), s.ingest.stored[0].StartTime)
		})
	}
}

func TestResults_AcceptsAResultOfAMonitorTheListingSkipped(t *testing.T) {
	// Paging by offset skips a monitor when another is paused or deleted during the scan. The probe
	// drops a result that is not_assigned for good, so the monitor's own read decides.
	s := newResultsServer(t)
	for _, m := range s.source.active {
		require.NotEqual(t, monSkipped, m.ID, "the listing leaves the monitor out")
	}
	start := time.Now().UTC().Add(-time.Minute)

	got := s.results(t, checkResult("skipped", monSkipped, start), checkResult("control", monCheckout, start.Add(time.Second)))

	assert.Equal(t, []probeproto.ResultOutcome{accepted("skipped"), accepted("control")}, got)
	require.Len(t, s.ingest.storedFor(monSkipped), 1)
	assert.Equal(t, "Skipped", s.ingest.storedFor(monSkipped)[0].MonitorName, "it is stored with the monitor's own settings")
	assert.Equal(t, []string{monSkipped}, s.monitors.finds)
}

// An instance can be replaced to make room for a new one, or pruned, between the middleware's stamp
// and the results handler. It has to say hello again, and stores nothing meanwhile.
func TestResults_AnInstanceThatIsGoneMustSayHello(t *testing.T) {
	s := newResultsServer(t)
	s.probes.instanceGone = true

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{checkResult("a", monCheckout, time.Now().UTC().Add(-time.Minute))}})

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Empty(t, s.ingest.storedFor(monCheckout))
}

func TestResults_NonUUIDMonitorIDIsUnknownMonitor(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)

	// monitors.id is a UUID column, and PostgreSQL fails a lookup of anything else, which would
	// fail the whole batch and have the probe send it again for good
	got := s.results(t,
		checkResult("words", "no-such-monitor", start),
		checkResult("not-hex", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", start),
		checkResult("too-short", "0a1b2c3d-0001-4000-8000-00000000000", start),
		checkResult("control", monCheckout, start.Add(time.Second)),
	)

	assert.Equal(t, []probeproto.ResultOutcome{
		rejected("words", probeproto.ReasonUnknownMonitor),
		rejected("not-hex", probeproto.ReasonUnknownMonitor),
		rejected("too-short", probeproto.ReasonUnknownMonitor),
		accepted("control"),
	}, got, "unknown_monitor for each, and the rest of the batch is not held back")
	assert.Empty(t, s.monitors.finds, "no id that isn't a UUID reaches the database")
}

func TestResults_IngestsPerMonitorInStartTimeOrder(t *testing.T) {
	s := newResultsServer(t)
	base := time.Now().UTC().Add(-10 * time.Minute)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

	got := s.results(t,
		checkResult("c3", monCheckout, at(3)),
		checkResult("s2", monSearch, at(2)),
		checkResult("c1", monCheckout, at(1)),
		checkResult("s1", monSearch, at(1)),
		checkResult("c2", monCheckout, at(2)),
	)

	assert.Equal(t, []probeproto.ResultOutcome{accepted("c3"), accepted("s2"), accepted("c1"), accepted("s1"), accepted("c2")}, got,
		"the outcomes are in the order the results were sent")
	starts := func(monitorID string) []time.Time {
		var times []time.Time
		for _, stored := range s.ingest.storedFor(monitorID) {
			times = append(times, stored.StartTime)
		}
		return times
	}
	assert.Equal(t, []time.Time{at(1), at(2), at(3)}, starts(monCheckout), "each monitor's results arrive oldest first")
	assert.Equal(t, []time.Time{at(1), at(2)}, starts(monSearch))
}

func TestResults_ResultsOfOneMonitorStartingTogetherKeepTheOrderSent(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	first, second := checkResult("first", monCheckout, start), checkResult("second", monCheckout, start)
	first.Message, second.Message = "sent first", "sent second"

	s.results(t, first, second)

	require.Len(t, s.ingest.stored, 2)
	assert.Equal(t, []string{"sent first", "sent second"}, []string{s.ingest.stored[0].Message, s.ingest.stored[1].Message})
}

func TestResults_FillsTheMonitorLikePush(t *testing.T) {
	s := newResultsServer(t)
	zurich := time.FixedZone("CEST", 2*60*60)
	start := time.Now().In(zurich).Add(-5 * time.Minute).Truncate(time.Millisecond)
	end := start.Add(843 * time.Millisecond)

	got := s.results(t, probeproto.Result{
		ID: "result-1", MonitorID: monCheckout, Status: probeproto.StatusDown, Message: "connection refused", PingMs: 843,
		StartTime: start, EndTime: end,
		TLSInfo: json.RawMessage(`{"valid":true}`),
	})

	require.Equal(t, []probeproto.ResultOutcome{accepted("result-1")}, got)
	require.Len(t, s.ingest.stored, 1)
	stored := s.ingest.stored[0]
	assert.Equal(t, &ingester.Result{
		ProbeID:            "probe-1",
		MonitorID:          monCheckout,
		MonitorName:        "Checkout",
		MonitorType:        "http",
		MonitorInterval:    60,
		MonitorTimeout:     16,
		MonitorMaxRetries:  3,
		MonitorRetryInt:    30,
		MonitorResendInt:   10,
		MonitorConfig:      `{"url":"https://example.com/health","check_cert_expiry":true}`,
		Status:             shared.MonitorStatusDown,
		Message:            "connection refused",
		PingMs:             843,
		StartTime:          start.UTC(),
		EndTime:            end.UTC(),
		IsUnderMaintenance: false,
		TLSInfo:            &certificate.TLSInfo{Valid: true},
		CheckCertExpiry:    true,
	}, stored)
	assert.Equal(t, time.UTC, stored.StartTime.Location(), "the times are the probe's, in UTC")
	assert.Equal(t, time.UTC, stored.EndTime.Location())
}

func TestResults_ReadsEachOfTheFourStatusWords(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	words := map[string]shared.MonitorStatus{
		probeproto.StatusUp:          shared.MonitorStatusUp,
		probeproto.StatusDown:        shared.MonitorStatusDown,
		probeproto.StatusPending:     shared.MonitorStatusPending,
		probeproto.StatusMaintenance: shared.MonitorStatusMaintenance,
	}
	i := 0
	for word, want := range words {
		i++
		result := checkResult("result-"+word, monSearch, start.Add(time.Duration(i)*time.Second))
		result.Status = word

		require.Equal(t, []probeproto.ResultOutcome{accepted(result.ID)}, s.results(t, result), word)

		assert.Equal(t, want, s.ingest.stored[len(s.ingest.stored)-1].Status, word)
	}
}

func TestResults_AMonitorInAMaintenanceWindowReportsMaintenance(t *testing.T) {
	s := newResultsServer(t)
	s.maintenance.under[monCheckout] = true
	start := time.Now().UTC().Add(-time.Minute)
	down := checkResult("down", monCheckout, start)
	down.Status, down.Message, down.PingMs = probeproto.StatusDown, "connection refused", 250
	other := checkResult("other", monSearch, start)
	other.Status, other.Message = probeproto.StatusDown, "connection refused"

	s.results(t, down, other)

	require.Len(t, s.ingest.stored, 2)
	inWindow := s.ingest.storedFor(monCheckout)[0]
	assert.True(t, inWindow.IsUnderMaintenance)
	assert.Equal(t, shared.MonitorStatusMaintenance, inWindow.Status, "as the push URL does, planned downtime is neither Down nor Up")
	assert.Equal(t, "Monitor under maintenance", inWindow.Message)
	assert.Zero(t, inWindow.PingMs)
	outside := s.ingest.storedFor(monSearch)[0]
	assert.False(t, outside.IsUnderMaintenance)
	assert.Equal(t, shared.MonitorStatusDown, outside.Status)
	assert.Equal(t, "connection refused", outside.Message)
}

func TestResults_RetryAfterIngestErrorStoresNothingTwice(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	first := checkResult("first", monCheckout, start)
	second := checkResult("second", monCheckout, start.Add(time.Second))
	broken := true
	s.ingest.failOn = func(r *ingester.Result) error {
		if broken && r.StartTime.Equal(second.StartTime) {
			return errors.New("the database is down")
		}
		return nil
	}

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{first, second}})

	assert.Equal(t, http.StatusInternalServerError, w.Code, "a failure to store is the server's, so the probe keeps the batch and sends it again")
	assert.NotContains(t, w.Body.String(), "database", "and the probe is not told what failed")
	require.Len(t, s.ingest.stored, 1)

	broken = false
	got := s.results(t, first, second)

	assert.Equal(t, []probeproto.ResultOutcome{rejected("first", probeproto.ReasonDuplicate), accepted("second")}, got,
		"the result that was stored is a duplicate, and the one that wasn't is stored now")
	require.Len(t, s.ingest.stored, 2)
	assert.Equal(t, first.StartTime, s.ingest.stored[0].StartTime)
	assert.Equal(t, second.StartTime, s.ingest.stored[1].StartTime)
	assert.Len(t, s.ingest.attempts, 3, "the first result was tried once, and the second twice")
}

// A probe sends a batch again when it didn't hear the answer, and the retry can arrive while the
// first request is still storing. The ids are claimed before anything is stored, so the retry finds
// them taken. It is not told they are duplicates, though: the first request may still fail, and the
// probe drops a duplicate for good. It is told to come back, and finds them stored.
func TestResults_ARetryWhileTheFirstRequestIsStoringIsToldToComeBack(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	batch := probeproto.ResultsRequest{Results: []probeproto.Result{
		checkResult("first", monCheckout, start),
		checkResult("second", monCheckout, start.Add(time.Second)),
	}}
	// Every result handed to the ingester waits at the gate, with a signal that it got there
	entered := make(chan struct{}, 8)
	gate := make(chan struct{})
	s.ingest.hold = func(*ingester.Result) {
		entered <- struct{}{}
		<-gate
	}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- s.post(t, batch) }()
	<-entered // the first request is storing "first", with both ids claimed

	retry := s.post(t, batch)

	assert.Equal(t, http.StatusServiceUnavailable, retry.Code, retry.Body.String())
	assert.Equal(t, strconv.Itoa(int(protocol.PendingRetryAfter/time.Second)), retry.Header().Get("Retry-After"),
		"the probe waits that long before it sends the batch again")
	assert.Empty(t, s.ingest.attempts, "and the retry handed nothing to the ingester: the first request is still at its first result")
	close(gate)
	first := <-firstDone
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var reply probeproto.ResultsResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &reply))
	assert.Equal(t, []probeproto.ResultOutcome{accepted("first"), accepted("second")}, reply.Results)

	again := s.results(t, batch.Results...)

	assert.Equal(t, []probeproto.ResultOutcome{rejected("first", probeproto.ReasonDuplicate), rejected("second", probeproto.ReasonDuplicate)}, again,
		"once the first request has stored them, the ids are taken")
	require.Len(t, s.ingest.stored, 2, "one result for each id")
	assert.Equal(t, start, s.ingest.stored[0].StartTime)
	assert.Equal(t, start.Add(time.Second), s.ingest.stored[1].StartTime)
}

// A result whose id another request claimed and has not kept (it may be storing it, or it may have
// died) is neither stored nor answered duplicate. The rest of the batch is stored as usual, and the
// answer is 503 with a Retry-After, so that the probe sends the batch again: the results stored
// by now are duplicates then, and the one that was waiting is stored if its claim is gone.
func TestResults_AResultAnotherRequestHoldsIsNotStoredAndTheBatchIsToldToComeBack(t *testing.T) {
	window := protocol.NewMemoryWindow()
	s := newResultsServerWith(t, window)
	start := time.Now().UTC().Add(-time.Minute)
	away := checkResult("away", monCheckout, start)
	other := checkResult("other", monSearch, start)
	claim, err := window.Claim(context.Background(), "probe-1", "away", time.Now())
	require.NoError(t, err)
	require.Equal(t, protocol.ClaimWon, claim, "another request's claim")

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{away, other}})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.Equal(t, "a result in this batch is being stored by another request: send the batch again", errorMessageOf(t, w))
	require.Len(t, s.ingest.stored, 1, "the rest of the batch is stored as usual")
	assert.Equal(t, monSearch, s.ingest.stored[0].MonitorID)

	require.NoError(t, window.Release(context.Background(), "probe-1", "away"), "the other request gave it up")
	got := s.results(t, away, other)

	assert.Equal(t, []probeproto.ResultOutcome{accepted("away"), rejected("other", probeproto.ReasonDuplicate)}, got,
		"on the probe's retry the waiting result is stored, and the one stored before is a duplicate")
	assert.Len(t, s.ingest.stored, 2)
}

// A request that fails answers 500 for the whole batch, and the probe sends all of it again. What
// it claimed and did not store has to be free for that retry.
func TestResults_AFailedBatchFreesTheIdsItDidNotStore(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	stored := checkResult("stored", monCheckout, start)
	broken := checkResult("broken", monCheckout, start.Add(time.Second))
	waiting := checkResult("waiting", monSearch, start)
	down := true
	s.ingest.failOn = func(r *ingester.Result) error {
		if down && r.StartTime.Equal(broken.StartTime) {
			return errors.New("the database is down")
		}
		return nil
	}

	// The monitors' results are stored side by side, so the search monitor's result may or may not
	// have been tried when the checkout monitor's second one failed
	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{stored, broken, waiting}})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	waitingWasStored := len(s.ingest.storedFor(monSearch)) == 1
	require.Len(t, s.ingest.storedFor(monCheckout), 1, "a monitor's results are stored in turn: the one after the failure is not tried")

	down = false
	got := s.results(t, stored, broken, waiting)

	want := []probeproto.ResultOutcome{rejected("stored", probeproto.ReasonDuplicate), accepted("broken"), accepted("waiting")}
	if waitingWasStored {
		want[2] = rejected("waiting", probeproto.ReasonDuplicate)
	}
	assert.Equal(t, want, got, "only the results that were stored are duplicates")
	assert.Len(t, s.ingest.stored, 3)
}

// brokenWindow is a window whose claims fail from the nth on, as a Redis that stopped answering
// does, and that keeps what was released and what was kept
type brokenWindow struct {
	protocol.Window
	failFrom int
	claims   int
	released []string
	// kept are the ids of every Keep call, and keepErr is what Keep fails with, when set
	kept    []string
	keepErr error
	mu      sync.Mutex
}

func (w *brokenWindow) Keep(ctx context.Context, probeID string, ids ...string) error {
	w.mu.Lock()
	w.kept = append(w.kept, ids...)
	w.mu.Unlock()
	if w.keepErr != nil {
		return w.keepErr
	}
	return w.Window.Keep(ctx, probeID, ids...)
}

func (w *brokenWindow) Claim(ctx context.Context, probeID, id string, now time.Time) (protocol.ClaimOutcome, error) {
	w.claims++
	if w.claims >= w.failFrom {
		return 0, errors.New("redis: connection refused")
	}
	return w.Window.Claim(ctx, probeID, id, now)
}

func (w *brokenWindow) Release(ctx context.Context, probeID, id string) error {
	w.released = append(w.released, id)
	return w.Window.Release(ctx, probeID, id)
}

// The ids of the results that were stored are kept as the results are stored, whatever happens to
// the rest of the request: a request that dies afterwards must not leave them to run out. What was
// not stored is released, not kept.
func TestResults_KeepsTheIdsOfTheResultsItStored(t *testing.T) {
	window := &brokenWindow{Window: protocol.NewMemoryWindow(), failFrom: 1000}
	s := newResultsServerWith(t, window)
	start := time.Now().UTC().Add(-time.Minute)
	stored := checkResult("stored", monCheckout, start)
	broken := checkResult("broken", monCheckout, start.Add(time.Second))
	s.ingest.failOn = func(r *ingester.Result) error {
		if r.StartTime.Equal(broken.StartTime) {
			return errors.New("the database is down")
		}
		return nil
	}

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{stored, broken}})

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, []string{"stored"}, window.kept, "the stored one, though the batch failed")
	assert.Equal(t, []string{"broken"}, window.released, "and the one that was not stored is given back")
}

// keepCalls is a window that records the ids of each Keep call
type keepCalls struct {
	protocol.Window
	mu    sync.Mutex
	calls [][]string
}

func (w *keepCalls) Keep(ctx context.Context, probeID string, ids ...string) error {
	w.mu.Lock()
	w.calls = append(w.calls, append([]string(nil), ids...))
	w.mu.Unlock()
	return w.Window.Keep(ctx, probeID, ids...)
}

// The ids of a monitor's results are kept in one call (one pipeline in Redis), and the monitors'
// results are stored side by side: a batch of several monitors' results makes one call for each
func TestResults_KeepsAMonitorsIdsInOneCall(t *testing.T) {
	calls := &keepCalls{Window: protocol.NewMemoryWindow()}
	s := newResultsServerWith(t, calls)
	start := time.Now().UTC().Add(-time.Minute)

	got := s.results(t,
		checkResult("a1", monCheckout, start),
		checkResult("a2", monCheckout, start.Add(time.Second)),
		checkResult("b1", monSearch, start))

	assert.Equal(t, []probeproto.ResultOutcome{accepted("a1"), accepted("a2"), accepted("b1")}, got)
	calls.mu.Lock()
	defer calls.mu.Unlock()
	assert.ElementsMatch(t, [][]string{{"a1", "a2"}, {"b1"}}, calls.calls)
}

// A Keep that fails is logged and not the request's failure: the results are stored already, and a
// 500 would have the probe send them again
func TestResults_AKeepThatFailsDoesNotFailTheRequest(t *testing.T) {
	window := &brokenWindow{Window: protocol.NewMemoryWindow(), failFrom: 1000, keepErr: errors.New("redis: connection refused")}
	s := newResultsServerWith(t, window)
	start := time.Now().UTC().Add(-time.Minute)

	got := s.results(t, checkResult("first", monCheckout, start))

	assert.Equal(t, []probeproto.ResultOutcome{accepted("first")}, got)
	assert.Len(t, s.ingest.stored, 1)
	assert.Empty(t, window.released, "a stored result is not given back")
}

// A window that can't say whether an id was taken fails the batch with 503, for the probe to send
// it again. Storing a result that was not recorded could store it twice.
func TestResults_AWindowThatFailsAnswers503AndStoresNothing(t *testing.T) {
	window := &brokenWindow{Window: protocol.NewMemoryWindow(), failFrom: 2}
	s := newResultsServerWith(t, window)
	start := time.Now().UTC().Add(-time.Minute)
	first := checkResult("first", monCheckout, start)
	second := checkResult("second", monSearch, start)

	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{first, second}})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Empty(t, s.ingest.attempts, "nothing is stored, the ids of the batch are claimed before any is")
	assert.Equal(t, []string{"first"}, window.released, "and the id that was claimed is given back")

	window.failFrom = 1000
	got := s.results(t, first, second)

	assert.Equal(t, []probeproto.ResultOutcome{accepted("first"), accepted("second")}, got, "the probe's retry is accepted")
	assert.Len(t, s.ingest.stored, 2)
}

// The longest a request stores for is shorter than a claim lasts, so that a request can't be at it
// when its claims have run out, and longer than the probe waits for an answer, so that a probe that
// is still listening gets one
func TestStoreDeadline_IsLongerThanTheProbeWaitsAndShorterThanAClaimLasts(t *testing.T) {
	assert.Greater(t, protocol.StoreDeadline, 30*time.Second, "the probe gives up on a call after 30 seconds")
	assert.Less(t, protocol.StoreDeadline+10*time.Second, protocol.ClaimTTL, "with room for the claims, the keep and the release around it")
}

// A request stores for StoreDeadline at the most. When the ingester is still at it then (a database
// that does not answer), what was stored is kept, what was not is given back, and the answer is a
// 503 with a Retry-After, as for an id another request holds: the probe sends the batch again, and
// the claims of this request are gone or stored by then, never left to run out under a request that
// is still storing.
func TestResults_AStoreThatOutlivesTheDeadlineGivesBackWhatItDidNotStoreAndToldToComeBack(t *testing.T) {
	window := &brokenWindow{Window: protocol.NewMemoryWindow(), failFrom: 1000}
	s := newResultsServerTimed(t, window, protocol.Timing{StoreDeadline: 100 * time.Millisecond})
	start := time.Now().UTC().Add(-time.Minute)
	fast := checkResult("fast", monCheckout, start)
	stuck := checkResult("stuck", monSearch, start)
	s.ingest.blockOn = func(r *ingester.Result) bool { return r.MonitorID == monSearch }

	began := time.Now()
	w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{fast, stuck}})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.Equal(t, "storing the results took too long: send the batch again", errorMessageOf(t, w))
	assert.Less(t, time.Since(began), 5*time.Second, "the request ended at the deadline")
	require.Len(t, s.ingest.stored, 1)
	assert.Equal(t, monCheckout, s.ingest.stored[0].MonitorID)
	assert.Equal(t, []string{"fast"}, window.kept, "what was stored is kept")
	assert.Equal(t, []string{"stuck"}, window.released, "and what was not is given back")

	s.ingest.blockOn = nil
	got := s.results(t, fast, stuck)

	assert.Equal(t, []probeproto.ResultOutcome{rejected("fast", probeproto.ReasonDuplicate), accepted("stuck")}, got,
		"on the probe's retry the stored result is a duplicate and the other is stored")
}

// inFlight watches the results the ingester is storing at the same time, and holds each for a
// moment so that the ones that can overlap do
type inFlight struct {
	mu        sync.Mutex
	now, most int
	byMonitor map[string]int
	mostOfOne int
	holdFor   time.Duration
	// waitForAll, when set, has each result wait until this many are being stored, or a second
	waitForAll int
}

func newInFlight(holdFor time.Duration) *inFlight {
	return &inFlight{byMonitor: map[string]int{}, holdFor: holdFor}
}

func (f *inFlight) hold(r *ingester.Result) {
	f.mu.Lock()
	f.now++
	f.most = max(f.most, f.now)
	f.byMonitor[r.MonitorID]++
	f.mostOfOne = max(f.mostOfOne, f.byMonitor[r.MonitorID])
	f.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for f.waitForAll > 0 && time.Now().Before(deadline) {
		f.mu.Lock()
		enough := f.now >= f.waitForAll
		f.mu.Unlock()
		if enough {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(f.holdFor)

	f.mu.Lock()
	f.now--
	f.byMonitor[r.MonitorID]--
	f.mu.Unlock()
}

// addMonitors gives the probe n more monitors to send results for
func (s *resultsServer) addMonitors(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		m := &monitor.Model{
			ID: uuid.NewString(), Name: "Extra", Type: "http", Active: true, Interval: 60, Timeout: 16,
			Config: `{"url":"https://example.com/extra"}`, ProbeIDs: []string{"probe-1"},
		}
		s.source.active = append(s.source.active, m)
		s.monitors.byID[m.ID] = m
		ids[i] = m.ID
	}
	return ids
}

// A probe's batch holds results of many monitors, and each is a few queries to the database. Stored
// one after another, a batch of 500 takes as long as the database is far away, times 500. The results
// of different monitors don't depend on one another, so they are stored side by side.
func TestResults_StoresTheResultsOfDifferentMonitorsAtTheSameTime(t *testing.T) {
	s := newResultsServer(t)
	extra := s.addMonitors(1)[0]
	watch := newInFlight(0)
	watch.waitForAll = 4
	s.ingest.hold = watch.hold
	start := time.Now().UTC().Add(-time.Minute)

	got := s.results(t,
		checkResult("checkout", monCheckout, start),
		checkResult("search", monSearch, start),
		checkResult("extra", extra, start),
		checkResult("skipped", monSkipped, start),
	)

	assert.Equal(t, []probeproto.ResultOutcome{accepted("checkout"), accepted("search"), accepted("extra"), accepted("skipped")}, got,
		"the outcomes are in the order the results were sent")
	assert.Equal(t, 4, watch.most, "the four monitors' results were being stored at once")
}

// Ingest reads a monitor's previous heartbeat, so a monitor's results are stored one at a time,
// oldest first, whatever else is stored beside them
func TestResults_StoresOneMonitorsResultsInTurnWhileTheOthersGoOn(t *testing.T) {
	s := newResultsServer(t)
	watch := newInFlight(5 * time.Millisecond)
	s.ingest.hold = watch.hold
	base := time.Now().UTC().Add(-10 * time.Minute)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

	got := s.results(t,
		checkResult("c3", monCheckout, at(3)),
		checkResult("s2", monSearch, at(2)),
		checkResult("c1", monCheckout, at(1)),
		checkResult("s1", monSearch, at(1)),
		checkResult("c2", monCheckout, at(2)),
	)

	assert.Equal(t, []probeproto.ResultOutcome{accepted("c3"), accepted("s2"), accepted("c1"), accepted("s1"), accepted("c2")}, got)
	assert.Equal(t, 1, watch.mostOfOne, "never two results of one monitor at once")
	assert.Equal(t, 2, watch.most, "but the two monitors side by side")
	var checkout []time.Time
	for _, stored := range s.ingest.storedFor(monCheckout) {
		checkout = append(checkout, stored.StartTime)
	}
	assert.Equal(t, []time.Time{at(1), at(2), at(3)}, checkout, "oldest first")
}

// A batch of 500 monitors is not 500 connections to the database
func TestResults_StoresAtMostStoreConcurrencyMonitorsAtATime(t *testing.T) {
	s := newResultsServer(t)
	ids := s.addMonitors(3 * protocol.StoreConcurrency)
	watch := newInFlight(20 * time.Millisecond)
	s.ingest.hold = watch.hold
	start := time.Now().UTC().Add(-time.Minute)
	var batch []probeproto.Result
	for i, id := range ids {
		batch = append(batch, checkResult(fmt.Sprintf("r%d", i), id, start))
	}

	got := s.results(t, batch...)

	require.Len(t, got, len(batch))
	for _, outcome := range got {
		assert.True(t, outcome.Accepted)
	}
	assert.Equal(t, protocol.StoreConcurrency, watch.most, "as many as the limit, and no more")
	assert.Len(t, s.ingest.stored, len(batch))
}

// The database allows a number of connections, and every result being stored holds one: the requests
// of all a server's probe instances together store no more than StoreConcurrencyTotal at a time
func TestResults_StoresAtMostStoreConcurrencyTotalResultsAtATimeForAllRequests(t *testing.T) {
	s := newResultsServer(t)
	ids := s.addMonitors(2 * protocol.StoreConcurrency)
	watch := newInFlight(50 * time.Millisecond)
	s.ingest.hold = watch.hold
	const requests = 5 // the limiter lets an instance post 5 at once
	require.Greater(t, requests*protocol.StoreConcurrency, protocol.StoreConcurrencyTotal, "the requests ask for more than the total")
	start := time.Now().UTC().Add(-time.Minute)

	var wg sync.WaitGroup
	codes := make(chan int, requests)
	for request := range requests {
		var batch []probeproto.Result
		for i, id := range ids {
			batch = append(batch, checkResult(fmt.Sprintf("q%d-r%d", request, i), id, start.Add(time.Duration(request)*time.Second)))
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- s.post(t, probeproto.ResultsRequest{Results: batch}).Code
		}()
	}
	wg.Wait()
	close(codes)

	for code := range codes {
		assert.Equal(t, http.StatusOK, code)
	}
	assert.Len(t, s.ingest.stored, requests*len(ids), "every result is stored")
	assert.LessOrEqual(t, watch.most, protocol.StoreConcurrencyTotal, "never more than the total")
	assert.GreaterOrEqual(t, watch.most, protocol.StoreConcurrencyTotal-protocol.StoreConcurrency, "but the requests do share it")
}

func TestResults_AnIdTwiceInOneBatchIsStoredOnce(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)

	got := s.results(t, checkResult("same", monCheckout, start), checkResult("same", monCheckout, start))

	assert.Equal(t, []probeproto.ResultOutcome{accepted("same"), rejected("same", probeproto.ReasonDuplicate)}, got)
	assert.Len(t, s.ingest.stored, 1)
}

func TestResults_AnEmptyBatchIsAnEmptyList(t *testing.T) {
	for name, body := range map[string]string{
		"an empty list": `{"results":[]}`,
		"no list":       `{}`,
		"a null list":   `{"results":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newResultsServer(t)

			w := s.post(t, body)

			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.JSONEq(t, `{"results":[]}`, w.Body.String())
			assert.Empty(t, s.ingest.attempts)
		})
	}
}

func TestResults_AcceptsBatchMaxResultsAndRefusesOneMore(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	batch := func(n int) []probeproto.Result {
		results := make([]probeproto.Result, n)
		for i := range results {
			results[i] = checkResult("result-"+strconv.Itoa(i), monCheckout, start.Add(time.Duration(i)*time.Millisecond))
		}
		return results
	}

	got := s.results(t, batch(protocol.BatchMax)...)
	assert.Len(t, got, protocol.BatchMax)
	assert.Len(t, s.ingest.stored, protocol.BatchMax)

	w := s.post(t, probeproto.ResultsRequest{Results: batch(protocol.BatchMax + 1)})

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "batch holds 501 results, the limit is 500", errorMessageOf(t, w))
	assert.Len(t, s.ingest.stored, protocol.BatchMax, "and none of it is stored")
}

func TestResults_ABodyThatIsNotJSONAnswers400(t *testing.T) {
	for name, body := range map[string]string{
		"text":              `results`,
		"a list":            `[]`,
		"results of a type": `{"results":"none"}`,
		"an item of a type": `{"results":[{"start_time":"yesterday"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newResultsServer(t)

			w := s.post(t, body)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Empty(t, s.ingest.attempts)
		})
	}
}

func TestResults_ABodyOver1MBAnswers413BeforeReadingIt(t *testing.T) {
	s := newResultsServer(t)
	body := `{"results":[{"id":"a","message":"` + strings.Repeat("x", protocol.MaxBody) + `"}]}`

	w := s.post(t, body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	assert.Equal(t, protocol.MessageBodyTooLarge, errorMessageOf(t, w))
	assert.Zero(t, s.source.calls, "no monitor was read for it")
	assert.Empty(t, s.ingest.attempts)
}

func TestResults_ReadsTheProbesMonitorsOncePerBatch(t *testing.T) {
	s := newResultsServer(t)
	start := time.Now().UTC().Add(-time.Minute)
	var sent []probeproto.Result
	for i := range 5 {
		sent = append(sent,
			checkResult("owned-"+strconv.Itoa(i), monCheckout, start.Add(time.Duration(i)*time.Second)),
			checkResult("gone-"+strconv.Itoa(i), monGone, start.Add(time.Duration(i)*time.Second)),
			checkResult("elsewhere-"+strconv.Itoa(i), monElsewhere, start.Add(time.Duration(i)*time.Second)),
		)
	}

	got := s.results(t, sent...)

	require.Len(t, got, 15)
	assert.Equal(t, 1, s.source.calls)
	assert.ElementsMatch(t, []string{monGone, monElsewhere}, s.monitors.finds,
		"a monitor that isn't the probe's is looked up once, and one the probe owns not at all")
}

func TestResults_AFailureToReadTheMonitorsAnswers500(t *testing.T) {
	start := time.Now().UTC().Add(-time.Minute)
	for name, break_ := range map[string]func(*resultsServer){
		"the probe's monitors": func(s *resultsServer) { s.source.err = errors.New("the database is down") },
		"another monitor":      func(s *resultsServer) { s.monitors.err = errors.New("the database is down") },
	} {
		t.Run(name, func(t *testing.T) {
			s := newResultsServer(t)
			break_(s)

			w := s.post(t, probeproto.ResultsRequest{Results: []probeproto.Result{
				checkResult("a", monCheckout, start), checkResult("b", monElsewhere, start),
			}})

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Empty(t, s.ingest.attempts, "nothing is stored when the server can't tell what the results are for")
		})
	}
}
