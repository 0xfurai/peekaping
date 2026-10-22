package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const telemetryPath = "/api/v1/telemetry"

// lowerUUIDv4 is the instance ID the Worker accepts
var lowerUUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type telemetryPayload struct {
	InstanceID               string   `json:"instance_id"`
	Version                  string   `json:"version"`
	Mode                     string   `json:"mode"`
	DB                       string   `json:"db"`
	OS                       string   `json:"os"`
	Monitors                 string   `json:"monitors"`
	MonitorTypes             []string `json:"monitor_types"`
	NotificationChannelTypes []string `json:"notification_providers"`
	Features                 struct {
		StatusPages bool `json:"status_pages"`
		Maintenance bool `json:"maintenance"`
		Metrics     bool `json:"metrics"`
	} `json:"features"`
}

type telemetryState struct {
	Enabled   bool             `json:"enabled"`
	ForcedOff bool             `json:"forced_off"`
	Payload   telemetryPayload `json:"payload"`
}

// telemetryBody checks the status code and returns the response body, which holds the state
// itself and not the {status, message, data} envelope the settings API wraps its data in.
func telemetryBody(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) telemetryState {
	t.Helper()
	require.Equal(t, wantStatus, w.Code, w.Body.String())
	var state telemetryState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &state), w.Body.String())
	return state
}

func (a *app) telemetry(t *testing.T) telemetryState {
	t.Helper()
	return telemetryBody(t, a.request(t, http.MethodGet, telemetryPath, nil, a.asAdmin), http.StatusOK)
}

func (a *app) setTelemetry(t *testing.T, enabled bool) telemetryState {
	t.Helper()
	return telemetryBody(t, a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": enabled}, a.asAdmin), http.StatusOK)
}

func withTelemetryEnv(value string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.Telemetry = value }
}

func withDoNotTrack(value string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.DoNotTrack = value }
}

// storedSetting returns the setting row for key, or nil
func (a *app) storedSetting(t *testing.T, key string) *setting.Model {
	t.Helper()
	var stored *setting.Model
	require.NoError(t, a.container.Invoke(func(settings setting.Service) error {
		var err error
		stored, err = settings.GetByKey(context.Background(), key)
		return err
	}))
	return stored
}

func TestTelemetryAPI_OffByDefault(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodGet, telemetryPath, nil, a.asAdmin)

	state := telemetryBody(t, w, http.StatusOK)
	assert.False(t, state.Enabled)
	assert.False(t, state.ForcedOff)
	assert.Equal(t, "", state.Payload.InstanceID, "no instance ID before the admin opts in")
	assert.Equal(t, "cluster", state.Payload.Mode, "the API under test is cmd/api's, which runs in cluster mode")
	assert.Equal(t, "sqlite", state.Payload.DB)
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, state.Payload.OS)
	assert.Equal(t, "0", state.Payload.Monitors)
	assert.JSONEq(t, `{"status_pages": false, "maintenance": false, "metrics": false}`, string(mustMarshal(t, state.Payload.Features)))
	assert.Contains(t, w.Body.String(), `"monitor_types":[]`, "an empty list is [], never null")
	assert.Contains(t, w.Body.String(), `"notification_providers":[]`, "an empty list is [], never null")
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"), "reading the state stores nothing")
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return encoded
}

func TestTelemetryAPI_OptInCreatesIDOptOutDeletesIt(t *testing.T) {
	a := newAppWithAdmin(t)

	first := a.setTelemetry(t, true)
	assert.True(t, first.Enabled)
	assert.Regexp(t, lowerUUIDv4, first.Payload.InstanceID)
	assert.Equal(t, first, a.telemetry(t), "the state survives a reload")
	assert.Equal(t, first.Payload.InstanceID, a.setTelemetry(t, true).Payload.InstanceID, "opting in twice keeps the ID")
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
	assert.Equal(t, first.Payload.InstanceID, a.storedSetting(t, "TELEMETRY_INSTANCE_ID").Value)

	out := a.setTelemetry(t, false)
	assert.False(t, out.Enabled)
	assert.Equal(t, "", out.Payload.InstanceID)
	assert.Equal(t, out, a.telemetry(t))
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"), "opting out deletes the ID")
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_LAST_SENT"), "and the time of the last ping")

	second := a.setTelemetry(t, true)
	assert.Regexp(t, lowerUUIDv4, second.Payload.InstanceID)
	assert.NotEqual(t, first.Payload.InstanceID, second.Payload.InstanceID, "opting in again gives a new ID")
}

func TestTelemetryAPI_OptingOutWhileOffChangesNothing(t *testing.T) {
	a := newAppWithAdmin(t)

	out := a.setTelemetry(t, false)

	assert.False(t, out.Enabled)
	assert.Equal(t, "", out.Payload.InstanceID)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
}

func TestTelemetryAPI_ForcedOffByEnv(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"TELEMETRY=off":     withTelemetryEnv("off"),
		"TELEMETRY=false":   withTelemetryEnv("false"),
		"TELEMETRY=0":       withTelemetryEnv("0"),
		"TELEMETRY=No":      withTelemetryEnv("No"),
		"DO_NOT_TRACK=1":    withDoNotTrack("1"),
		"DO_NOT_TRACK=true": withDoNotTrack("true"),
	} {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, option)

			assert.True(t, a.telemetry(t).ForcedOff)

			// The choice is still stored, and the environment still wins
			state := a.setTelemetry(t, true)
			assert.True(t, state.Enabled)
			assert.True(t, state.ForcedOff)
			assert.Regexp(t, lowerUUIDv4, state.Payload.InstanceID)
			assert.Equal(t, state, a.telemetry(t))

			state = a.setTelemetry(t, false)
			assert.False(t, state.Enabled)
			assert.True(t, state.ForcedOff)
		})
	}
}

func TestTelemetryAPI_OtherEnvValuesDoNotForceItOff(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"TELEMETRY=on":   withTelemetryEnv("on"),
		"TELEMETRY=1":    withTelemetryEnv("1"),
		"DO_NOT_TRACK=0": withDoNotTrack("0"),
	} {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, option)

			assert.False(t, a.telemetry(t).ForcedOff)
		})
	}
}

func TestTelemetryAPI_PreviewShowsWhatTheInstallHolds(t *testing.T) {
	a := newAppWithAdmin(t, withMetrics)
	first := a.createMonitor(t, "Checkout", "preview-token-1")
	a.createMonitor(t, "Billing", "preview-token-2")
	a.pauseMonitor(t, first)
	decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("On-call webhook"), a.asAdmin), http.StatusCreated)
	a.createStatusPage(t, statusPage{slug: "acme", published: false})
	decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", maintenanceBody("Database upgrade"), a.asAdmin), http.StatusCreated)

	state := a.setTelemetry(t, true)

	assert.Equal(t, "1-10", state.Payload.Monitors, "paused monitors count")
	assert.Equal(t, []string{"push"}, state.Payload.MonitorTypes)
	assert.Equal(t, []string{"webhook"}, state.Payload.NotificationChannelTypes)
	assert.True(t, state.Payload.Features.StatusPages)
	assert.True(t, state.Payload.Features.Maintenance)
	assert.True(t, state.Payload.Features.Metrics)
	body, err := json.Marshal(state)
	require.NoError(t, err)
	for _, private := range []string{"Checkout", "Billing", "preview-token", "On-call", "hooks.example", "acme", "Database upgrade"} {
		assert.NotContains(t, string(body), private)
	}
}

func TestTelemetryAPI_ReportsEveryNotificationChannelTypeTheWorkerAccepts(t *testing.T) {
	// A channel type the Worker would reject is left out of the ping without a trace
	newApp(t)
	typeRule := regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	require.NotEmpty(t, notification_channel.NotificationChannelProviderRegistry)
	for name := range notification_channel.NotificationChannelProviderRegistry {
		assert.Regexp(t, typeRule, name)
	}
}

func TestTelemetryAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)

	for name, body := range map[string]any{
		"malformed JSON":  "{",
		"no enabled":      map[string]any{},
		"null enabled":    map[string]any{"enabled": nil},
		"string enabled":  map[string]any{"enabled": "yes"},
		"number enabled":  map[string]any{"enabled": 1},
		"nothing at all":  "",
		"an array":        []bool{true},
		"enabled as text": `{"enabled": "true"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodPut, telemetryPath, body, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	assert.False(t, a.telemetry(t).Enabled)
}

func TestTelemetryAPI_RequiresCredentials(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := a.request(t, method, telemetryPath, map[string]any{"enabled": true})
		assert.Equal(t, http.StatusUnauthorized, w.Code, method)
	}
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
}

func TestTelemetryAPI_APIKeyCanReadButNotChangeTheChoice(t *testing.T) {
	a := newAppWithAdmin(t)

	// An API key reads the state, like the settings routes it shares auth with
	state := telemetryBody(t, a.request(t, http.MethodGet, telemetryPath, nil, a.withAPIKey), http.StatusOK)
	assert.False(t, state.Enabled)

	// The opt-in is the admin's choice, so an API key can't make it
	w := a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": true}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.False(t, a.telemetry(t).Enabled)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))

	// Nor can it opt the admin out, which would delete the instance ID
	optedIn := a.setTelemetry(t, true)
	w = a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": false}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Equal(t, optedIn, telemetryBody(t, a.request(t, http.MethodGet, telemetryPath, nil, a.withAPIKey), http.StatusOK))
	assert.Equal(t, optedIn.Payload.InstanceID, a.storedSetting(t, "TELEMETRY_INSTANCE_ID").Value)
}

func TestTelemetryAPI_SettingsAPICannotReadOrWriteItsKeys(t *testing.T) {
	a := newAppWithAdmin(t)
	a.setTelemetry(t, true)
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
	instanceID := a.telemetry(t).Payload.InstanceID

	for _, key := range []string{"TELEMETRY_ENABLED", "TELEMETRY_INSTANCE_ID", "TELEMETRY_LAST_SENT"} {
		for _, creds := range []credentials{a.asAdmin, a.withAPIKey} {
			path := "/api/v1/settings/key/" + key
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, creds).Code, "GET "+key)
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPut, path, map[string]any{"value": "false", "type": "string"}, creds).Code, "PUT "+key)
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, path, nil, creds).Code, "DELETE "+key)
		}
	}

	state := a.telemetry(t)
	assert.True(t, state.Enabled, "the settings API did not opt the install out")
	assert.Equal(t, instanceID, state.Payload.InstanceID, "or change its ID")
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_LAST_SENT"))
}
