package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/reporting"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSentry stands in for the Sentry server and keeps the events it is sent
type fakeSentry struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newFakeSentry(t *testing.T) *fakeSentry {
	t.Helper()
	s := &fakeSentry{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(body))
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeSentry) dsn() string {
	return "http://public@" + strings.TrimPrefix(s.URL, "http://") + "/1"
}

// events are the events of the envelopes received: the third line of an envelope
func (s *fakeSentry) events() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []map[string]any
	for _, body := range s.bodies {
		lines := strings.Split(strings.TrimSpace(body), "\n")
		if len(lines) < 3 {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(lines[2]), &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

type errorReportingState struct {
	Enabled   bool `json:"enabled"`
	ForcedOff bool `json:"forced_off"`
}

func (in *instance) setErrorReporting(t *testing.T, enabled bool) errorReportingState {
	t.Helper()
	status, body := in.request(t, http.MethodPut, "/api/v1/error-reporting", map[string]any{"enabled": enabled})
	require.Equal(t, http.StatusOK, status, string(body))
	var state errorReportingState
	require.NoError(t, json.Unmarshal(body, &state), string(body))
	return state
}

// reportAPanic reports a panic from this test, as a hook that recovered one would
func reportAPanic() {
	defer func() {
		reporting.CapturePanic("test", recover(), debug.Stack())
	}()
	panic("check failed for https://secret.example.com/health?token=abc")
}

func TestRun_ReportsPanicsOnlyAfterTheAdminOptsInAndFlushesAtShutdown(t *testing.T) {
	sentry := newFakeSentry(t)
	t.Cleanup(func() { reporting.Setup(reporting.Options{}) })
	cfg := loadConfig(t, map[string]string{"ERROR_REPORTING_DSN": sentry.dsn(), "ERROR_REPORTING": "", "DO_NOT_TRACK": ""})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	assert.False(t, reporting.Enabled(), "off until the admin opts in")
	reportAPanic()
	assert.False(t, in.setErrorReporting(t, false).Enabled)
	assert.False(t, reporting.Enabled())

	state := in.setErrorReporting(t, true)
	assert.Equal(t, errorReportingState{Enabled: true}, state)
	assert.True(t, reporting.Enabled(), "a change in Settings takes effect in the one process at once")

	reportAPanic()
	require.NoError(t, in.stop(t), "the report is flushed as the process shuts down")

	require.Eventually(t, func() bool { return len(sentry.events()) == 1 }, wait, 10*time.Millisecond)
	event := sentry.events()[0]
	assert.Equal(t, version.Version, event["release"])
	assert.Equal(t, map[string]any{
		"mode": "single", "db": "sqlite", "os": runtime.GOOS + "/" + runtime.GOARCH, "component": "test",
	}, event["tags"], "peekaping runs in single mode, whatever MODE says")
	sent, err := json.Marshal(event)
	require.NoError(t, err)
	assert.NotContains(t, string(sent), "secret.example.com")
	assert.NotContains(t, string(sent), "token=abc")
}

func TestRun_AppliesTheStoredChoiceAtStart(t *testing.T) {
	sentry := newFakeSentry(t)
	t.Cleanup(func() { reporting.Setup(reporting.Options{}) })
	cfg := loadConfig(t, map[string]string{"ERROR_REPORTING_DSN": sentry.dsn(), "ERROR_REPORTING": "", "DO_NOT_TRACK": ""})
	logger, logs := observedLogger()
	first := startPeekaping(t, cfg, logger, logs)
	first.registerAdmin(t)
	first.setErrorReporting(t, true)
	require.NoError(t, first.stop(t))

	logger, logs = observedLogger()
	second := startPeekaping(t, cfg, logger, logs)

	require.Eventually(t, reporting.Enabled, wait, 10*time.Millisecond, "the admin opted in before the restart")
	require.NoError(t, second.stop(t))
}

func TestRun_SendsNoErrorReportWhenTheEnvironmentForcesThemOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"ERROR_REPORTING=off": {"ERROR_REPORTING": "off"},
		"DO_NOT_TRACK=1":      {"DO_NOT_TRACK": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			sentry := newFakeSentry(t)
			t.Cleanup(func() { reporting.Setup(reporting.Options{}) })
			env["ERROR_REPORTING_DSN"] = sentry.dsn()
			cfg := loadConfig(t, env)
			logger, logs := observedLogger()
			in := startPeekaping(t, cfg, logger, logs)
			in.registerAdmin(t)

			state := in.setErrorReporting(t, true)
			reportAPanic()
			require.NoError(t, in.stop(t))

			assert.Equal(t, errorReportingState{Enabled: true, ForcedOff: true}, state, "the choice is stored, and the environment wins")
			assert.False(t, reporting.Enabled())
			time.Sleep(200 * time.Millisecond)
			assert.Empty(t, sentry.events())
		})
	}
}
