package apitest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

const (
	helloPath = "/probe/v1/hello"
	byePath   = "/probe/v1/bye"
)

// asProbe authenticates a request as an instance of the probe the token belongs to, the way the
// probe protocol asks: the token as a bearer and the instance id in a header
func asProbe(token, instanceID string) credentials {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(probeproto.HeaderInstanceID, instanceID)
	}
}

// withHeader sets one more header on a request
func withHeader(key, value string) credentials {
	return func(req *http.Request) { req.Header.Set(key, value) }
}

// helloFrom is the hello of a probe instance, as the probe binary would send it
func helloFrom(instanceID string) probeproto.HelloRequest {
	return probeproto.HelloRequest{
		InstanceID:      instanceID,
		ProtocolVersion: probeproto.ProtocolVersion,
		ProbeVersion:    "0.1.0",
		SupportedTypes:  []string{"http", "tcp", "dns"},
		Hostname:        "edge-1.example.com",
		OS:              "linux",
		Arch:            "arm64",
	}
}

// probeApp is an app with a remote probe, which is what the protocol is for
func probeApp(t *testing.T) (*app, probeWithToken) {
	t.Helper()
	a := newAppWithAdmin(t)
	return a, a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "eu-central"})
}

// sayHello makes an instance of the probe say hello and checks that the server welcomes it
func (a *app) sayHello(t *testing.T, token, instanceID string) probeproto.HelloResponse {
	t.Helper()
	w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(token, instanceID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var welcome probeproto.HelloResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &welcome))
	return welcome
}

func (a *app) getProbe(t *testing.T, id string) probeResponse {
	t.Helper()
	return decode[probeResponse](t, a.request(t, http.MethodGet, probesPath+"/"+id, nil, a.asAdmin), http.StatusOK)
}

func gzipBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(plain)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func gunzipBytes(t *testing.T, compressed []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	require.NoError(t, err)
	plain, err := io.ReadAll(zr)
	require.NoError(t, err)
	return plain
}

func TestProbeProtocol_RejectsBadTokens(t *testing.T) {
	a, made := probeApp(t)
	disabled := a.createProbe(t, map[string]any{"name": "Paris"})
	decode[probeResponse](t, a.request(t, http.MethodPut, probesPath+"/"+disabled.ID, map[string]any{"disabled": true}, a.asAdmin), http.StatusOK)
	instanceID := uuid.NewString()

	for _, tc := range []struct {
		name  string
		creds []credentials
	}{
		{"no credentials", nil},
		{"only the instance id", []credentials{withHeader(probeproto.HeaderInstanceID, instanceID)}},
		{"a garbage token", []credentials{asProbe("pkp_not-a-token-of-any-probe", instanceID)}},
		{"a disabled probe's token", []credentials{asProbe(disabled.Token, instanceID)}},
		{"the admin's JWT", []credentials{asProbe(a.accessToken, instanceID)}},
		{"an API key as a bearer", []credentials{asProbe(a.apiKey, instanceID)}},
		{"an API key in its own header", []credentials{a.withAPIKey, withHeader(probeproto.HeaderInstanceID, instanceID)}},
		{"a token without the scheme", []credentials{withHeader("Authorization", made.Token), withHeader(probeproto.HeaderInstanceID, instanceID)}},
		{"another scheme", []credentials{withHeader("Authorization", "Basic "+made.Token), withHeader(probeproto.HeaderInstanceID, instanceID)}},
	} {
		for _, path := range []string{helloPath, byePath} {
			t.Run(tc.name+" "+path, func(t *testing.T) {
				w := a.request(t, http.MethodPost, path, helloFrom(instanceID), tc.creds...)

				assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
				assert.NotEmpty(t, errorMessage(t, w.Body.Bytes()))
				assert.NotContains(t, w.Body.String(), made.Token)
			})
		}
	}
	assert.Empty(t, a.getProbe(t, made.ID).Instances, "a refused hello records nothing")
	assert.Empty(t, a.getProbe(t, disabled.ID).Instances)
}

func TestProbeProtocol_AProbeTokenOpensNothingButTheProbeRoutes(t *testing.T) {
	a, made := probeApp(t)

	for _, tc := range []call{
		{"monitors", http.MethodGet, "/api/v1/monitors", nil},
		{"probes", http.MethodGet, probesPath, nil},
		{"API keys", http.MethodGet, "/api/v1/api-keys", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := a.request(t, tc.method, tc.path, tc.body, asProbe(made.Token, uuid.NewString()))

			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
}

func TestProbeProtocol_NeedsTheInstanceHeader(t *testing.T) {
	a, made := probeApp(t)
	bearer := withHeader("Authorization", "Bearer "+made.Token)

	for _, tc := range []struct {
		name  string
		creds []credentials
	}{
		{"missing", []credentials{bearer}},
		{"empty", []credentials{bearer, withHeader(probeproto.HeaderInstanceID, "")}},
		{"with a space in it", []credentials{bearer, withHeader(probeproto.HeaderInstanceID, "two words")}},
		{"256 characters", []credentials{bearer, withHeader(probeproto.HeaderInstanceID, strings.Repeat("a", 256))}},
		{"not ASCII", []credentials{bearer, withHeader(probeproto.HeaderInstanceID, "instância")}},
	} {
		for _, path := range []string{helloPath, byePath} {
			t.Run(tc.name+" "+path, func(t *testing.T) {
				w := a.request(t, http.MethodPost, path, helloFrom("whatever"), tc.creds...)

				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Contains(t, errorMessage(t, w.Body.Bytes()), probeproto.HeaderInstanceID)
			})
		}
	}
	assert.Empty(t, a.getProbe(t, made.ID).Instances)
}

func TestProbeProtocol_UnknownInstanceMustHello(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()

	w := a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, instanceID))

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "unknown instance: send hello first", errorMessage(t, w.Body.Bytes()))

	a.sayHello(t, made.Token, instanceID)
	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, instanceID)).Code,
		"the same call works once the instance has said hello")
}

func TestProbeProtocol_AnotherProbesInstanceIsUnknown(t *testing.T) {
	a, made := probeApp(t)
	other := a.createProbe(t, map[string]any{"name": "Paris"})
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)

	w := a.request(t, http.MethodPost, byePath, nil, asProbe(other.Token, instanceID))

	assert.Equal(t, http.StatusConflict, w.Code, "an instance belongs to the probe whose token said hello for it")
	assert.Len(t, a.getProbe(t, made.ID).Instances, 1, "and the other probe's bye removed nothing")
}

// A hello of one probe must not take over an instance of another, and its refusal says so
func TestProbeProtocol_HelloRefusesAnInstanceIdAnotherProbeHas(t *testing.T) {
	a, made := probeApp(t)
	other := a.createProbe(t, map[string]any{"name": "Paris"})
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)

	w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(other.Token, instanceID))

	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, errorMessage(t, w.Body.Bytes()), "instance id")
	assert.Empty(t, a.getProbe(t, other.ID).Instances)
	assert.Equal(t, "edge-1.example.com", a.getProbe(t, made.ID).Instances[0].Hostname)
}

// The stamp is made by the middleware every route after hello has. Bye removes the instance it
// stamped, so the test runs the middleware in front of a handler that leaves the instance be,
// until the routes that don't remove it (assignments, results) come.
func TestProbeProtocol_StampsLastSeen(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)
	longAgo := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, a.container.Invoke(func(probes probe.Service) error {
		_, err := probes.TouchInstance(context.Background(), made.ID, instanceID, longAgo)
		return err
	}))
	require.WithinDuration(t, longAgo, a.getProbe(t, made.ID).Instances[0].LastSeenAt, time.Second, "the test starts from a known instant")

	var status int
	require.NoError(t, a.container.Invoke(func(auth *protocol.Auth) {
		chain := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }),
			auth.Authenticate(), auth.KnownInstance())
		req := httptest.NewRequest(http.MethodGet, "/probe/v1/assignments", nil)
		asProbe(made.Token, instanceID)(req)
		w := httptest.NewRecorder()
		chain.ServeHTTP(w, req)
		status = w.Code
	}))

	require.Equal(t, http.StatusNoContent, status)
	seen := a.getProbe(t, made.ID).Instances[0].LastSeenAt
	assert.True(t, seen.After(longAgo), "last seen moved on")
	assert.WithinDuration(t, time.Now(), seen, time.Minute)
}

func TestProbeProtocol_HelloRecordsTheInstance(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	before := time.Now()

	w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(made.Token, instanceID))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	var welcome probeproto.HelloResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &welcome))
	assert.Equal(t, 1, welcome.ProtocolVersion)
	assert.NotEmpty(t, welcome.ServerVersion)
	assert.WithinDuration(t, time.Now(), welcome.ServerTime, time.Minute)
	assert.Equal(t, time.UTC, welcome.ServerTime.Location())
	assert.Equal(t, probeproto.ProbeInfo{ID: made.ID, Name: "Frankfurt", Location: "eu-central"}, welcome.Probe)
	assert.Equal(t, 25000, welcome.PollTimeoutMs)
	assert.Equal(t, 2000, welcome.FlushIntervalMs)
	assert.Equal(t, 500, welcome.BatchMax)
	assert.Equal(t, 10000, welcome.BufferMax)
	// The reply is the protocol's own message, not the API's envelope
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.NotContains(t, raw, "data")
	assert.Contains(t, raw, "poll_timeout_ms")

	got := a.getProbe(t, made.ID)
	require.Len(t, got.Instances, 1)
	assert.Equal(t, probeInstanceResponse{
		ID: instanceID, Hostname: "edge-1.example.com", OS: "linux", Arch: "arm64", Version: "0.1.0",
		ProtocolVersion: 1, LastSeenAt: got.Instances[0].LastSeenAt,
	}, got.Instances[0])
	assert.WithinDuration(t, before, got.Instances[0].LastSeenAt, time.Minute)
	assert.Equal(t, []string{"http", "tcp", "dns"}, got.SupportedTypes)
}

func TestProbeProtocol_HelloAgainUpdatesTheSameInstance(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)

	again := helloFrom(instanceID)
	again.Hostname, again.ProbeVersion, again.SupportedTypes = "edge-2.example.com", "0.2.0", []string{"http"}
	w := a.request(t, http.MethodPost, helloPath, again, asProbe(made.Token, instanceID))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := a.getProbe(t, made.ID)
	require.Len(t, got.Instances, 1)
	assert.Equal(t, "edge-2.example.com", got.Instances[0].Hostname)
	assert.Equal(t, "0.2.0", got.Instances[0].Version)
	assert.Equal(t, []string{"http"}, got.SupportedTypes)
}

func TestProbeProtocol_TheLocalProbeSaysHelloWithTheBootstrapToken(t *testing.T) {
	const token = "bootstrap-token-0123456789-abcdefghijklmnop"
	a := newAppWithAdmin(t, func(cfg *config.Config) { cfg.ProbeBootstrapToken = token })
	instanceID := uuid.NewString()

	welcome := a.sayHello(t, token, instanceID)

	assert.Equal(t, "local", welcome.Probe.ID)
	assert.Equal(t, "Local", welcome.Probe.Name)
	assert.Len(t, a.getProbe(t, "local").Instances, 1)
}

func TestProbeProtocol_HelloRejectsOldProtocol(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	old := helloFrom(instanceID)
	old.ProtocolVersion = 0

	w := a.request(t, http.MethodPost, helloPath, old, asProbe(made.Token, instanceID))

	assert.Equal(t, http.StatusUpgradeRequired, w.Code, w.Body.String())
	assert.Equal(t, "protocol version 0 is below the server's minimum 1: upgrade the probe", errorMessage(t, w.Body.Bytes()))
	got := a.getProbe(t, made.ID)
	assert.Empty(t, got.Instances, "nothing is recorded for a probe the server can't speak with")
	assert.Equal(t, []string{}, got.SupportedTypes)
}

func TestProbeProtocol_HelloRejectsABadBody(t *testing.T) {
	a, made := probeApp(t)

	// Each case is an instance of its own: one instance may say hello five times at once
	hello := func(change func(*probeproto.HelloRequest)) func(id string) any {
		return func(id string) any {
			req := helloFrom(id)
			change(&req)
			return req
		}
	}
	for _, tc := range []struct {
		name string
		body func(id string) any
		want string
	}{
		{"not JSON", func(string) any { return "{" }, "invalid request body"},
		{"empty", func(string) any { return "" }, "invalid request body"},
		{"a different instance id from the header's", hello(func(r *probeproto.HelloRequest) { r.InstanceID = "someone-else" }), "instance_id"},
		{"no instance id", hello(func(r *probeproto.HelloRequest) { r.InstanceID = "" }), "instance_id"},
		{"a hostname the registry can't hold", hello(func(r *probeproto.HelloRequest) { r.Hostname = strings.Repeat("h", 256) }), "hostname"},
		{"an os the registry can't hold", hello(func(r *probeproto.HelloRequest) { r.OS = strings.Repeat("o", 65) }), "os"},
		{"an arch the registry can't hold", hello(func(r *probeproto.HelloRequest) { r.Arch = strings.Repeat("a", 65) }), "arch"},
		{"a probe version the registry can't hold", hello(func(r *probeproto.HelloRequest) { r.ProbeVersion = strings.Repeat("v", 65) }), "probe_version"},
		{"an empty monitor type", hello(func(r *probeproto.HelloRequest) { r.SupportedTypes = []string{"http", ""} }), "supported_types"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instanceID := uuid.NewString()

			w := a.request(t, http.MethodPost, helloPath, tc.body(instanceID), asProbe(made.Token, instanceID))

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, errorMessage(t, w.Body.Bytes()), tc.want)
		})
	}
	assert.Empty(t, a.getProbe(t, made.ID).Instances)
}

func TestProbeProtocol_ByeRemovesTheInstance(t *testing.T) {
	a, made := probeApp(t)
	staying, leaving := uuid.NewString(), uuid.NewString()
	a.sayHello(t, made.Token, staying)
	a.sayHello(t, made.Token, leaving)
	require.Len(t, a.getProbe(t, made.ID).Instances, 2)

	w := a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, leaving))

	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Empty(t, w.Body.String())
	instances := a.getProbe(t, made.ID).Instances
	require.Len(t, instances, 1)
	assert.Equal(t, staying, instances[0].ID)
	assert.Equal(t, http.StatusConflict, a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, leaving)).Code,
		"a probe that said bye has to say hello again")
}

func TestProbeProtocol_GzipBodiesAndReplies(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	body, err := json.Marshal(helloFrom(instanceID))
	require.NoError(t, err)

	t.Run("a gzipped body is accepted", func(t *testing.T) {
		w := a.request(t, http.MethodPost, helloPath, string(gzipBytes(t, body)), asProbe(made.Token, instanceID), withHeader("Content-Encoding", "gzip"))

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Empty(t, w.Header().Get("Content-Encoding"), "the client didn't ask for a gzipped reply")
		assert.Len(t, a.getProbe(t, made.ID).Instances, 1)
	})

	t.Run("a reply is gzipped when the request allows it", func(t *testing.T) {
		w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(made.Token, instanceID), withHeader("Accept-Encoding", "gzip"))

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
		assert.Contains(t, w.Header().Values("Vary"), "Accept-Encoding")
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
		var welcome probeproto.HelloResponse
		require.NoError(t, json.Unmarshal(gunzipBytes(t, w.Body.Bytes()), &welcome))
		assert.Equal(t, made.ID, welcome.Probe.ID)
	})

	t.Run("both together", func(t *testing.T) {
		w := a.request(t, http.MethodPost, helloPath, string(gzipBytes(t, body)), asProbe(made.Token, instanceID),
			withHeader("Content-Encoding", "gzip"), withHeader("Accept-Encoding", "gzip, deflate"))

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	})

	t.Run("an error reply is gzipped too", func(t *testing.T) {
		w := a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, "never-said-hello"), withHeader("Accept-Encoding", "gzip"))

		require.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
		assert.Contains(t, string(gunzipBytes(t, w.Body.Bytes())), "send hello first")
	})

	t.Run("a reply with no body isn't", func(t *testing.T) {
		w := a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, instanceID), withHeader("Accept-Encoding", "gzip"))

		require.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.Empty(t, w.Body.Bytes())
	})

	t.Run("a broken gzip stream is a bad request", func(t *testing.T) {
		id := uuid.NewString()
		w := a.request(t, http.MethodPost, helloPath, "this is not gzip", asProbe(made.Token, id), withHeader("Content-Encoding", "gzip"))

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, errorMessage(t, w.Body.Bytes()), "gzip")
	})

	t.Run("a stream that breaks after its header is a bad request", func(t *testing.T) {
		id := uuid.NewString()
		compressed := gzipBytes(t, bytes.Repeat(body, 200))
		w := a.request(t, http.MethodPost, helloPath, string(compressed[:len(compressed)/2]), asProbe(made.Token, id), withHeader("Content-Encoding", "gzip"))

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
}

func TestProbeProtocol_TooManyRequestsAnswer429(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()

	for n := 1; n <= 5; n++ {
		w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(made.Token, instanceID))
		require.Equal(t, http.StatusOK, w.Code, "hello %d is within the burst of 5: %s", n, w.Body.String())
	}
	w := a.request(t, http.MethodPost, helloPath, helloFrom(instanceID), asProbe(made.Token, instanceID))

	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.Equal(t, "too many requests", errorMessage(t, w.Body.Bytes()))

	other := uuid.NewString()
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, helloFrom(other), asProbe(made.Token, other)).Code,
		"another instance has a limit of its own")
	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, instanceID)).Code,
		"and so has each route")
}

func TestProbeProtocol_LimitsOnlyAuthenticatedRequests(t *testing.T) {
	a, _ := probeApp(t)

	for range 20 {
		w := a.request(t, http.MethodPost, helloPath, helloFrom("x"), asProbe("pkp_not-a-token", "x"))

		require.Equal(t, http.StatusUnauthorized, w.Code, "a request that fails to authenticate never meets the rate limit")
	}
}

func TestProbeProtocol_BodyOver1MBAnswers413(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	hello := helloFrom(instanceID)
	hello.Hostname = strings.Repeat("h", 1<<20+1)
	body, err := json.Marshal(hello)
	require.NoError(t, err)

	t.Run("a gzipped body that inflates past 1 MB", func(t *testing.T) {
		compressed := gzipBytes(t, body)
		require.Less(t, len(compressed), 100<<10, "it is small on the wire, as a zip bomb is")

		w := a.request(t, http.MethodPost, helloPath, string(compressed), asProbe(made.Token, instanceID), withHeader("Content-Encoding", "gzip"))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
		assert.Equal(t, "request body too large", errorMessage(t, w.Body.Bytes()))
	})

	t.Run("a plain body over 1 MB", func(t *testing.T) {
		w := a.request(t, http.MethodPost, helloPath, string(body), asProbe(made.Token, instanceID))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
		assert.Equal(t, "request body too large", errorMessage(t, w.Body.Bytes()))
	})

	t.Run("padding after a complete message counts too", func(t *testing.T) {
		valid, err := json.Marshal(helloFrom(instanceID))
		require.NoError(t, err)
		padded := append(valid, bytes.Repeat([]byte(" "), 1<<20)...)

		w := a.request(t, http.MethodPost, helloPath, string(gzipBytes(t, padded)), asProbe(made.Token, instanceID), withHeader("Content-Encoding", "gzip"))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	})
	assert.Empty(t, a.getProbe(t, made.ID).Instances)
}

// A MongoDB install has no probes (ADR 0001): its probe routes answer 501
func TestProbeProtocol_MongoInstallAnswers501(t *testing.T) {
	a := newAppDecorated(t, func(c *dig.Container) error {
		return c.Decorate(func() probe.Repository { return probe.NewUnsupportedRepository() })
	})
	a.registerAdmin(t)
	instanceID := uuid.NewString()

	for _, path := range []string{helloPath, byePath} {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodPost, path, helloFrom(instanceID), asProbe("pkp_any-token-at-all", instanceID))

			assert.Equal(t, http.StatusNotImplemented, w.Code, w.Body.String())
			assert.Equal(t, "probes need SQLite or PostgreSQL storage", errorMessage(t, w.Body.Bytes()))
		})
	}
}
