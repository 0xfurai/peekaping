package main

import (
	"encoding/json"
	"fmt"
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

func (in *instance) telemetry(t *testing.T) telemetryState {
	t.Helper()
	status, body := in.request(t, http.MethodGet, "/api/v1/telemetry", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var state telemetryState
	require.NoError(t, json.Unmarshal(body, &state), string(body))
	return state
}

func (in *instance) setTelemetry(t *testing.T, enabled bool) telemetryState {
	t.Helper()
	status, body := in.request(t, http.MethodPut, "/api/v1/telemetry", map[string]any{"enabled": enabled})
	require.Equal(t, http.StatusOK, status, string(body))
	var state telemetryState
	require.NoError(t, json.Unmarshal(body, &state), string(body))
	return state
}

func TestRun_SendsTheUsagePingOnceADayByDefault(t *testing.T) {
	checkOften(t)
	wk := newUsagePingWorker(t)
	cfg := loadConfig(t, map[string]string{"TELEMETRY_URL": wk.URL, "TELEMETRY": ""})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	require.Eventually(t, func() bool { return len(wk.received()) == 1 }, wait, 10*time.Millisecond, "the ping arrives without the admin choosing")
	ping := wk.received()[0]
	state := in.telemetry(t)
	assert.True(t, state.Enabled)
	assert.Equal(t, state.Payload, ping, "the ping is the payload the preview shows")
	assert.Equal(t, "single", ping.Mode, "peekaping runs in single mode, whatever MODE says")
	assert.Equal(t, "sqlite", ping.DB)
	assert.NotEmpty(t, ping.InstanceID)
	time.Sleep(300 * time.Millisecond)
	assert.Len(t, wk.received(), 1, "dozens of checks, one ping a day")

	require.NoError(t, in.stop(t), "the sender stops with the other background jobs")
}

func TestRun_SendsNoUsagePingAfterTheAdminOptsOutNotEvenAfterARestart(t *testing.T) {
	checkOften(t)
	wk := newUsagePingWorker(t)
	cfg := loadConfig(t, map[string]string{"TELEMETRY_URL": wk.URL, "TELEMETRY": ""})
	logger, logs := observedLogger()
	first := startPeekaping(t, cfg, logger, logs)
	first.registerAdmin(t)
	state := first.setTelemetry(t, false)
	assert.False(t, state.Enabled)
	assert.Empty(t, state.Payload.InstanceID)
	pingsBeforeTheChoice := len(wk.received())
	require.NoError(t, first.stop(t))

	logger, logs = observedLogger()
	second := startPeekaping(t, cfg, logger, logs)
	second.accessToken = first.accessToken // the signing secrets are in the database
	time.Sleep(300 * time.Millisecond)

	assert.Len(t, wk.received(), pingsBeforeTheChoice, "the opt-out is stored: nothing is sent after the restart")
	assert.False(t, second.telemetry(t).Enabled)
	require.NoError(t, second.stop(t))
}

func TestRun_SendsNothingWhenTheEnvironmentForcesTheUsagePingOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"TELEMETRY=off": {"TELEMETRY": "off"},
	} {
		for _, choose := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, admin chose=%v", name, choose), func(t *testing.T) {
				checkOften(t)
				wk := newUsagePingWorker(t)
				env["TELEMETRY_URL"] = wk.URL
				cfg := loadConfig(t, env)
				logger, logs := observedLogger()
				in := startPeekaping(t, cfg, logger, logs)
				in.registerAdmin(t)

				state := in.telemetry(t)
				if choose {
					state = in.setTelemetry(t, true)
				}
				time.Sleep(300 * time.Millisecond)

				assert.True(t, state.Enabled)
				assert.True(t, state.ForcedOff)
				assert.Empty(t, wk.received(), "the environment wins over the default and over the admin's choice")
				require.NoError(t, in.stop(t))
			})
		}
	}
}
