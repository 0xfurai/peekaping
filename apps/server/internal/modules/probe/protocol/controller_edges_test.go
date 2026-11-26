package protocol_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	helloPath       = "/probe/v1/hello"
	assignmentsPath = "/probe/v1/assignments"
)

// edgeProbes is the probe service of the hello and assignments tests: the stub of the auth tests
// with the probe that assignments read, and a record of the instances hello registered
type edgeProbes struct {
	*stubProbes
	current    *probe.Model
	currentErr error
	registered []*probe.Instance
}

func (p *edgeProbes) FindByID(context.Context, string) (*probe.Model, error) {
	return p.current, p.currentErr
}

func (p *edgeProbes) RegisterInstance(_ context.Context, _ string, instance *probe.Instance) error {
	p.registered = append(p.registered, instance)
	return nil
}

// fakeProxies is the proxy service that finds a proxy by its id, in whatever workspace
type fakeProxies struct {
	proxy.Service
	byID  map[string]*proxy.Model
	err   error
	finds []string
	mu    sync.Mutex
}

func (p *fakeProxies) FindByID(_ context.Context, _ workspace.ID, id string) (*proxy.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finds = append(p.finds, id)
	return p.byID[id], p.err
}

// assigningMonitors is the monitor service that says how many monitors a probe was assigned, and
// keeps the types it was asked to assign by
type assigningMonitors struct {
	monitor.Service
	assigned int
	err      error
	types    [][]string
}

func (m *assigningMonitors) AssignProbeToAllAnyWorkspace(_ context.Context, _ string, supportedTypes []string, _ func(context.Context) (bool, error)) (int, error) {
	m.types = append(m.types, supportedTypes)
	return m.assigned, m.err
}

// cancellingSource is the monitor source that ends the request it is read for and fails
type cancellingSource struct {
	cancel context.CancelFunc
}

func (s *cancellingSource) FindActiveByProbeAnyWorkspace(context.Context, string) ([]*monitor.Model, error) {
	s.cancel()
	return nil, errors.New("the request is over")
}

// edgeServer is the hello and assignments routes over fakes, with the probe-1 of the auth tests
type edgeServer struct {
	probes     *edgeProbes
	proxies    *fakeProxies
	monitors   *assigningMonitors
	controller *protocol.Controller
	router     *httpx.Router
}

func newEdgeServer(t *testing.T, source protocol.MonitorSource) *edgeServer {
	t.Helper()
	logger := zap.NewNop().Sugar()
	bus := eventstest.NewBus()
	s := &edgeServer{
		probes: &edgeProbes{stubProbes: newStub()},
		// The caller is an instance of the probe that is online and runs http and tcp monitors
		proxies:  &fakeProxies{byID: map[string]*proxy.Model{}},
		monitors: &assigningMonitors{},
	}
	s.probes.current = &probe.Model{ID: "probe-1", Name: "Frankfurt", Instances: []*probe.Instance{
		{ID: instanceID, SupportedTypes: []string{"http", "tcp"}, LastSeenAt: time.Now().UTC(), EnforcesPrivateTargetBlock: true},
	}}
	if source == nil {
		owned, _ := resultsMonitors()
		source = &fakeSource{active: owned}
	}
	s.controller = protocol.NewController(protocol.ControllerParams{
		Probes:   s.probes,
		Source:   source,
		Proxies:  s.proxies,
		Monitors: s.monitors,
		Waker:    protocol.NewWaker(bus, logger),
		Bus:      bus,
		Timing:   protocol.Timing{PollTimeout: 200 * time.Millisecond},
		Logger:   logger,
	})
	s.router = httpx.NewRouter()
	protocol.NewRoute(protocol.NewAuth(s.probes, nil, logger, shared.LocalProbeIsTheProcess), protocol.NewRateLimiter()).ConnectRoute(s.router.Group("/probe/v1"), s.controller)
	return s
}

func (s *edgeServer) serve(req *http.Request) *httptest.ResponseRecorder {
	for key, value := range credentials(goodToken, instanceID) {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

func (s *edgeServer) hello(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(probeproto.HelloRequest{
		InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"}, EnforcesPrivateTargetBlock: true,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, helloPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return s.serve(req)
}

func (s *edgeServer) assignments(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return s.serve(httptest.NewRequest(http.MethodGet, assignmentsPath, nil))
}

// monitorIDsOf are the ids of the monitors an assignments answer holds
func monitorIDsOf(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var reply probeproto.AssignmentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply), w.Body.String())
	ids := []string{}
	for _, m := range reply.Monitors {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestHello_AFailureToFindTheInstanceAnswers500AndRegistersNothing(t *testing.T) {
	s := newEdgeServer(t, nil)
	s.probes.instanceErr = errors.New("the database is down")

	w := s.hello(t)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Equal(t, "Internal server error", errorMessageOf(t, w))
	assert.Empty(t, s.probes.registered)
}

func TestHello_RegistersTheInstanceAndAnswersWithTheProbe(t *testing.T) {
	s := newEdgeServer(t, nil)

	w := s.hello(t)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var reply probeproto.HelloResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply), w.Body.String())
	assert.Equal(t, probeproto.ProbeInfo{ID: "probe-1", Name: "Frankfurt"}, reply.Probe)
	assert.Equal(t, 200, reply.PollTimeoutMs)
	assert.False(t, reply.BlockPrivateTargets, "no guard, no block")
	require.Len(t, s.probes.registered, 1)
	assert.Equal(t, instanceID, s.probes.registered[0].ID)
	assert.Equal(t, []string{"http"}, s.probes.registered[0].SupportedTypes)
	assert.True(t, s.probes.registered[0].EnforcesPrivateTargetBlock)
}

func TestHello_AssignsTheProbeItsMonitorsByTheTypesTheInstanceRuns(t *testing.T) {
	s := newEdgeServer(t, nil)
	s.monitors.assigned = 3

	w := s.hello(t)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, [][]string{{"http"}}, s.monitors.types)
}

func TestHello_AFailureToAssignAnswers503AfterTheInstanceIsRegistered(t *testing.T) {
	s := newEdgeServer(t, nil)
	s.monitors.err = errors.New("the database is down")

	w := s.hello(t)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Equal(t, "the server could not take the request just now: send it again", errorMessageOf(t, w))
	assert.Len(t, s.probes.registered, 1, "the instance is on record, and the probe's next hello assigns")
}

func TestProbeCalls_WithoutAnAuthenticatedProbeAreRefusedWith401(t *testing.T) {
	s := newEdgeServer(t, nil)
	request := func() *http.Request { return httptest.NewRequest(http.MethodPost, "/", nil) }
	ctx := context.Background()

	_, helloErr := s.controller.Hello(ctx, &protocol.HelloInput{Request: request()})
	_, assignmentsErr := s.controller.Assignments(ctx, &protocol.AssignmentsInput{Request: request()})
	_, resultsErr := s.controller.Results(ctx, &protocol.ResultsInput{Request: request()})

	for name, err := range map[string]error{"hello": helloErr, "assignments": assignmentsErr, "results": resultsErr} {
		var failure *httpx.ResponseError
		require.ErrorAs(t, err, &failure, name)
		assert.Equal(t, http.StatusUnauthorized, failure.Status, name)
		raw, marshalErr := json.Marshal(failure)
		require.NoError(t, marshalErr, name)
		var body struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(raw, &body), name)
		assert.Equal(t, "a probe token is required: send Authorization: Bearer <probe token>", body.Message, name)
	}
}

func TestAssignments_ReadsTheMonitorsOfTheInstance(t *testing.T) {
	s := newEdgeServer(t, nil)

	w := s.assignments(t)

	assert.Equal(t, []string{monCheckout, monSearch}, monitorIDsOf(t, w), "the http and the tcp monitor, by id; no push monitor and no dns")
}

func TestAssignments_ACallerThatFellOutOfTheOnlineWindowIsStillDealtItsMonitors(t *testing.T) {
	s := newEdgeServer(t, nil)
	// The middleware stamped the instance just now, but the probe read here has it from a moment
	// before: just outside the window
	s.probes.current.Instances[0].LastSeenAt = time.Now().UTC().Add(-probe.InstanceOnlineWindow - time.Second)

	w := s.assignments(t)

	assert.Equal(t, []string{monCheckout, monSearch}, monitorIDsOf(t, w))
}

func TestAssignments_ACallerThatTheProbeDoesNotHoldIsDealtNothing(t *testing.T) {
	s := newEdgeServer(t, nil)
	s.probes.current.Instances = nil

	w := s.assignments(t)

	assert.Equal(t, []string{}, monitorIDsOf(t, w))
}

func TestAssignments_AProbeThatIsGoneAnswers401(t *testing.T) {
	for name, current := range map[string]*probe.Model{
		"deleted":  nil,
		"disabled": {ID: "probe-1", Disabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			s := newEdgeServer(t, nil)
			s.probes.current = current

			w := s.assignments(t)

			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			assert.Equal(t, "invalid probe token", errorMessageOf(t, w))
		})
	}
}

func TestAssignments_AFailureToReadAnswers500(t *testing.T) {
	for name, break_ := range map[string]func(*edgeServer, *fakeSource){
		"the probe's monitors": func(_ *edgeServer, source *fakeSource) { source.err = errors.New("the database is down") },
		"the probe":            func(s *edgeServer, _ *fakeSource) { s.probes.currentErr = errors.New("the database is down") },
		"a proxy":              func(s *edgeServer, _ *fakeSource) { s.proxies.err = errors.New("the database is down") },
	} {
		t.Run(name, func(t *testing.T) {
			owned, _ := resultsMonitors()
			owned[0].ProxyId = "proxy-1"
			source := &fakeSource{active: owned}
			s := newEdgeServer(t, source)
			break_(s, source)

			w := s.assignments(t)

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Equal(t, "Internal server error", errorMessageOf(t, w))
		})
	}
}

func TestAssignments_AProxyIsReadOnceForTheMonitorsThatNameIt(t *testing.T) {
	owned, _ := resultsMonitors()
	owned[0].ProxyId, owned[1].ProxyId = "proxy-1", "proxy-1"
	s := newEdgeServer(t, &fakeSource{active: owned})

	w := s.assignments(t)

	assert.Equal(t, []string{monCheckout, monSearch}, monitorIDsOf(t, w), "a proxy that no longer exists is the monitor's without one")
	assert.Equal(t, []string{"proxy-1"}, s.proxies.finds)
}

func TestAssignments_APollWhoseProbeHungUpWhileItReadsGetsNoBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newEdgeServer(t, &cancellingSource{cancel: cancel})

	w := s.serve(httptest.NewRequest(http.MethodGet, assignmentsPath, nil).WithContext(ctx))

	assert.Equal(t, http.StatusNotModified, w.Code, "a failure nobody reads is not an error")
	assert.Empty(t, w.Body.String())
}
