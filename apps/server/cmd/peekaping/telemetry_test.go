package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/telemetry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usagePingWorker stands in for the telemetry Worker and keeps the pings it gets
type usagePingWorker struct {
	*httptest.Server
	mu    sync.Mutex
	pings []telemetry.Payload
}

func newUsagePingWorker(t *testing.T) *usagePingWorker {
	t.Helper()
	wk := &usagePingWorker{}
	wk.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var ping telemetry.Payload
		if assert.NoError(t, json.Unmarshal(body, &ping), string(body)) {
			wk.mu.Lock()
			wk.pings = append(wk.pings, ping)
			wk.mu.Unlock()
		}
	}))
	t.Cleanup(wk.Close)
	return wk
}

func (wk *usagePingWorker) received() []telemetry.Payload {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	return append([]telemetry.Payload(nil), wk.pings...)
}

// checkOften makes the sender check every few milliseconds, until the test ends
func checkOften(t *testing.T) {
	t.Helper()
	saved := telemetrySchedule
	t.Cleanup(func() { telemetrySchedule = saved })
	telemetrySchedule = telemetry.Schedule{FirstCheck: 10 * time.Millisecond, Every: 10 * time.Millisecond}
}

type telemetryState struct {
	Enabled   bool              `json:"enabled"`
	ForcedOff bool              `json:"forced_off"`
	Payload   telemetry.Payload `json:"payload"`
}

func (in *instance) setTelemetry(t *testing.T, enabled bool) telemetryState {
	t.Helper()
	status, body := in.request(t, http.MethodPut, "/api/v1/telemetry", map[string]any{"enabled": enabled})
	require.Equal(t, http.StatusOK, status, string(body))
	var state telemetryState
	require.NoError(t, json.Unmarshal(body, &state), string(body))
	return state
}

func TestRun_SendsTheUsagePingOnceADayOnlyAfterTheAdminOptsIn(t *testing.T) {
	checkOften(t)
	wk := newUsagePingWorker(t)
	cfg := loadConfig(t, map[string]string{"TELEMETRY_URL": wk.URL, "TELEMETRY": "", "DO_NOT_TRACK": ""})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, wk.received(), "nothing is sent before the admin opts in")

	state := in.setTelemetry(t, true)

	require.Eventually(t, func() bool { return len(wk.received()) == 1 }, wait, 10*time.Millisecond, "the ping arrives")
	ping := wk.received()[0]
	assert.Equal(t, state.Payload, ping, "the ping is the payload the preview showed")
	assert.Equal(t, "single", ping.Mode, "peekaping runs in single mode, whatever MODE says")
	assert.Equal(t, "sqlite", ping.DB)
	assert.NotEmpty(t, ping.InstanceID)
	time.Sleep(300 * time.Millisecond)
	assert.Len(t, wk.received(), 1, "dozens of checks, one ping a day")

	require.NoError(t, in.stop(t), "the sender stops with the other background jobs")
}

func TestRun_SendsNothingWhenTheEnvironmentForcesTheUsagePingOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"TELEMETRY=off":  {"TELEMETRY": "off"},
		"DO_NOT_TRACK=1": {"DO_NOT_TRACK": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			checkOften(t)
			wk := newUsagePingWorker(t)
			env["TELEMETRY_URL"] = wk.URL
			cfg := loadConfig(t, env)
			logger, logs := observedLogger()
			in := startPeekaping(t, cfg, logger, logs)
			in.registerAdmin(t)

			state := in.setTelemetry(t, true)
			time.Sleep(300 * time.Millisecond)

			assert.True(t, state.Enabled)
			assert.True(t, state.ForcedOff)
			assert.Empty(t, wk.received(), "the environment wins over the admin's choice")
			require.NoError(t, in.stop(t))
		})
	}
}
