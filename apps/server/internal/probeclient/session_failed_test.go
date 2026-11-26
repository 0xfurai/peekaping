package probeclient_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// half is a Backoff whose jitter is spent: each wait is half its step, which doubles from 500ms of
// a 1s step
func half() *probeclient.Backoff {
	return &probeclient.Backoff{Rand: func() float64 { return 0 }}
}

func protocolError(status int, retryAfter time.Duration) error {
	return &probeproto.Error{Status: status, Message: "the server says so", RetryAfter: retryAfter}
}

// greetedSession is a session that said hello to a server that welcomed it, with the logs it keeps
func greetedSession(t *testing.T) (*probeclient.Session, func(level string) []string) {
	t.Helper()
	url, _ := helloServer(t, time.Now(), func(int) (int, string) { return 0, "" })
	logger, logs := observedLogger()
	session := newSession(url, func(c *probeclient.SessionConfig) { c.Logger = logger })
	require.NoError(t, session.Connect(context.Background()))
	require.True(t, session.Greeted())
	return session, func(level string) []string { return logged(logs, level) }
}

// What a session makes of each failure: how long to wait, whether the hello is lost, and what is
// logged at which level
func TestSession_Failed_DecidesTheWaitAndWhetherToSayHelloAgain(t *testing.T) {
	const poll = "poll for assignments"
	tests := []struct {
		name      string
		call      string
		err       error
		wait      time.Duration
		lost      bool
		level     string
		logPrefix string
	}{
		{"a call that never got an answer", poll, errors.New("connection reset"), 500 * time.Millisecond, true, "warn", "Lost the server"},
		{"a 409 to a call other than hello", poll, protocolError(http.StatusConflict, 0), 0, true, "info", "The server doesn't know this instance; saying hello again"},
		{"a 409 to hello", callHello, protocolError(http.StatusConflict, 0), 500 * time.Millisecond, false, "warn", "The server failed a call"},
		{"a 426", poll, protocolError(http.StatusUpgradeRequired, 0), time.Hour, true, "error", "The server doesn't speak this probe's protocol version"},
		{"a 426 that asks for a wait", poll, protocolError(http.StatusUpgradeRequired, time.Minute), time.Hour, true, "error", "The server doesn't speak this probe's protocol version"},
		{"a 401", poll, protocolError(http.StatusUnauthorized, 0), 500 * time.Millisecond, true, "error", "The server refused the probe token"},
		{"a 429 with a Retry-After", poll, protocolError(http.StatusTooManyRequests, 7*time.Second), 7 * time.Second, false, "warn", "The server asked the probe to wait"},
		{"a 429 with a Retry-After past the cap", poll, protocolError(http.StatusTooManyRequests, 10*time.Minute), 5 * time.Minute, false, "warn", "The server asked the probe to wait"},
		{"a 429 with a Retry-After of the cap", poll, protocolError(http.StatusTooManyRequests, 5*time.Minute), 5 * time.Minute, false, "warn", "The server asked the probe to wait"},
		{"a 429 without a Retry-After", poll, protocolError(http.StatusTooManyRequests, 0), 500 * time.Millisecond, false, "warn", "The server asked the probe to wait"},
		{"a 429 with a Retry-After below zero", poll, protocolError(http.StatusTooManyRequests, -time.Second), 500 * time.Millisecond, false, "warn", "The server asked the probe to wait"},
		{"a 503 with a Retry-After", poll, protocolError(http.StatusServiceUnavailable, 3*time.Second), 3 * time.Second, true, "warn", "The server asked the probe to wait"},
		{"a 503 without a Retry-After", poll, protocolError(http.StatusServiceUnavailable, 0), 500 * time.Millisecond, true, "warn", "The server asked the probe to wait"},
		{"a 500", poll, protocolError(http.StatusInternalServerError, 0), 500 * time.Millisecond, true, "warn", "The server failed a call"},
		{"a 502", poll, protocolError(http.StatusBadGateway, 4*time.Second), 500 * time.Millisecond, true, "warn", "The server failed a call"},
		{"a 404", poll, protocolError(http.StatusNotFound, 0), 500 * time.Millisecond, false, "warn", "The server failed a call"},
		{"a 400", poll, protocolError(http.StatusBadRequest, 2*time.Second), 500 * time.Millisecond, false, "warn", "The server failed a call"},
		{"a 401 to hello", callHello, protocolError(http.StatusUnauthorized, 0), 500 * time.Millisecond, true, "error", "The server refused the probe token"},
		{"a 426 to hello", callHello, protocolError(http.StatusUpgradeRequired, 0), time.Hour, true, "error", "The server doesn't speak this probe's protocol version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session, logs := greetedSession(t)

			wait := session.Failed(tc.call, tc.err, half())

			assert.Equal(t, tc.wait, wait)
			assert.Equal(t, !tc.lost, session.Greeted(), "whether the hello is kept")
			entries := logs(tc.level)
			require.NotEmpty(t, entries)
			assert.Contains(t, entries[len(entries)-1], tc.logPrefix)
			assert.Contains(t, entries[len(entries)-1], tc.call)
		})
	}
}

// callHello is the name package probeclient gives the call that says hello
const callHello = "say hello"

// The first 409 to a call is answered with a hello at once; a 409 after it, before the server has
// answered anything else, is backed off, and the waits go on doubling
func TestSession_Failed_SaysHelloAgainOnceFor409AndBacksOffAfterIt(t *testing.T) {
	session, logs := greetedSession(t)
	backoff := half()
	conflict := protocolError(http.StatusConflict, 0)

	assert.Zero(t, session.Failed("send results", conflict, backoff))
	assert.Equal(t, 500*time.Millisecond, session.Failed("send results", conflict, backoff))
	assert.Equal(t, time.Second, session.Failed("send results", conflict, backoff), "and goes on backing off")

	assert.Len(t, logs("info"), 2, "the first 409 is logged at info, with the greeting that follows")
	require.Len(t, logs("warn"), 2)
	assert.Contains(t, logs("warn")[0], "The server doesn't know this instance, though it was greeted since the last answer")

	backoff.Reset()
	assert.Zero(t, session.Failed("send results", conflict, backoff), "an answer from the server starts it over")
}

// The failures that are not a 409 wait on the same backoff, which doubles
func TestSession_Failed_WaitsLongerAfterEachFailure(t *testing.T) {
	session, _ := greetedSession(t)
	backoff := half()

	assert.Equal(t, 500*time.Millisecond, session.Failed("send results", errors.New("down"), backoff))
	assert.Equal(t, time.Second, session.Failed("send results", protocolError(http.StatusUnauthorized, 0), backoff))
	assert.Equal(t, 7*time.Second, session.Failed("send results", protocolError(http.StatusTooManyRequests, 7*time.Second), backoff), "a Retry-After doesn't touch it")
	assert.Equal(t, 2*time.Second, session.Failed("send results", protocolError(http.StatusInternalServerError, 0), backoff))
	assert.Equal(t, time.Hour, session.Failed("send results", protocolError(http.StatusUpgradeRequired, 0), backoff), "nor does a 426")
	assert.Equal(t, 4*time.Second, session.Failed("send results", protocolError(http.StatusBadRequest, 0), backoff))
}
