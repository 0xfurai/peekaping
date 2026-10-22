package probeproto_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	probeToken = "pkp_test-token"
	instanceID = "3f6b1c1e-6f0a-4d0a-8d3b-2d9c1f6a7b10"
)

// gunzipJSON decodes a request body the client gzipped
func gunzipJSON(t *testing.T, r *http.Request, into any) {
	t.Helper()
	require.Equal(t, "gzip", r.Header.Get("Content-Encoding"), "the client gzips its request bodies")
	zr, err := gzip.NewReader(r.Body)
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(zr).Decode(into))
}

// reply writes body as JSON, gzipped when gzipped is set
func reply(w http.ResponseWriter, status int, body any, gzipped bool) {
	w.Header().Set("Content-Type", "application/json")
	var out io.Writer = w
	if gzipped {
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		defer zw.Close()
		out = zw
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(out).Encode(body)
}

func newClient(t *testing.T, handler http.HandlerFunc) *probeproto.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return probeproto.NewClient(server.URL, probeToken, instanceID, server.Client())
}

func TestClient_HelloSendsCredentialsAndAGzippedBody(t *testing.T) {
	want := probeproto.HelloRequest{
		InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, ProbeVersion: "0.1.0",
		SupportedTypes: []string{"http", "tcp"}, Hostname: "edge-1", OS: "linux", Arch: "arm64",
	}
	serverTime := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/probe/v1/hello", r.URL.Path)
		assert.Equal(t, "Bearer "+probeToken, r.Header.Get("Authorization"))
		assert.Equal(t, instanceID, r.Header.Get("X-Probe-Instance"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "gzip", r.Header.Get("Accept-Encoding"))
		var got probeproto.HelloRequest
		gunzipJSON(t, r, &got)
		assert.Equal(t, want, got)
		reply(w, http.StatusOK, probeproto.HelloResponse{
			ProtocolVersion: 1, ServerVersion: "0.0.47", ServerTime: serverTime,
			Probe:         probeproto.ProbeInfo{ID: "p1", Name: "Frankfurt", Location: "eu"},
			PollTimeoutMs: 25000, FlushIntervalMs: 2000, BatchMax: 500, BufferMax: 10000,
		}, false)
	})

	got, err := client.Hello(context.Background(), &want)

	require.NoError(t, err)
	assert.Equal(t, &probeproto.HelloResponse{
		ProtocolVersion: 1, ServerVersion: "0.0.47", ServerTime: serverTime,
		Probe:         probeproto.ProbeInfo{ID: "p1", Name: "Frankfurt", Location: "eu"},
		PollTimeoutMs: 25000, FlushIntervalMs: 2000, BatchMax: 500, BufferMax: 10000,
	}, got)
}

func TestClient_DecodesAGzippedReply(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{
			{ID: "r1", Accepted: true}, {ID: "r2", Reason: probeproto.ReasonDuplicate},
		}}, true)
	})

	got, err := client.Results(context.Background(), &probeproto.ResultsRequest{})

	require.NoError(t, err)
	assert.Equal(t, []probeproto.ResultOutcome{{ID: "r1", Accepted: true}, {ID: "r2", Reason: "duplicate"}}, got.Results)
}

func TestClient_ResultsPostsTheBatch(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	batch := probeproto.ResultsRequest{Results: []probeproto.Result{{
		ID: "r1", MonitorID: "m1", Status: probeproto.StatusDown, Message: "timeout", PingMs: 5000,
		StartTime: start, EndTime: start.Add(5 * time.Second), TLSInfo: json.RawMessage(`{"valid":false}`),
	}}}
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/probe/v1/results", r.URL.Path)
		var got probeproto.ResultsRequest
		gunzipJSON(t, r, &got)
		assert.Equal(t, batch, got)
		reply(w, http.StatusOK, probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{{ID: "r1", Accepted: true}}}, false)
	})

	got, err := client.Results(context.Background(), &batch)

	require.NoError(t, err)
	assert.Equal(t, []probeproto.ResultOutcome{{ID: "r1", Accepted: true}}, got.Results)
}

func TestClient_AssignmentsAsksForTheVersionItHolds(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/probe/v1/assignments", r.URL.Path)
		assert.Equal(t, "v 1&2", r.URL.Query().Get("version"), "the version is escaped")
		assert.Equal(t, "Bearer "+probeToken, r.Header.Get("Authorization"))
		assert.Equal(t, instanceID, r.Header.Get("X-Probe-Instance"))
		assert.Empty(t, r.Header.Get("Content-Encoding"), "a GET has no body to gzip")
		reply(w, http.StatusOK, probeproto.AssignmentsResponse{Version: "v2", Monitors: []probeproto.Monitor{{ID: "m1", Name: "Site", Type: "http"}}}, false)
	})

	got, err := client.Assignments(context.Background(), "v 1&2")

	require.NoError(t, err)
	assert.Equal(t, "v2", got.Version)
	require.Len(t, got.Monitors, 1)
	assert.Equal(t, "m1", got.Monitors[0].ID)
}

func TestClient_AssignmentsReturnsNothingOnNotModified(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) })

	got, err := client.Assignments(context.Background(), "v1")

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestClient_ByeAcceptsNoContent(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/probe/v1/bye", r.URL.Path)
		assert.Equal(t, "Bearer "+probeToken, r.Header.Get("Authorization"))
		assert.Equal(t, instanceID, r.Header.Get("X-Probe-Instance"))
		w.WriteHeader(http.StatusNoContent)
	})

	assert.NoError(t, client.Bye(context.Background()))
}

func TestClient_AnErrorReplyCarriesItsStatusMessageAndRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    probeproto.Error
	}{
		{"rate limit", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "3")
			reply(w, http.StatusTooManyRequests, map[string]any{"message": "too many requests", "data": nil}, false)
		}, probeproto.Error{Status: 429, Message: "too many requests", RetryAfter: 3 * time.Second}},
		{"upgrade, gzipped", func(w http.ResponseWriter, r *http.Request) {
			reply(w, http.StatusUpgradeRequired, map[string]any{"message": "upgrade the probe", "data": nil}, true)
		}, probeproto.Error{Status: 426, Message: "upgrade the probe"}},
		{"a body that isn't the API's", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad gateway upstream", http.StatusBadGateway)
		}, probeproto.Error{Status: 502, Message: "bad gateway upstream"}},
		{"no body", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
			probeproto.Error{Status: 401, Message: "Unauthorized"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(t, tc.handler)

			_, err := client.Hello(context.Background(), &probeproto.HelloRequest{})

			var got *probeproto.Error
			require.ErrorAs(t, err, &got)
			assert.Equal(t, tc.want, *got)
			assert.Contains(t, got.Error(), tc.want.Message)
		})
	}
}

func TestClient_AnErrorOnAssignmentsAndBye(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusConflict, map[string]any{"message": "unknown instance: send hello first", "data": nil}, false)
	})

	_, err := client.Assignments(context.Background(), "")
	var got *probeproto.Error
	require.ErrorAs(t, err, &got)
	assert.Equal(t, 409, got.Status)

	require.ErrorAs(t, client.Bye(context.Background()), &got)
	assert.Equal(t, "unknown instance: send hello first", got.Message)
}

func TestClient_AnUnreachableServerIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	client := probeproto.NewClient(server.URL, probeToken, instanceID, nil)

	err := client.Bye(context.Background())

	require.Error(t, err)
	var got *probeproto.Error
	assert.NotErrorAs(t, err, &got, "a failure to reach the server has no status")
}

func TestClient_BaseURLMayEndWithASlash(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := probeproto.NewClient(server.URL+"/", probeToken, instanceID, server.Client())

	require.NoError(t, client.Bye(context.Background()))

	assert.Equal(t, "/probe/v1/bye", path)
}

func TestClient_StopsWhenTheContextIsCancelled(t *testing.T) {
	released := make(chan struct{})
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { <-released })
	t.Cleanup(func() { close(released) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Assignments(ctx, "v1")

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// The field names are the contract, so they are checked against literals
func TestTypes_WireFormat(t *testing.T) {
	hello, err := json.Marshal(probeproto.HelloRequest{
		InstanceID: "i", ProtocolVersion: 1, ProbeVersion: "0.1.0", SupportedTypes: []string{"http"}, Hostname: "h", OS: "linux", Arch: "arm64",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"instance_id":"i","protocol_version":1,"probe_version":"0.1.0","supported_types":["http"],"hostname":"h","os":"linux","arch":"arm64"}`, string(hello))

	response, err := json.Marshal(probeproto.HelloResponse{
		ProtocolVersion: 1, ServerVersion: "0.0.47", ServerTime: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		Probe:         probeproto.ProbeInfo{ID: "p", Name: "n", Location: "l"},
		PollTimeoutMs: 25000, FlushIntervalMs: 2000, BatchMax: 500, BufferMax: 10000,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protocol_version":1,"server_version":"0.0.47","server_time":"2026-10-22T09:00:00Z","probe":{"id":"p","name":"n","location":"l"},"poll_timeout_ms":25000,"flush_interval_ms":2000,"batch_max":500,"buffer_max":10000}`, string(response))

	result, err := json.Marshal(probeproto.Result{ID: "r", MonitorID: "m", Status: probeproto.StatusUp, PingMs: 12,
		StartTime: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 10, 22, 9, 0, 1, 0, time.UTC)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"r","monitor_id":"m","status":"up","message":"","ping_ms":12,"start_time":"2026-10-22T09:00:00Z","end_time":"2026-10-22T09:00:01Z"}`, string(result))

	assert.Equal(t, 1, probeproto.ProtocolVersion)
	assert.Equal(t, "/probe/v1", probeproto.PathPrefix)
	assert.Equal(t, "X-Probe-Instance", probeproto.HeaderInstanceID)
}
