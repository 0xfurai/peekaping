package probeclient_test

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helloServer answers each hello with the status the script gives for its number (from 1): 0 is a
// welcome from a server whose clock reads serverTime
func helloServer(t *testing.T, serverTime time.Time, script func(n int) (status int, message string)) (url string, hellos *atomic.Int32) {
	t.Helper()
	hellos = new(atomic.Int32)
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := int(hellos.Add(1))
		if status, message := script(n); status != 0 {
			fail(w, status, message, 0)
			return
		}
		reply(w, http.StatusOK, welcomeFrom(serverTime))
	})
	return server.URL, hellos
}

func newSession(url string, mod func(*probeclient.SessionConfig)) *probeclient.Session {
	cfg := probeclient.SessionConfig{URL: url, Token: probeToken, ProbeVersion: "0.0.48", SupportedTypes: []string{"http", "tcp"}, Logger: nopLogger()}
	if mod != nil {
		mod(&cfg)
	}
	return probeclient.NewSession(cfg)
}

func TestSession_HelloCarriesTheInstance(t *testing.T) {
	var got probeproto.HelloRequest
	var header, auth string
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/probe/v1/hello", r.URL.Path)
		readJSON(t, r, &got)
		header, auth = r.Header.Get(probeproto.HeaderInstanceID), r.Header.Get("Authorization")
		reply(w, http.StatusOK, welcomeFrom(time.Now()))
	})
	logger, logs := observedLogger()
	session := newSession(server.URL, func(c *probeclient.SessionConfig) { c.Logger = logger })

	require.NoError(t, session.Connect(context.Background()))

	hostname, err := os.Hostname()
	require.NoError(t, err)
	_, err = uuid.Parse(got.InstanceID)
	require.NoError(t, err, "the instance id is random")
	assert.Equal(t, probeproto.HelloRequest{
		InstanceID: got.InstanceID, ProtocolVersion: 1, ProbeVersion: "0.0.48", SupportedTypes: []string{"http", "tcp"},
		Hostname: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH,
	}, got)
	assert.Equal(t, got.InstanceID, header, "and the header of every request names the same instance")
	assert.Equal(t, got.InstanceID, session.InstanceID())
	assert.Equal(t, "Bearer "+probeToken, auth)
	assert.Equal(t, "Frankfurt", session.Welcome().Probe.Name)

	connected := logs.FilterMessage("Connected to the server").All()
	require.Len(t, connected, 1, "it logs who it is connected as")
	assert.Equal(t, "Frankfurt", connected[0].ContextMap()["probe"])
	assert.Equal(t, "0.0.48", connected[0].ContextMap()["server_version"])

	other := newSession(server.URL, nil)
	require.NoError(t, other.Connect(context.Background()))
	assert.NotEqual(t, session.InstanceID(), other.InstanceID(), "each process has an instance id of its own")
}

func TestSession_ConnectSaysHelloOnlyOnce(t *testing.T) {
	url, hellos := helloServer(t, time.Now(), func(int) (int, string) { return 0, "" })
	session := newSession(url, nil)

	require.NoError(t, session.Connect(context.Background()))
	require.NoError(t, session.Connect(context.Background()))

	assert.EqualValues(t, 1, hellos.Load())
}

func TestSession_RetriesHourlyOn426(t *testing.T) {
	const message = "protocol version 1 is below the server's minimum 2: upgrade the probe"
	url, hellos := helloServer(t, time.Now(), func(n int) (int, string) {
		if n <= 3 {
			return http.StatusUpgradeRequired, message
		}
		return 0, ""
	})
	logger, logs := observedLogger()
	pause := &sleeper{}
	session := newSession(url, func(c *probeclient.SessionConfig) { c.Logger, c.Sleep = logger, pause.Sleep })

	err := session.Connect(context.Background())

	require.NoError(t, err, "the probe keeps trying until the server speaks its version, it doesn't give up")
	assert.EqualValues(t, 4, hellos.Load())
	assert.Equal(t, []time.Duration{time.Hour, time.Hour, time.Hour}, pause.waits())
	errors := logged(logs, "error")
	require.Len(t, errors, 3)
	assert.Contains(t, errors[0], message, "each is logged with the server's message")
}

func TestSession_BacksOffOn401(t *testing.T) {
	url, hellos := helloServer(t, time.Now(), func(n int) (int, string) {
		if n <= 5 {
			return http.StatusUnauthorized, "invalid probe token"
		}
		return 0, ""
	})
	logger, logs := observedLogger()
	pause := &sleeper{}
	session := newSession(url, func(c *probeclient.SessionConfig) { c.Logger, c.Sleep = logger, pause.Sleep })

	err := session.Connect(context.Background())

	require.NoError(t, err)
	assert.EqualValues(t, 6, hellos.Load())
	waits := pause.waits()
	require.Len(t, waits, 5)
	for i, step := range []time.Duration{1, 2, 4, 8, 16} {
		assert.GreaterOrEqual(t, waits[i], step*time.Second/2, "wait %d", i+1)
		assert.LessOrEqual(t, waits[i], step*time.Second, "wait %d", i+1)
	}
	errors := logged(logs, "error")
	require.Len(t, errors, 5, "every refusal is logged")
	assert.Contains(t, errors[0], "invalid probe token")
}

// A 409 answers a call that needs a hello first. Said of hello itself it makes no sense, and a
// server that goes on saying it must not be asked again in a tight loop.
func TestSession_BacksOffOn409ToHello(t *testing.T) {
	url, hellos := helloServer(t, time.Now(), func(n int) (int, string) {
		if n <= 4 {
			return http.StatusConflict, "unknown instance: send hello first"
		}
		return 0, ""
	})
	pause := &sleeper{}
	session := newSession(url, func(c *probeclient.SessionConfig) { c.Sleep = pause.Sleep })

	require.NoError(t, session.Connect(context.Background()))

	assert.EqualValues(t, 5, hellos.Load())
	waits := pause.waits()
	require.Len(t, waits, 4, "every refused hello is followed by a wait")
	for i, step := range []time.Duration{1, 2, 4, 8} {
		assert.GreaterOrEqual(t, waits[i], step*time.Second/2, "wait %d", i+1)
		assert.LessOrEqual(t, waits[i], step*time.Second, "wait %d", i+1)
	}
}

func TestSession_WarnsOnClockSkew(t *testing.T) {
	local := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		serverTime time.Time
		warns      bool
	}{
		{"a server 31 s ahead", local.Add(31 * time.Second), true},
		{"a server 2 minutes behind", local.Add(-2 * time.Minute), true},
		{"a server 29 s ahead", local.Add(29 * time.Second), false},
		{"a server 29 s behind", local.Add(-29 * time.Second), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, _ := helloServer(t, tt.serverTime, func(int) (int, string) { return 0, "" })
			logger, logs := observedLogger()
			session := newSession(url, func(c *probeclient.SessionConfig) {
				c.Logger, c.Now = logger, func() time.Time { return local }
			})

			require.NoError(t, session.Connect(context.Background()))

			warnings := logged(logs, "warn")
			if tt.warns {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "clock")
			} else {
				assert.Empty(t, warnings)
			}
		})
	}
}

func TestSession_HonoursRetryAfterOnHello(t *testing.T) {
	var hellos atomic.Int32
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch hellos.Add(1) {
		case 1:
			fail(w, http.StatusTooManyRequests, "too many requests", 7*time.Second)
		case 2:
			fail(w, http.StatusServiceUnavailable, "starting up", 3*time.Second)
		default:
			reply(w, http.StatusOK, welcomeFrom(time.Now()))
		}
	})
	pause := &sleeper{}
	session := newSession(server.URL, func(c *probeclient.SessionConfig) { c.Sleep = pause.Sleep })

	require.NoError(t, session.Connect(context.Background()))

	assert.Equal(t, []time.Duration{7 * time.Second, 3 * time.Second}, pause.waits())
}

func TestSession_CapsRetryAfterAtFiveMinutes(t *testing.T) {
	var hellos atomic.Int32
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch hellos.Add(1) {
		case 1:
			fail(w, http.StatusTooManyRequests, "too many requests", 24*time.Hour)
		case 2:
			fail(w, http.StatusServiceUnavailable, "starting up", 301*time.Second)
		case 3:
			fail(w, http.StatusServiceUnavailable, "starting up", 5*time.Minute)
		case 4:
			fail(w, http.StatusTooManyRequests, "too many requests", 299*time.Second)
		default:
			reply(w, http.StatusOK, welcomeFrom(time.Now()))
		}
	})
	pause := &sleeper{}
	session := newSession(server.URL, func(c *probeclient.SessionConfig) { c.Sleep = pause.Sleep })

	require.NoError(t, session.Connect(context.Background()))

	assert.Equal(t, []time.Duration{5 * time.Minute, 5 * time.Minute, 5 * time.Minute, 299 * time.Second}, pause.waits(),
		"a Retry-After past five minutes is cut to five, and one within them is obeyed as is")
}

func TestSession_ConnectStopsWithItsContext(t *testing.T) {
	url, _ := helloServer(t, time.Now(), func(int) (int, string) { return http.StatusUpgradeRequired, "upgrade" })
	ctx, cancel := context.WithCancel(context.Background())
	pause := &sleeper{after: func(n int, _ time.Duration) {
		if n == 2 {
			cancel()
		}
	}}
	session := newSession(url, func(c *probeclient.SessionConfig) { c.Sleep = pause.Sleep })

	err := session.Connect(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}
