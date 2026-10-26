package protocol_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	goodToken  = "pkp_good-token"
	instanceID = "3f6b1c1e-6f0a-4d0a-8d3b-2d9c1f6a7b10"
)

// stubProbes is the part of the probe service the bearer middleware calls
type stubProbes struct {
	probe.Service
	probes   map[string]*probe.Model
	known    map[string]bool // "probe id/instance id" of the instances that said hello
	findErr  error
	touchErr error
	touched  []touch
	// instanceTypes are the monitor types the instances that said hello run
	instanceTypes []string
	// instanceGone has FindInstance find none, though the instance was stamped by TouchInstance
	instanceGone bool
	// mu guards what the methods record: the results requests of a test may run side by side
	mu sync.Mutex

	// markOnline is what MarkOnline answers: whether this caller changed the probe's status
	markOnline     bool
	markOnlineErr  error
	markedOnline   []string    // the ids of the probes MarkOnline was asked about
	markedOnlineAt []time.Time // the times it was given, in the same order
}

type touch struct {
	probeID, instanceID string
	at                  time.Time
}

func newStub() *stubProbes {
	return &stubProbes{
		probes: map[string]*probe.Model{goodToken: {ID: "probe-1", Name: "Frankfurt"}},
		known:  map[string]bool{"probe-1/" + instanceID: true},
	}
}

func (s *stubProbes) FindByToken(ctx context.Context, token string) (*probe.Model, error) {
	return s.probes[token], s.findErr
}

// FindInstance is an instance that said hello, with the types its build runs
func (s *stubProbes) FindInstance(ctx context.Context, probeID, id string) (*probe.Instance, error) {
	if !s.known[probeID+"/"+id] || s.instanceGone {
		return nil, nil
	}
	return &probe.Instance{ID: id, SupportedTypes: s.instanceTypes}, nil
}

func (s *stubProbes) TouchInstance(ctx context.Context, probeID, id string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touched = append(s.touched, touch{probeID, id, at})
	return s.known[probeID+"/"+id], s.touchErr
}

func (s *stubProbes) MarkOnline(ctx context.Context, id string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markedOnline = append(s.markedOnline, id)
	s.markedOnlineAt = append(s.markedOnlineAt, at)
	return s.markOnline, s.markOnlineErr
}

// request goes through the middleware to a handler that records what the middleware left on the context
type outcome struct {
	called     bool
	probe      *probe.Model
	instanceID string
	hasProbe   bool
	// bus is where the middleware published
	bus *eventstest.Bus
}

func run(stub *stubProbes, middleware func(*protocol.Auth) httpx.Middleware, headers map[string]string) (*httptest.ResponseRecorder, *outcome) {
	return runOn(stub, shared.LocalProbeIsTheProcess, zap.NewNop().Sugar(), middleware, headers)
}

// runOn is run for an install whose local probe is up the way local says, logging to logger
func runOn(stub *stubProbes, local shared.LocalProbeLiveness, logger *zap.SugaredLogger, middleware func(*protocol.Auth) httpx.Middleware, headers map[string]string) (*httptest.ResponseRecorder, *outcome) {
	got := &outcome{bus: eventstest.NewBus()}
	auth := protocol.NewAuth(stub, got.bus, logger, local)
	handler := middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.called = true
		got.probe, got.instanceID, got.hasProbe = protocol.ProbeFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/probe/v1/hello", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w, got
}

func authenticate(a *protocol.Auth) httpx.Middleware { return a.Authenticate() }
func knownInstance(a *protocol.Auth) httpx.Middleware {
	// the chain a route other than hello has
	return func(next http.Handler) http.Handler { return a.Authenticate()(a.KnownInstance()(next)) }
}

func credentials(token, id string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "X-Probe-Instance": id}
}

func TestAuthenticate_LetsAProbeWithItsTokenAndInstanceIdThrough(t *testing.T) {
	stub := newStub()

	w, got := run(stub, authenticate, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, got.called)
	require.True(t, got.hasProbe)
	assert.Equal(t, "probe-1", got.probe.ID)
	assert.Equal(t, "Frankfurt", got.probe.Name)
	assert.Equal(t, instanceID, got.instanceID)
	assert.Empty(t, stub.touched, "hello is the one call that needs no instance on record, and records it itself")
}

func TestAuthenticate_AcceptsTheSchemeInAnyCase(t *testing.T) {
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		w, _ := run(newStub(), authenticate, map[string]string{"Authorization": scheme + " " + goodToken, "X-Probe-Instance": instanceID})

		assert.Equal(t, http.StatusOK, w.Code, scheme)
	}
}

func TestAuthenticate_Answers401WithoutAUsableToken(t *testing.T) {
	for name, header := range map[string]string{
		"no header":                    "",
		"another scheme":               "Basic " + goodToken,
		"the token alone":              goodToken,
		"the scheme alone":             "Bearer",
		"the scheme and spaces":        "Bearer    ",
		"two tokens":                   "Bearer " + goodToken + " " + goodToken,
		"a token the registry doesn't": "Bearer pkp_unknown",
		"an admin's JWT":               "Bearer eyJhbGciOiJIUzI1NiJ9.eyJ1c2VySWQiOiIxIn0.c2ln",
	} {
		t.Run(name, func(t *testing.T) {
			headers := map[string]string{"X-Probe-Instance": instanceID}
			if header != "" {
				headers["Authorization"] = header
			}

			w, got := run(newStub(), authenticate, headers)

			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			assert.False(t, got.called)
			assert.NotContains(t, w.Body.String(), goodToken)
		})
	}
}

func TestAuthenticate_SaysWhyAnUnknownTokenIsRefused(t *testing.T) {
	w, _ := run(newStub(), authenticate, credentials("pkp_unknown", instanceID))

	assert.JSONEq(t, `{"message":"invalid probe token","data":null}`, w.Body.String())
}

func TestAuthenticate_ChecksTheTokenBeforeTheInstanceId(t *testing.T) {
	w, _ := run(newStub(), authenticate, map[string]string{"Authorization": "Bearer pkp_unknown"})

	assert.Equal(t, http.StatusUnauthorized, w.Code, "a caller with no valid token learns nothing about the headers")
}

func TestAuthenticate_Answers400ForAMissingOrMalformedInstanceId(t *testing.T) {
	longest := strings.Repeat("a", 255)
	for name, tc := range map[string]struct {
		id     string
		status int
	}{
		"missing":             {"", http.StatusBadRequest},
		"a space inside":      {"two words", http.StatusBadRequest},
		"a leading space":     {" " + instanceID, http.StatusBadRequest},
		"a tab":               {"a\tb", http.StatusBadRequest},
		"not ASCII":           {"instância", http.StatusBadRequest},
		"256 characters":      {longest + "a", http.StatusBadRequest},
		"255 characters":      {longest, http.StatusOK},
		"one character":       {"a", http.StatusOK},
		"a UUID":              {instanceID, http.StatusOK},
		"punctuation is fine": {"host-1.example:8080/x_y~z", http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			headers := map[string]string{"Authorization": "Bearer " + goodToken}
			if tc.id != "" {
				headers["X-Probe-Instance"] = tc.id
			}

			w, _ := run(newStub(), authenticate, headers)

			assert.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
}

func TestAuthenticate_Answers400ForARepeatedInstanceHeader(t *testing.T) {
	stub := newStub()
	auth := protocol.NewAuth(stub, eventstest.NewBus(), zap.NewNop().Sugar(), shared.LocalProbeIsTheProcess)
	handler := auth.Authenticate()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("must not be called") }))
	req := httptest.NewRequest(http.MethodPost, "/probe/v1/hello", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Add("X-Probe-Instance", instanceID)
	req.Header.Add("X-Probe-Instance", "another")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "X-Probe-Instance")
}

func TestAuthenticate_Answers500WithoutTheCauseWhenTheLookupFails(t *testing.T) {
	stub := newStub()
	stub.findErr = errors.New("database is locked")

	w, got := run(stub, authenticate, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"message":"Internal server error","data":null}`, w.Body.String())
	assert.False(t, got.called)
}

func TestKnownInstance_StampsTheInstanceAndLetsItThrough(t *testing.T) {
	stub := newStub()
	before := time.Now()

	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, got.called)
	require.Len(t, stub.touched, 1)
	assert.Equal(t, "probe-1", stub.touched[0].probeID)
	assert.Equal(t, instanceID, stub.touched[0].instanceID)
	assert.False(t, stub.touched[0].at.Before(before.Add(-time.Second)), "stamped with the time of the request")
	assert.WithinDuration(t, time.Now(), stub.touched[0].at, time.Minute)
	assert.Equal(t, time.UTC, stub.touched[0].at.Location())
}

func TestKnownInstance_Answers409ToAnInstanceThatNeverSaidHello(t *testing.T) {
	w, got := run(newStub(), knownInstance, credentials(goodToken, "a-new-instance"))

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"message":"unknown instance: send hello first","data":null}`, w.Body.String())
	assert.False(t, got.called)
}

func TestKnownInstance_ReportsFailuresOfTheStamp(t *testing.T) {
	stub := newStub()
	stub.touchErr = errors.New("disk full")
	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "disk full")
	assert.False(t, got.called)
}

func offlineStub(markChangesIt bool) *stubProbes {
	stub := newStub()
	stub.probes[goodToken].Status = probe.StatusOffline
	stub.markOnline = markChangesIt
	return stub
}

func TestKnownInstance_BringsAnOfflineProbeOnlineAndSaysSo(t *testing.T) {
	stub := offlineStub(true)

	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, got.called)
	assert.Equal(t, []string{"probe-1"}, stub.markedOnline)
	online := eventstest.Published(got.bus, probe.ProbeOnline)
	require.Len(t, online, 1)
	assert.Equal(t, "probe-1", online[0].ProbeID)
	assert.Equal(t, "Frankfurt", online[0].ProbeName)
	assert.WithinDuration(t, time.Now(), online[0].Since, time.Minute, "since is the time of the request that brought the probe back")
	require.Len(t, stub.markedOnlineAt, 1)
	assert.True(t, stub.markedOnlineAt[0].Equal(online[0].Since), "the probe's history records the time of that request, which is the stamp's")
	require.Len(t, stub.touched, 1)
	assert.True(t, stub.markedOnlineAt[0].Equal(stub.touched[0].at))
	assert.Empty(t, eventstest.Published(got.bus, probe.ProbeOffline))
}

// Two API replicas may both see an offline probe and ask. Only the one that changed it announces it.
func TestKnownInstance_SaysNothingWhenAnotherCallerBroughtTheProbeOnline(t *testing.T) {
	stub := offlineStub(false)

	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, got.called)
	assert.Equal(t, []string{"probe-1"}, stub.markedOnline, "it asked")
	assert.Empty(t, eventstest.Published(got.bus, probe.ProbeOnline))
}

func TestKnownInstance_CostsAnOnlineProbeNoExtraQuery(t *testing.T) {
	stub := newStub()
	stub.probes[goodToken].Status = probe.StatusOnline
	stub.markOnline = true

	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, stub.markedOnline)
	assert.Empty(t, eventstest.Published(got.bus, probe.ProbeOnline))
}

func TestKnownInstance_DoesNotBringAProbeOnlineForAnInstanceThatNeverSaidHello(t *testing.T) {
	stub := offlineStub(true)

	w, got := run(stub, knownInstance, credentials(goodToken, "a-new-instance"))

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Empty(t, stub.markedOnline, "no instance is connected, so the probe is not")
	assert.Empty(t, eventstest.Published(got.bus, probe.ProbeOnline))
}

func TestKnownInstance_LetsTheRequestThroughWhenTheStatusCannotBeChanged(t *testing.T) {
	stub := offlineStub(true)
	stub.markOnlineErr = errors.New("database is locked")

	w, got := run(stub, knownInstance, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code, "the sweep sets the probe online within its interval")
	assert.True(t, got.called)
	assert.Empty(t, eventstest.Published(got.bus, probe.ProbeOnline))
}

func TestAuthenticate_LeavesAnOfflineProbeOfflineUntilItsInstanceIsKnown(t *testing.T) {
	stub := offlineStub(true)

	w, _ := run(stub, authenticate, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, stub.markedOnline, "hello records the instance first, then brings the probe online")
}

func TestKnownInstance_RefusesARouteThatDoesNotAuthenticateTheProbe(t *testing.T) {
	stub := newStub()
	auth := protocol.NewAuth(stub, eventstest.NewBus(), zap.NewNop().Sugar(), shared.LocalProbeIsTheProcess)
	handler := auth.KnownInstance()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("must not be called") }))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/probe/v1/bye", nil))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, stub.touched)
}

func TestProbeFromContext_FindsNothingOnARequestThatWasNotAuthenticated(t *testing.T) {
	p, id, ok := protocol.ProbeFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context())

	assert.Nil(t, p)
	assert.Empty(t, id)
	assert.False(t, ok)
}

// localToken is the token of the local probe in the stub
const localToken = "pkp_local-token"

// localStub has the local probe's token beside the good one
func localStub() *stubProbes {
	stub := newStub()
	stub.probes[localToken] = &probe.Model{ID: shared.LocalProbeID, Name: "Local"}
	return stub
}

func TestAuthenticate_RefusesTheLocalProbesTokenInSingleModeBeforeItReadsTheInstance(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"with an instance id":    credentials(localToken, instanceID),
		"without an instance id": {"Authorization": "Bearer " + localToken},
	} {
		t.Run(name, func(t *testing.T) {
			stub := localStub()
			stub.known[shared.LocalProbeID+"/"+instanceID] = true

			w, got := runOn(stub, shared.LocalProbeIsTheProcess, zap.NewNop().Sugar(), knownInstance, headers)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "this install runs in single mode, where the peekaping process checks the local probe's monitors; use a remote probe's token")
			assert.False(t, got.called)
			assert.Empty(t, stub.touched, "no instance of the local probe is stamped")
			assert.Empty(t, stub.markedOnline)
		})
	}
}

func TestAuthenticate_LogsTheRefusalOfTheLocalProbesTokenAtWarnWithoutTheToken(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	stub := localStub()

	runOn(stub, shared.LocalProbeIsTheProcess, zap.New(core).Sugar(), authenticate, credentials(localToken, instanceID))

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Message, "single mode")
	for _, entry := range entries {
		assert.NotContains(t, entry.Message, localToken)
		for _, field := range entry.Context {
			assert.NotContains(t, field.String, localToken)
		}
	}
}

func TestAuthenticate_LetsARemoteProbeThroughInSingleMode(t *testing.T) {
	stub := localStub()

	w, got := runOn(stub, shared.LocalProbeIsTheProcess, zap.NewNop().Sugar(), authenticate, credentials(goodToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, got.called)
	assert.Equal(t, "probe-1", got.probe.ID)
}

func TestAuthenticate_LetsTheLocalProbesInstancesThroughInClusterMode(t *testing.T) {
	stub := localStub()

	w, got := runOn(stub, shared.LocalProbeIsItsInstances, zap.NewNop().Sugar(), authenticate, credentials(localToken, instanceID))

	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, got.called)
	assert.Equal(t, shared.LocalProbeID, got.probe.ID)
}
