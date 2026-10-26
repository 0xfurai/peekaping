package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routerTransport is an http.RoundTripper that serves each request with the app's router, in
// process: there is no listening socket, so the probeproto client talks to the real handlers
// without a network. It remembers what the client sent and what the server answered, so that a
// test can check the wire's details too.
type routerTransport struct {
	a *app

	mu        sync.Mutex
	requests  []*http.Request
	responses []*http.Response
}

func (rt *routerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The server sees the request as net/http's server would hand it over
	served := httptest.NewRequest(req.Method, req.URL.String(), req.Body).WithContext(req.Context())
	served.Header = req.Header.Clone()
	served.ContentLength = req.ContentLength
	recorder := httptest.NewRecorder()
	rt.a.router.ServeHTTP(recorder, served)

	resp := recorder.Result()
	resp.Request = req
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.requests = append(rt.requests, req)
	rt.responses = append(rt.responses, resp)
	return resp, nil
}

// last is the latest request the client made and the reply it got
func (rt *routerTransport) last(t *testing.T) (*http.Request, *http.Response) {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	require.NotEmpty(t, rt.requests, "the client made no request")
	return rt.requests[len(rt.requests)-1], rt.responses[len(rt.responses)-1]
}

// probeClient is a probeproto client of the app's probe protocol with the token and instance id
func (a *app) probeClient(token, instanceID string) (*probeproto.Client, *routerTransport) {
	rt := &routerTransport{a: a}
	return probeproto.NewClient("http://probe.test", token, instanceID, &http.Client{Transport: rt}), rt
}

// The wire package's client and the server's handlers agree: what the client sends the handlers
// accept, and what they answer the client reads. The client's own tests speak to a stand-in
// server, so this is the test that would notice the two drifting apart. Assignments and results
// join it with their routes.
func TestProbeContract_HelloAndBye(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	client, rt := a.probeClient(made.Token, instanceID)
	ctx := context.Background()

	welcome, err := client.Hello(ctx, &probeproto.HelloRequest{
		InstanceID:      instanceID,
		ProtocolVersion: probeproto.ProtocolVersion,
		ProbeVersion:    "0.1.0",
		SupportedTypes:  []string{"http", "push"},
		Hostname:        "edge-1.example.com",
		OS:              "linux",
		Arch:            "arm64",
	})

	require.NoError(t, err)
	assert.Equal(t, probeproto.ProtocolVersion, welcome.ProtocolVersion)
	assert.NotEmpty(t, welcome.ServerVersion)
	assert.WithinDuration(t, time.Now(), welcome.ServerTime, time.Minute)
	assert.Equal(t, probeproto.ProbeInfo{ID: made.ID, Name: "Frankfurt", Location: "eu-central"}, welcome.Probe)
	assert.Equal(t, 25000, welcome.PollTimeoutMs)
	assert.Equal(t, 2000, welcome.FlushIntervalMs)
	assert.Equal(t, 500, welcome.BatchMax)
	assert.Equal(t, 10000, welcome.BufferMax)

	req, resp := rt.last(t)
	assert.Equal(t, "gzip", req.Header.Get("Content-Encoding"), "the client gzips the body it sends")
	assert.Equal(t, "gzip", req.Header.Get("Accept-Encoding"))
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), "and the server gzips the reply it was allowed to")

	got := a.getProbe(t, made.ID)
	require.Len(t, got.Instances, 1)
	assert.Equal(t, instanceID, got.Instances[0].ID)
	assert.Equal(t, "edge-1.example.com", got.Instances[0].Hostname)
	assert.Equal(t, "0.1.0", got.Instances[0].Version)
	assert.Equal(t, []string{"http", "push"}, got.SupportedTypes)

	require.NoError(t, client.Bye(ctx))
	_, resp = rt.last(t)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	retired := a.getProbe(t, made.ID).Instances
	require.Len(t, retired, 1, "the instance that said bye is retired and not removed")
	assert.WithinDuration(t, time.Now().Add(-time.Minute), retired[0].LastSeenAt, 5*time.Second)

	// A server that can't speak the probe's protocol version says so, as an error with a status
	_, err = client.Hello(ctx, &probeproto.HelloRequest{InstanceID: instanceID, ProtocolVersion: 0})
	var upgrade *probeproto.Error
	require.ErrorAs(t, err, &upgrade)
	assert.Equal(t, http.StatusUpgradeRequired, upgrade.Status)
	assert.Contains(t, upgrade.Message, "upgrade the probe")
}

func TestProbeContract_ErrorsComeBackAsTypedErrors(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	client, _ := a.probeClient(made.Token, instanceID)
	ctx := context.Background()

	t.Run("a protocol version below the minimum is a 426", func(t *testing.T) {
		_, err := client.Hello(ctx, &probeproto.HelloRequest{InstanceID: instanceID, ProtocolVersion: 0})

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusUpgradeRequired, failure.Status)
		assert.Equal(t, "protocol version 0 is below the server's minimum 1: upgrade the probe", failure.Message)
		assert.Zero(t, failure.RetryAfter)
		assert.Empty(t, a.getProbe(t, made.ID).Instances, "and the server recorded nothing")
	})

	t.Run("an instance that never said hello is a 409", func(t *testing.T) {
		err := client.Bye(ctx)

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusConflict, failure.Status)
		assert.Equal(t, "unknown instance: send hello first", failure.Message)
	})

	t.Run("a token no probe has is a 401", func(t *testing.T) {
		stranger, _ := a.probeClient("pkp_a-token-no-probe-has", instanceID)

		_, err := stranger.Hello(ctx, &probeproto.HelloRequest{InstanceID: instanceID, ProtocolVersion: 1})

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusUnauthorized, failure.Status)
		assert.Equal(t, "invalid probe token", failure.Message)
	})

	t.Run("a malformed instance id is a 400", func(t *testing.T) {
		confused, _ := a.probeClient(made.Token, "two words")

		_, err := confused.Hello(ctx, &probeproto.HelloRequest{InstanceID: "two words", ProtocolVersion: 1})

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusBadRequest, failure.Status)
	})

	t.Run("a probe that calls too often is a 429 that says when to come back", func(t *testing.T) {
		eager := uuid.NewString()
		impatient, _ := a.probeClient(made.Token, eager)
		var failure *probeproto.Error
		for range 5 {
			_, err := impatient.Hello(ctx, &probeproto.HelloRequest{InstanceID: eager, ProtocolVersion: 1})
			require.NoError(t, err)
		}

		_, err := impatient.Hello(ctx, &probeproto.HelloRequest{InstanceID: eager, ProtocolVersion: 1})

		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusTooManyRequests, failure.Status)
		assert.Equal(t, time.Second, failure.RetryAfter)
		assert.Equal(t, "too many requests", failure.Message)
	})
}

func TestProbeContract_Assignments(t *testing.T) {
	a := localProbeApp(t, shortPoll)
	proxyID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", map[string]any{
		"protocol": "socks5", "host": "egress.example.com", "port": 1080, "auth": true, "username": "scout", "password": "s3cret",
	}, a.asAdmin), http.StatusCreated).ID
	monitorID := a.createHTTPMonitor(t, "Checkout", proxyID)
	instanceID := uuid.NewString()
	client, rt := a.probeClient(localToken, instanceID)
	ctx := context.Background()
	_, err := client.Hello(ctx, &probeproto.HelloRequest{InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"}})
	require.NoError(t, err)

	held, err := client.Assignments(ctx, "")

	require.NoError(t, err)
	require.NotNil(t, held)
	assert.NotEmpty(t, held.Version)
	require.Len(t, held.Monitors, 1)
	assert.Equal(t, monitorID, held.Monitors[0].ID)
	assert.Equal(t, "Checkout", held.Monitors[0].Name)
	assert.Equal(t, "http", held.Monitors[0].Type)
	require.NotNil(t, held.Monitors[0].Proxy)
	assert.Equal(t, probeproto.Proxy{
		ID: proxyID, Protocol: "socks5", Host: "egress.example.com", Port: 1080, Auth: true, Username: "scout", Password: "s3cret",
		UpdatedAt: held.Monitors[0].Proxy.UpdatedAt,
	}, *held.Monitors[0].Proxy)
	req, resp := rt.last(t)
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), "the server gzips the reply it was allowed to")

	unchanged, err := client.Assignments(ctx, held.Version)

	require.NoError(t, err, "a poll that times out is not an error")
	assert.Nil(t, unchanged, "it is (nil, nil)")
	req, resp = rt.last(t)
	assert.Equal(t, held.Version, req.URL.Query().Get("version"))
	assert.Equal(t, http.StatusNotModified, resp.StatusCode)

	other, err := client.Assignments(ctx, "a-version-the-server-never-made")
	require.NoError(t, err)
	require.NotNil(t, other, "and a version it does not hold is answered at once")
	assert.Equal(t, held.Version, other.Version)
}

func TestProbeContract_Results(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	client, rt := a.probeClient(localToken, instanceID)
	ctx := context.Background()
	welcome, err := client.Hello(ctx, &probeproto.HelloRequest{InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, SupportedTypes: []string{"http"}})
	require.NoError(t, err)
	up := checkResult("up", monitorID, checkedAt(0))
	up.TLSInfo = json.RawMessage(`{"valid":true,"certInfo":{"subject":"CN=example.com","issuer":"CN=Example CA","daysRemaining":42}}`)
	batch := &probeproto.ResultsRequest{Results: []probeproto.Result{
		up,
		checkResult("lost", "no-such-monitor", checkedAt(1)),
		checkResult("stale", monitorID, time.Now().UTC().Add(-3*time.Hour)),
	}}

	got, err := client.Results(ctx, batch)

	require.NoError(t, err)
	assert.Equal(t, &probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{
		{ID: "up", Accepted: true},
		{ID: "lost", Reason: probeproto.ReasonUnknownMonitor},
		{ID: "stale", Reason: probeproto.ReasonTooOld},
	}}, got, "the outcomes come back typed, in the order sent")
	req, resp := rt.last(t)
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "/probe/v1/results", req.URL.Path)
	assert.Equal(t, "gzip", req.Header.Get("Content-Encoding"), "the client gzips the batch")
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), "and the server gzips the outcomes")
	beats := a.storedHeartbeats(t, monitorID)
	require.Len(t, beats, 1, "the accepted result is stored, and the others are not")
	assert.Equal(t, "local", beats[0].ProbeID)
	tls := a.request(t, http.MethodGet, "/api/v1/monitors/"+monitorID+"/tls", nil, a.asAdmin)
	require.Equal(t, http.StatusOK, tls.Code, tls.Body.String())
	var tlsInfo struct {
		Data struct {
			Valid    bool `json:"valid"`
			CertInfo struct {
				Subject       string `json:"subject"`
				DaysRemaining int    `json:"daysRemaining"`
			} `json:"certInfo"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(tls.Body.Bytes(), &tlsInfo), tls.Body.String())
	assert.True(t, tlsInfo.Data.Valid, "with the certificate information the probe read: %s", tls.Body.String())
	assert.Equal(t, "CN=example.com", tlsInfo.Data.CertInfo.Subject)
	assert.Equal(t, 42, tlsInfo.Data.CertInfo.DaysRemaining)

	again, err := client.Results(ctx, batch)

	require.NoError(t, err)
	assert.Equal(t, probeproto.ResultOutcome{ID: "up", Reason: probeproto.ReasonDuplicate}, again.Results[0], "a batch sent again is answered duplicate")

	t.Run("a batch past the limit hello states is a 400", func(t *testing.T) {
		tooMany := &probeproto.ResultsRequest{}
		for i := range welcome.BatchMax + 1 {
			tooMany.Results = append(tooMany.Results, checkResult(fmt.Sprintf("result-%d", i), monitorID, checkedAt(0)))
		}

		_, err := client.Results(ctx, tooMany)

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusBadRequest, failure.Status)
		assert.Equal(t, "batch holds 501 results, the limit is 500", failure.Message)
	})

	t.Run("an instance that never said hello is a 409", func(t *testing.T) {
		stranger, _ := a.probeClient(localToken, uuid.NewString())

		_, err := stranger.Results(ctx, batch)

		var failure *probeproto.Error
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, http.StatusConflict, failure.Status)
	})
}
