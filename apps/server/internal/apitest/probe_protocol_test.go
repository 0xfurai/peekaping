package apitest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
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

// The stamp is made by the middleware every route after hello has. Bye retires the instance it
// stamped, so the test calls a route that leaves the instance be.
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

	w := a.assignments(t, made.Token, instanceID, "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
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

// queuedNotificationTopics are the topics of the notification events waiting for an api replica to
// send them, oldest first. Cluster mode sends a probe's offline and online events this way, so
// that one replica sends each. No replica takes them in these tests.
func (a *app) queuedNotificationTopics(t *testing.T) []string {
	t.Helper()
	topics := []string{}
	require.NoError(t, a.container.Invoke(func(inspector *asynq.Inspector) error {
		tasks, err := inspector.ListPendingTasks(notification_channel.QueueName)
		if errors.Is(err, asynq.ErrQueueNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, task := range tasks {
			var queued struct {
				Topic string `json:"topic"`
			}
			if err := json.Unmarshal(task.Payload, &queued); err != nil {
				return err
			}
			topics = append(topics, queued.Topic)
		}
		return nil
	}))
	return topics
}

func (a *app) clearQueuedNotifications(t *testing.T) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(inspector *asynq.Inspector) error {
		_, err := inspector.DeleteAllPendingTasks(notification_channel.QueueName)
		if errors.Is(err, asynq.ErrQueueNotFound) {
			return nil
		}
		return err
	}))
}

func TestProbeProtocol_RequestBringsAnOfflineProbeOnline(t *testing.T) {
	// An offline probe whose instance said hello before: what the sweep leaves behind when the
	// instance goes quiet
	offlineProbe := func(t *testing.T) (a *app, made probeWithToken, instanceID string) {
		a, made = probeApp(t)
		instanceID = uuid.NewString()
		a.sayHello(t, made.Token, instanceID)
		require.Equal(t, "online", a.getProbe(t, made.ID).Status, "the first hello brings the probe online")
		require.NoError(t, a.container.Invoke(func(probes probe.Service) error {
			changed, err := probes.MarkOffline(context.Background(), made.ID)
			require.True(t, changed)
			return err
		}))
		require.Equal(t, "offline", a.getProbe(t, made.ID).Status)
		a.clearQueuedNotifications(t)
		return a, made, instanceID
	}

	t.Run("a hello", func(t *testing.T) {
		a, made, instanceID := offlineProbe(t)

		a.sayHello(t, made.Token, instanceID)

		assert.Equal(t, "online", a.getProbe(t, made.ID).Status)
		assert.Equal(t, []string{"probe.online"}, a.queuedNotificationTopics(t), "announced once, through the notification queue")
	})

	t.Run("a request from an instance that said hello", func(t *testing.T) {
		a, made, instanceID := offlineProbe(t)

		w := a.assignments(t, made.Token, instanceID, "")

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "online", a.getProbe(t, made.ID).Status, "at once, not at the next sweep")
		assert.Equal(t, []string{"probe.online"}, a.queuedNotificationTopics(t))

		a.assignments(t, made.Token, instanceID, "")
		a.sayHello(t, made.Token, instanceID)
		assert.Equal(t, []string{"probe.online"}, a.queuedNotificationTopics(t), "a probe that is online says nothing more")
	})

	t.Run("a request from an instance that never said hello", func(t *testing.T) {
		a, made, _ := offlineProbe(t)

		w := a.assignments(t, made.Token, uuid.NewString(), "")

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, "offline", a.getProbe(t, made.ID).Status, "none of its instances is connected")
		assert.Empty(t, a.queuedNotificationTopics(t))
	})
}

// racingAdmin is the probe service with an admin who disables a probe in the moment between a
// request's authentication, which found the probe enabled, and the status change the request causes
type racingAdmin struct {
	probe.Service
	disableOnNextTouch atomic.Bool
}

func (s *racingAdmin) TouchInstance(ctx context.Context, probeID string, instanceID string, at time.Time) (bool, error) {
	found, err := s.Service.TouchInstance(ctx, probeID, instanceID, at)
	if err != nil || !s.disableOnNextTouch.CompareAndSwap(true, false) {
		return found, err
	}
	disabled := true
	_, err = s.Service.Update(ctx, probeID, &probe.UpdateRequest{Disabled: &disabled})
	return found, err
}

func TestProbeProtocol_ADisabledProbeIsNotBroughtOnline(t *testing.T) {
	racing := &racingAdmin{}
	a := newAppDecorated(t, func(c *dig.Container) error {
		return c.Decorate(func(probes probe.Service) probe.Service {
			racing.Service = probes
			return racing
		})
	})
	a.registerAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "eu-central"})
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)
	require.NoError(t, a.container.Invoke(func(probes probe.Service) error {
		changed, err := probes.MarkOffline(context.Background(), made.ID)
		require.True(t, changed)
		return err
	}))
	a.clearQueuedNotifications(t)
	racing.disableOnNextTouch.Store(true)

	// The assignments handler reads the probe again and answers 401 for a disabled one. The status
	// of the answer is not the point: what the request leaves behind is.
	a.assignments(t, made.Token, instanceID, "")

	got := a.getProbe(t, made.ID)
	require.True(t, got.Disabled, "the admin disabled the probe during the request")
	assert.Equal(t, "offline", got.Status, "a disabled probe is not set online by a request that began before it was disabled")
	assert.Empty(t, a.queuedNotificationTopics(t), "and nobody is told that it is back")
}

// Bye takes the instance out of the dealing at once and leaves the row, so that the probe is not
// without instances: it counts as offline only after PROBE_OFFLINE_AFTER, which a graceful restart
// stays well inside of.
func TestProbeProtocol_ByeRetiresTheInstance(t *testing.T) {
	a, made := probeApp(t)
	staying, leaving := uuid.NewString(), uuid.NewString()
	a.sayHello(t, made.Token, staying)
	a.sayHello(t, made.Token, leaving)
	require.Len(t, a.getProbe(t, made.ID).Instances, 2)

	w := a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, leaving))

	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Empty(t, w.Body.String())
	lastSeen := map[string]time.Time{}
	for _, instance := range a.getProbe(t, made.ID).Instances {
		lastSeen[instance.ID] = instance.LastSeenAt
	}
	require.Len(t, lastSeen, 2, "the retired instance is still listed")
	assert.WithinDuration(t, time.Now().Add(-probe.InstanceOnlineWindow), lastSeen[leaving], 5*time.Second,
		"last seen is the edge of the online window, which is outside it")
	assert.WithinDuration(t, time.Now(), lastSeen[staying], 5*time.Second, "and the sibling's is untouched")
	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(made.Token, leaving)).Code,
		"a bye that is sent again, as after a lost answer, retires the instance again")
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

func TestProbeProtocol_HelloFromManyInstancesAnswers429(t *testing.T) {
	a, made := probeApp(t)

	// Every hello comes from an instance id the probe has not used, so no instance's own limit is met
	for n := 1; n <= 10; n++ {
		id := uuid.NewString()
		w := a.request(t, http.MethodPost, helloPath, helloFrom(id), asProbe(made.Token, id))
		require.Equal(t, http.StatusOK, w.Code, "hello %d is within the probe's burst of 10: %s", n, w.Body.String())
	}
	id := uuid.NewString()
	w := a.request(t, http.MethodPost, helloPath, helloFrom(id), asProbe(made.Token, id))

	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
	assert.Equal(t, "too many requests", errorMessage(t, w.Body.Bytes()))
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

const assignmentsPath = "/probe/v1/assignments"

// localToken is the bootstrap token of the local probe in the tests that need it: the one probe
// that is dealt monitors until #34
const localToken = "bootstrap-token-0123456789-abcdefghijklmnop"

// shortPoll shortens the time a poll waits to 200 ms, so that a test of the timeout is quick
func shortPoll(c *dig.Container) error {
	return c.Decorate(func() protocol.Timing { return protocol.Timing{PollTimeout: 200 * time.Millisecond} })
}

// localProbeApp is an app whose local probe has a token
func localProbeApp(t *testing.T, decorate func(*dig.Container) error) *app {
	t.Helper()
	a := newAppDecorated(t, decorate, func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })
	a.registerAdmin(t)
	return a
}

func httpMonitorBody(name, proxyID string) map[string]any {
	return map[string]any{
		"type": "http", "name": name, "interval": 60, "retry_interval": 60, "timeout": 16, "max_retries": 3, "resend_interval": 10,
		"active": true, "notification_ids": []string{}, "proxy_id": proxyID,
		"config": `{"url":"https://example.com/health","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`,
	}
}

func (a *app) createHTTPMonitor(t *testing.T, name, proxyID string) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", httpMonitorBody(name, proxyID), a.asAdmin), http.StatusCreated).ID
}

// assignments asks for the instance's monitors, saying which version it holds
func (a *app) assignments(t *testing.T, token, instanceID, version string) *httptest.ResponseRecorder {
	t.Helper()
	return a.pollAssignments(token, instanceID, version)
}

// pollAssignments is assignments for a goroutine of a test, which must not fail the test itself
func (a *app) pollAssignments(token, instanceID, version string, creds ...credentials) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, assignmentsPath+"?version="+url.QueryEscape(version), nil)
	asProbe(token, instanceID)(req)
	for _, apply := range creds {
		apply(req)
	}
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, req)
	return w
}

// startPoll has the instance poll in the background, and returns what the server answers
func (a *app) startPoll(token, instanceID, version string) <-chan *httptest.ResponseRecorder {
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() { answered <- a.pollAssignments(token, instanceID, version) }()
	return answered
}

func decodeAssignments(t *testing.T, w *httptest.ResponseRecorder) probeproto.AssignmentsResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got probeproto.AssignmentsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	return got
}

// stillWaiting reports whether a poll is going on after a while, which is how the tests know that
// it has registered with the waker before they change something
func stillWaiting(t *testing.T, answered <-chan *httptest.ResponseRecorder) {
	t.Helper()
	select {
	case w := <-answered:
		t.Fatalf("the poll answered %d before anything changed: %s", w.Code, w.Body.String())
	case <-time.After(300 * time.Millisecond):
	}
}

// answeredWithin returns the poll's answer, or fails the test if it doesn't come in time
func answeredWithin(t *testing.T, answered <-chan *httptest.ResponseRecorder, limit time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-answered:
		return w
	case <-time.After(limit):
		t.Fatalf("the poll was not answered within %s", limit)
		return nil
	}
}

func monitorIDs(monitors []probeproto.Monitor) []string {
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

func TestProbeProtocol_AssignmentsCarryMonitorAndProxy(t *testing.T) {
	a := localProbeApp(t, nil)
	proxyID := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", map[string]any{
		"protocol": "socks5", "host": "egress.example.com", "port": 1080, "auth": true, "username": "scout", "password": "s3cret",
	}, a.asAdmin), http.StatusCreated).ID
	httpID := a.createHTTPMonitor(t, "Checkout", proxyID)
	a.createMonitor(t, "Nightly cron", "nightly-cron-push-token")
	retiredID := a.createHTTPMonitor(t, "Retired", "")
	a.pauseMonitor(t, retiredID)
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)

	w := a.assignments(t, localToken, instanceID, "")

	got := decodeAssignments(t, w)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.NotEmpty(t, got.Version)
	require.Len(t, got.Monitors, 1, "the http monitor only: a push monitor runs no checks, and a paused one is not running")
	m := got.Monitors[0]
	assert.Equal(t, httpID, m.ID)
	assert.Equal(t, "Checkout", m.Name)
	assert.Equal(t, "http", m.Type)
	assert.Equal(t, 60, m.Interval)
	assert.Equal(t, 16, m.Timeout)
	assert.Equal(t, 3, m.MaxRetries)
	assert.Equal(t, 60, m.RetryInterval)
	assert.Equal(t, 10, m.ResendInterval)
	assert.JSONEq(t, httpMonitorBody("", "")["config"].(string), m.Config)
	assert.WithinDuration(t, time.Now(), m.UpdatedAt, time.Minute)
	require.NotNil(t, m.Proxy)
	assert.Equal(t, proxyID, m.Proxy.ID)
	assert.Equal(t, "socks5", m.Proxy.Protocol)
	assert.Equal(t, "egress.example.com", m.Proxy.Host)
	assert.Equal(t, 1080, m.Proxy.Port)
	assert.True(t, m.Proxy.Auth)
	assert.Equal(t, "scout", m.Proxy.Username)
	assert.Equal(t, "s3cret", m.Proxy.Password, "the probe needs the password to use the proxy")
	assert.WithinDuration(t, time.Now(), m.Proxy.UpdatedAt, time.Minute)
	assert.Equal(t, time.UTC, m.UpdatedAt.Location())
}

func TestProbeProtocol_AssignmentsOmitTheProxyOfAMonitorWithout(t *testing.T) {
	a := localProbeApp(t, nil)
	a.createHTTPMonitor(t, "Direct", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)

	w := a.assignments(t, localToken, instanceID, "")

	got := decodeAssignments(t, w)
	require.Len(t, got.Monitors, 1)
	assert.Nil(t, got.Monitors[0].Proxy)
	assert.NotContains(t, w.Body.String(), `"proxy"`)
}

func TestProbeProtocol_AssignmentsSkipTheTypesAProbeCannotRun(t *testing.T) {
	a := localProbeApp(t, nil)
	httpID := a.createHTTPMonitor(t, "Checkout", "")
	tcp := httpMonitorBody("Database", "")
	tcp["type"], tcp["config"] = "tcp", `{"host":"db.example.com","port":5432}`
	decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", tcp, a.asAdmin), http.StatusCreated)
	instanceID := uuid.NewString()
	hello := helloFrom(instanceID)
	hello.SupportedTypes = []string{"http"}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, hello, asProbe(localToken, instanceID)).Code)

	got := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))

	assert.Equal(t, []string{httpID}, monitorIDs(got.Monitors))
}

func TestProbeProtocol_RemoteProbeGetsNothingUntil34(t *testing.T) {
	a, made := probeApp(t)
	a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)

	w := a.assignments(t, made.Token, instanceID, "")

	got := decodeAssignments(t, w)
	assert.Empty(t, got.Monitors)
	assert.Contains(t, w.Body.String(), `"monitors":[]`, "an empty list, not null")
	assert.NotEmpty(t, got.Version)
	assert.Equal(t, got.Version, decodeAssignments(t, a.assignments(t, made.Token, instanceID, "")).Version, "the version is the same until something changes")
	a.createHTTPMonitor(t, "Another", "")
	assert.Equal(t, got.Version, decodeAssignments(t, a.assignments(t, made.Token, instanceID, "")).Version, "and a monitor that is not the probe's is nothing to it")
}

func TestProbeProtocol_AssignmentsNeedTheProbesToken(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)

	for name, creds := range map[string][]credentials{
		"no credentials":   nil,
		"the admin's JWT":  {asProbe(a.accessToken, instanceID)},
		"an API key":       {asProbe(a.apiKey, instanceID)},
		"an unknown token": {asProbe("pkp_no-probe-has-it", instanceID)},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, assignmentsPath, nil)
			for _, apply := range creds {
				apply(req)
			}
			w := httptest.NewRecorder()
			a.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
	t.Run("an instance that never said hello", func(t *testing.T) {
		w := a.assignments(t, made.Token, uuid.NewString(), "")

		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})
}

func TestProbeProtocol_SameVersionWaitsThen304(t *testing.T) {
	a := newAppDecorated(t, shortPoll)
	a.registerAdmin(t)
	made := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	instanceID := uuid.NewString()
	welcome := a.sayHello(t, made.Token, instanceID)
	assert.Equal(t, 200, welcome.PollTimeoutMs, "hello tells the probe how long the server holds a poll")
	held := decodeAssignments(t, a.assignments(t, made.Token, instanceID, "")).Version

	t.Run("answers 304 with no body once the poll times out", func(t *testing.T) {
		started := time.Now()

		w := a.assignments(t, made.Token, instanceID, held)

		assert.Equal(t, http.StatusNotModified, w.Code)
		assert.Empty(t, w.Body.Bytes())
		assert.Empty(t, w.Header().Get("Content-Type"))
		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.GreaterOrEqual(t, time.Since(started), 150*time.Millisecond, "it waited for a change first")
		assert.Less(t, time.Since(started), 5*time.Second)
	})

	t.Run("a reply that could be gzipped is not, when it has no body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, assignmentsPath+"?version="+url.QueryEscape(held), nil)
		asProbe(made.Token, instanceID)(req)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		a.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotModified, w.Code)
		assert.Empty(t, w.Body.Bytes(), "not even the trailer of an empty gzip stream")
		assert.Empty(t, w.Header().Get("Content-Encoding"))
	})

	t.Run("a different version is answered at once", func(t *testing.T) {
		w := a.assignments(t, made.Token, instanceID, "a-version-the-server-never-made")

		got := decodeAssignments(t, w)
		assert.Equal(t, held, got.Version)
	})

	t.Run("no version is answered at once", func(t *testing.T) {
		assert.Equal(t, held, decodeAssignments(t, a.assignments(t, made.Token, instanceID, "")).Version)
	})
}

func TestProbeProtocol_LongPollWakesOnMonitorUpdate(t *testing.T) {
	a := localProbeApp(t, nil)
	id := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	first := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))
	answered := a.startPoll(localToken, instanceID, first.Version)
	stillWaiting(t, answered)

	changed := time.Now()
	w := a.request(t, http.MethodPut, "/api/v1/monitors/"+id, httpMonitorBody("Checkout, renamed", ""), a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
	assert.Less(t, time.Since(changed), 2*time.Second, "it answered on the update, not at the 25 s timeout")
	assert.NotEqual(t, first.Version, got.Version)
	require.Len(t, got.Monitors, 1)
	assert.Equal(t, "Checkout, renamed", got.Monitors[0].Name)
}

func TestProbeProtocol_LongPollWakesOnMonitorCreateAndDelete(t *testing.T) {
	a := localProbeApp(t, nil)
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	version := decodeAssignments(t, a.assignments(t, localToken, instanceID, "")).Version

	var id string
	for _, step := range []struct {
		name   string
		change func()
		want   int
	}{
		{"a monitor is created", func() { id = a.createHTTPMonitor(t, "Checkout", "") }, 1},
		{"a monitor is deleted", func() {
			decode[any](t, a.request(t, http.MethodDelete, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)
		}, 0},
	} {
		answered := a.startPoll(localToken, instanceID, version)
		stillWaiting(t, answered)

		step.change()

		got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
		assert.NotEqual(t, version, got.Version, step.name)
		assert.Len(t, got.Monitors, step.want, step.name)
		version = got.Version
	}
}

// A monitor's row is touched when a heartbeat changes its status (the listener of cmd/api stores
// it), but the probe is told nothing of the status. A version that moved with it would make the
// probe fetch its monitors again, and restart their timers, after every Up and Down.
func TestProbeProtocol_StatusFlipKeepsTheVersion(t *testing.T) {
	a := localProbeApp(t, shortPoll)
	require.NoError(t, a.container.Invoke(func(listener *monitor.MonitorEventListener, bus events.EventBus) {
		listener.Subscribe(bus)
	}))
	id := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	held := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))
	require.Len(t, held.Monitors, 1)

	a.reportStatus(t, id, shared.MonitorStatusDown)

	w := a.assignments(t, localToken, instanceID, held.Version)
	assert.Equal(t, http.StatusNotModified, w.Code, "the version is the one the probe holds: %s", w.Body.String())
	again := decodeAssignments(t, a.assignments(t, localToken, instanceID, ""))
	assert.Equal(t, held.Version, again.Version)
	require.Len(t, again.Monitors, 1)
	assert.True(t, again.Monitors[0].UpdatedAt.After(held.Monitors[0].UpdatedAt), "the row was touched by the status change, as the wire's updated_at shows")
}

// The retired instance's row stays, and still the monitors are dealt among the sibling alone
func TestProbeProtocol_ByeRetiresTheInstanceAndRedeals(t *testing.T) {
	a := localProbeApp(t, nil)
	const count = 24
	all := make([]string, count)
	for i := range all {
		all[i] = a.createHTTPMonitor(t, fmt.Sprintf("Monitor %02d", i), "")
	}
	staying, leaving := uuid.NewString(), uuid.NewString()
	a.sayHello(t, localToken, staying)
	a.sayHello(t, localToken, leaving)

	stayingShare := decodeAssignments(t, a.assignments(t, localToken, staying, ""))
	leavingShare := decodeAssignments(t, a.assignments(t, localToken, leaving, ""))

	require.NotEmpty(t, leavingShare.Monitors, "the second instance has monitors to lose")
	require.NotEmpty(t, stayingShare.Monitors)
	assert.Equal(t, stayingShare.Version, leavingShare.Version, "the instances of a probe hold one version")
	assert.ElementsMatch(t, all, append(monitorIDs(stayingShare.Monitors), monitorIDs(leavingShare.Monitors)...), "each monitor is on exactly one of them")

	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(localToken, leaving)).Code)

	after := decodeAssignments(t, a.assignments(t, localToken, staying, stayingShare.Version))
	assert.NotEqual(t, stayingShare.Version, after.Version)
	assert.ElementsMatch(t, all, monitorIDs(after.Monitors), "the instance that stayed has everything now")
	assert.Subset(t, monitorIDs(after.Monitors), monitorIDs(stayingShare.Monitors))
}

func TestProbeProtocol_ByeAnswersTheWaitingPollsOfItsSiblings(t *testing.T) {
	a := localProbeApp(t, nil)
	const count = 24
	for i := range count {
		a.createHTTPMonitor(t, fmt.Sprintf("Monitor %02d", i), "")
	}
	staying, leaving := uuid.NewString(), uuid.NewString()
	a.sayHello(t, localToken, staying)
	a.sayHello(t, localToken, leaving)
	share := decodeAssignments(t, a.assignments(t, localToken, staying, ""))
	require.Less(t, len(share.Monitors), count)
	answered := a.startPoll(localToken, staying, share.Version)
	stillWaiting(t, answered)

	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, byePath, nil, asProbe(localToken, leaving)).Code)

	got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
	assert.NotEqual(t, share.Version, got.Version)
	assert.Len(t, got.Monitors, count)
}

func TestProbeProtocol_HelloWakesTheSiblingsPollsWhenSomethingChanged(t *testing.T) {
	a := localProbeApp(t, nil)
	const count = 24
	for i := range count {
		a.createHTTPMonitor(t, fmt.Sprintf("Monitor %02d", i), "")
	}
	first, second := uuid.NewString(), uuid.NewString()
	a.sayHello(t, localToken, first)
	alone := decodeAssignments(t, a.assignments(t, localToken, first, ""))
	require.Len(t, alone.Monitors, count)

	t.Run("an instance joins", func(t *testing.T) {
		answered := a.startPoll(localToken, first, alone.Version)
		stillWaiting(t, answered)

		a.sayHello(t, localToken, second)

		got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
		assert.NotEqual(t, alone.Version, got.Version)
		assert.Less(t, len(got.Monitors), count, "it gave the newcomer a share")
		alone = got
	})

	// The poll of the next two cases: the hello of an instance that is already online does not end
	// it, the hello with other monitor types does
	answered := a.startPoll(localToken, first, alone.Version)
	t.Run("an instance that is already online says hello again", func(t *testing.T) {
		stillWaiting(t, answered)

		a.sayHello(t, localToken, second)

		stillWaiting(t, answered)
	})

	t.Run("an instance says hello with other monitor types", func(t *testing.T) {
		hello := helloFrom(second)
		hello.SupportedTypes = []string{"tcp"}

		require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, hello, asProbe(localToken, second)).Code)

		got := decodeAssignments(t, answeredWithin(t, answered, 2*time.Second))
		assert.NotEqual(t, alone.Version, got.Version)
		assert.Empty(t, got.Monitors, "none of the monitors are of a type the probe runs now")
	})
}

func TestProbeProtocol_ShutdownAnswersTheWaitingPolls(t *testing.T) {
	a := localProbeApp(t, nil)
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	version := decodeAssignments(t, a.assignments(t, localToken, instanceID, "")).Version
	answered := a.startPoll(localToken, instanceID, version)
	stillWaiting(t, answered)

	require.NoError(t, a.container.Invoke(func(waker *protocol.Waker) { waker.Close() }))

	w := answeredWithin(t, answered, 2*time.Second)
	assert.Equal(t, http.StatusNotModified, w.Code, "a server that is stopping does not hold its probes for 25 s")
	assert.Equal(t, http.StatusNotModified, a.assignments(t, localToken, instanceID, version).Code, "nor does it start another wait")
}

func TestProbeProtocol_AssignmentsAreDocumented(t *testing.T) {
	a := newApp(t)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Parameters  []struct {
				Name     string `json:"name"`
				In       string `json:"in"`
				Required bool   `json:"required"`
			} `json:"parameters"`
			Security  []map[string][]string `json:"security"`
			Responses map[string]struct {
				Content map[string]json.RawMessage `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(a.request(t, http.MethodGet, "/openapi.json", nil).Body.Bytes(), &spec))

	op, ok := spec.Paths[assignmentsPath]["get"]

	require.True(t, ok, "GET %s is in the document", assignmentsPath)
	assert.Equal(t, "probeGetAssignments", op.OperationID)
	assert.Equal(t, []map[string][]string{{"ProbeAuth": {}}}, op.Security)
	type parameter struct {
		In       string
		Required bool
	}
	parameters := map[string]parameter{}
	for _, p := range op.Parameters {
		parameters[p.Name] = parameter{p.In, p.Required}
	}
	assert.Equal(t, parameter{"query", false}, parameters["version"])
	assert.Equal(t, parameter{"header", true}, parameters[probeproto.HeaderInstanceID])
	assert.Contains(t, op.Responses["200"].Content, "application/json")
	assert.Contains(t, op.Responses, "304", "the poll that times out")
	assert.Empty(t, op.Responses["304"].Content, "and has no body")
	for _, status := range []string{"400", "401", "409", "429", "500", "501"} {
		assert.Contains(t, op.Responses, status)
	}
}

const resultsPath = "/probe/v1/results"

// checkedAt is when a check of a test's started: a minute ago, plus the seconds that tell the
// checks of one test apart
func checkedAt(seconds int) time.Time {
	return time.Now().UTC().Add(-time.Minute + time.Duration(seconds)*time.Second).Truncate(time.Millisecond)
}

// checkResult is an Up result of the monitor that started at start
func checkResult(id, monitorID string, start time.Time) probeproto.Result {
	return probeproto.Result{
		ID: id, MonitorID: monitorID, Status: probeproto.StatusUp, Message: "200 - OK", PingMs: 12,
		StartTime: start, EndTime: start.Add(12 * time.Millisecond),
	}
}

// sendResults sends the results as the instance and returns what the server answered
func (a *app) sendResults(t *testing.T, token, instanceID string, results ...probeproto.Result) *httptest.ResponseRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, resultsPath, probeproto.ResultsRequest{Results: results}, asProbe(token, instanceID))
}

// outcomesOf sends the results and returns the outcomes, which the server must have answered 200
func (a *app) outcomesOf(t *testing.T, token, instanceID string, results ...probeproto.Result) []probeproto.ResultOutcome {
	t.Helper()
	w := a.sendResults(t, token, instanceID, results...)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var reply probeproto.ResultsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply), w.Body.String())
	return reply.Results
}

// queuedPayloads are the payloads of the heartbeats waiting for the ingester, oldest check first
func (a *app) queuedPayloads(t *testing.T) []map[string]any {
	t.Helper()
	var payloads []map[string]any
	for _, task := range a.queuedHeartbeats(t) {
		var payload map[string]any
		require.NoError(t, json.Unmarshal(task.Payload, &payload))
		payloads = append(payloads, payload)
	}
	slices.SortFunc(payloads, func(x, y map[string]any) int {
		return strings.Compare(x["start_time"].(string), y["start_time"].(string))
	})
	return payloads
}

func TestProbeProtocol_ResultsAreQueuedForTheIngester(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	up := checkResult("result-up", monitorID, checkedAt(0))
	down := checkResult("result-down", monitorID, checkedAt(30))
	down.Status, down.Message, down.PingMs = probeproto.StatusDown, "connection refused", 843
	down.EndTime = down.StartTime.Add(843 * time.Millisecond)

	got := a.outcomesOf(t, localToken, instanceID, down, up)

	assert.Equal(t, []probeproto.ResultOutcome{{ID: "result-down", Accepted: true}, {ID: "result-up", Accepted: true}}, got,
		"one outcome for each result, in the order sent")
	tasks := a.queuedHeartbeats(t)
	require.Len(t, tasks, 2, "the reply comes after the enqueue")
	for _, task := range tasks {
		assert.Equal(t, "monitor:ingest", task.Type)
		assert.Equal(t, "ingester", task.Queue)
	}
	payloads := a.queuedPayloads(t)
	require.Len(t, payloads, 2)
	for i, want := range []probeproto.Result{up, down} {
		payload := payloads[i]
		assert.Equal(t, "local", payload["probe_id"], "the probe that ran the check")
		assert.Equal(t, monitorID, payload["monitor_id"])
		assert.Equal(t, "Checkout", payload["monitor_name"], "the monitor's settings are filled in as for a push")
		assert.Equal(t, "http", payload["monitor_type"])
		assert.EqualValues(t, 60, payload["monitor_interval"])
		assert.EqualValues(t, 3, payload["monitor_max_retries"])
		assert.Equal(t, false, payload["is_under_maintenance"])
		assert.Equal(t, want.Message, payload["message"])
		assert.EqualValues(t, want.PingMs, payload["ping_ms"])
		startedAt, err := time.Parse(time.RFC3339Nano, payload["start_time"].(string))
		require.NoError(t, err)
		assert.True(t, want.StartTime.Equal(startedAt), "the result's own time, not the time it arrived: %s", payload["start_time"])
		endedAt, err := time.Parse(time.RFC3339Nano, payload["end_time"].(string))
		require.NoError(t, err)
		assert.True(t, want.EndTime.Equal(endedAt))
	}
	assert.EqualValues(t, 1, payloads[0]["status"], "Up")
	assert.EqualValues(t, 0, payloads[1]["status"], "Down")
}

func TestProbeProtocol_ResultsRejectReasonsEndToEnd(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	pushID := a.createMonitor(t, "Nightly cron", "nightly-cron-push-token")
	pausedID := a.createHTTPMonitor(t, "Retired", "")
	a.pauseMonitor(t, pausedID)
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	good := checkResult("good", monitorID, checkedAt(0))
	old := checkResult("old", monitorID, time.Now().UTC().Add(-2*time.Hour))
	ahead := checkResult("ahead", monitorID, time.Now().UTC().Add(5*time.Minute))
	batch := []probeproto.Result{
		good,
		checkResult("unknown", uuid.NewString(), checkedAt(1)),
		checkResult("not-a-uuid", "no-such-monitor", checkedAt(1)),
		checkResult("push", pushID, checkedAt(2)),
		checkResult("paused", pausedID, checkedAt(3)),
		old,
		ahead,
	}

	got := a.outcomesOf(t, localToken, instanceID, batch...)

	assert.Equal(t, []probeproto.ResultOutcome{
		{ID: "good", Accepted: true},
		{ID: "unknown", Reason: "unknown_monitor"},
		{ID: "not-a-uuid", Reason: "unknown_monitor"},
		{ID: "push", Reason: "not_assigned"},
		{ID: "paused", Reason: "not_assigned"},
		{ID: "old", Reason: "too_old"},
		{ID: "ahead", Reason: "invalid"},
	}, got)
	require.Len(t, a.queuedHeartbeats(t), 1, "only the good result is stored")

	again := a.outcomesOf(t, localToken, instanceID, good, checkResult("next", monitorID, checkedAt(10)))

	assert.Equal(t, []probeproto.ResultOutcome{{ID: "good", Reason: "duplicate"}, {ID: "next", Accepted: true}}, again,
		"a result the server stored is a duplicate when the probe sends it again")
	assert.Len(t, a.queuedHeartbeats(t), 2)
}

// Cluster mode's replicas each remember the ids they stored. A result that another replica took
// is not among them, so the queue is what catches it: it holds a task for the monitor and the
// start time. The test sends a second id for a check that started at the same time, which the
// first replica's memory doesn't know either.
func TestProbeProtocol_AResultTheQueueHoldsAlreadyIsADuplicate(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	started := checkedAt(0)
	require.Equal(t, []probeproto.ResultOutcome{{ID: "first-try", Accepted: true}},
		a.outcomesOf(t, localToken, instanceID, checkResult("first-try", monitorID, started)))

	got := a.outcomesOf(t, localToken, instanceID, checkResult("another-id", monitorID, started))

	assert.Equal(t, []probeproto.ResultOutcome{{ID: "another-id", Reason: "duplicate"}}, got, "and the probe is not told to try again")
	assert.Len(t, a.queuedHeartbeats(t), 1, "the heartbeat is queued once")
}

func TestProbeProtocol_ResultsOver1MBAnswer413(t *testing.T) {
	a, made := probeApp(t)
	instanceID := uuid.NewString()
	a.sayHello(t, made.Token, instanceID)
	big := checkResult("big", "any-monitor", checkedAt(0))
	big.Message = strings.Repeat("m", 1<<20+1)
	body, err := json.Marshal(probeproto.ResultsRequest{Results: []probeproto.Result{big}})
	require.NoError(t, err)

	t.Run("a plain body over 1 MB", func(t *testing.T) {
		w := a.request(t, http.MethodPost, resultsPath, string(body), asProbe(made.Token, instanceID))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
		assert.Equal(t, "request body too large", errorMessage(t, w.Body.Bytes()))
	})

	t.Run("a gzipped body that inflates past 1 MB", func(t *testing.T) {
		compressed := gzipBytes(t, body)
		require.Less(t, len(compressed), 100<<10, "it is small on the wire")

		w := a.request(t, http.MethodPost, resultsPath, string(compressed), asProbe(made.Token, instanceID), withHeader("Content-Encoding", "gzip"))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
		assert.Equal(t, "request body too large", errorMessage(t, w.Body.Bytes()))
	})
	assert.Empty(t, a.queuedHeartbeats(t))
}

func TestProbeProtocol_ResultsBatchOver500Answers400(t *testing.T) {
	a := localProbeApp(t, nil)
	monitorID := a.createHTTPMonitor(t, "Checkout", "")
	instanceID := uuid.NewString()
	a.sayHello(t, localToken, instanceID)
	batch := make([]probeproto.Result, 501)
	for i := range batch {
		batch[i] = checkResult(fmt.Sprintf("result-%d", i), monitorID, checkedAt(0).Add(time.Duration(i)*time.Millisecond))
	}

	w := a.sendResults(t, localToken, instanceID, batch...)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "batch holds 501 results, the limit is 500", errorMessage(t, w.Body.Bytes()))
	assert.Empty(t, a.queuedHeartbeats(t), "none of it is stored")
	assert.Len(t, a.outcomesOf(t, localToken, instanceID, batch[:500]...), 500, "the batch the server hands out in hello is the most it takes")
}

func TestProbeProtocol_ResultsAreDocumented(t *testing.T) {
	a := newApp(t)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `json:"operationId"`
			Security    []map[string][]string `json:"security"`
			Responses   map[string]struct {
				Content map[string]json.RawMessage `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(a.request(t, http.MethodGet, "/openapi.json", nil).Body.Bytes(), &spec))

	op, ok := spec.Paths[resultsPath]["post"]

	require.True(t, ok, "POST %s is in the document", resultsPath)
	assert.Equal(t, "probePostResults", op.OperationID)
	assert.Equal(t, []map[string][]string{{"ProbeAuth": {}}}, op.Security)
	assert.Contains(t, op.Responses["200"].Content, "application/json")
	for _, status := range []string{"400", "401", "409", "413", "429", "500", "501"} {
		assert.Contains(t, op.Responses, status)
	}
}
