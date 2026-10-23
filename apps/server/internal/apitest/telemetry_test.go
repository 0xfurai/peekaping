package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"sync"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/reporting/reportingtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
	Enabled       bool             `json:"enabled"`
	ForcedOff     bool             `json:"forced_off"`
	NoticePending bool             `json:"notice_pending"`
	Payload       telemetryPayload `json:"payload"`
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

func TestTelemetryAPI_OnByDefault(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodGet, telemetryPath, nil, a.asAdmin)

	state := telemetryBody(t, w, http.StatusOK)
	assert.True(t, state.Enabled, "on until the admin chooses")
	assert.False(t, state.ForcedOff)
	assert.Regexp(t, lowerUUIDv4, state.Payload.InstanceID, "the ping is on, so the install has an instance ID")
	assert.Equal(t, "cluster", state.Payload.Mode, "the API under test is cmd/api's, which runs in cluster mode")
	assert.Equal(t, "sqlite", state.Payload.DB)
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, state.Payload.OS)
	assert.Equal(t, "0", state.Payload.Monitors)
	assert.JSONEq(t, `{"status_pages": false, "maintenance": false, "metrics": false}`, string(mustMarshal(t, state.Payload.Features)))
	assert.Contains(t, w.Body.String(), `"monitor_types":[]`, "an empty list is [], never null")
	assert.Contains(t, w.Body.String(), `"notification_providers":[]`, "an empty list is [], never null")
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"), "the default is not stored as the admin's choice")
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
	assert.Equal(t, state.Payload.InstanceID, a.storedSetting(t, "TELEMETRY_INSTANCE_ID").Value)
	assert.Equal(t, state, a.telemetry(t), "and the ID stays the same")
}

// withDatabase points a new app at the database of another one, as a restart does
func withDatabase(path string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.DBName = path }
}

func (a *app) databasePath(t *testing.T) string {
	t.Helper()
	var path string
	require.NoError(t, a.container.Invoke(func(cfg *config.Config) { path = cfg.DBName }))
	return path
}

// restart builds a new server on the same database, with the admin still signed in: the signing
// secrets are in the database
func (a *app) restart(t *testing.T, options ...func(*config.Config)) *app {
	t.Helper()
	restarted := newApp(t, append([]func(*config.Config){withDatabase(a.databasePath(t))}, options...)...)
	restarted.accessToken, restarted.apiKey = a.accessToken, a.apiKey
	return restarted
}

func TestTelemetryAPI_AnInstallWithNoChoiceKeepsOneIDAcrossRestarts(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.telemetry(t)
	require.Regexp(t, lowerUUIDv4, first.Payload.InstanceID)

	restarted := a.restart(t)

	assert.Equal(t, first.Payload.InstanceID, restarted.telemetry(t).Payload.InstanceID)
}

func TestTelemetryAPI_AnExplicitOptOutSurvivesARestart(t *testing.T) {
	a := newAppWithAdmin(t)
	before := a.telemetry(t)
	require.NotEmpty(t, before.Payload.InstanceID)

	out := a.setTelemetry(t, false)
	assert.False(t, out.Enabled)
	assert.Equal(t, "", out.Payload.InstanceID)

	restarted := a.restart(t)
	state := restarted.telemetry(t)
	assert.False(t, state.Enabled, "the choice is stored")
	assert.Equal(t, "", state.Payload.InstanceID)
	assert.Nil(t, restarted.storedSetting(t, "TELEMETRY_INSTANCE_ID"), "and no ID is made again")
	assert.Equal(t, "false", restarted.storedSetting(t, "TELEMETRY_ENABLED").Value)

	in := restarted.setTelemetry(t, true)
	assert.Regexp(t, lowerUUIDv4, in.Payload.InstanceID)
	assert.NotEqual(t, before.Payload.InstanceID, in.Payload.InstanceID, "opting back in makes a new ID")
}

func TestTelemetryAPI_KillSwitchesWinWhenNothingIsStored(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"TELEMETRY=off": withTelemetryEnv("off"),
	} {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, option)

			state := a.telemetry(t)

			assert.True(t, state.ForcedOff)
			assert.Equal(t, "", state.Payload.InstanceID, "no identifier is made while the environment forbids the ping")
			assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
			assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
		})
	}
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

func TestTelemetryAPI_OptingOutTwiceChangesNothingTheSecondTime(t *testing.T) {
	a := newAppWithAdmin(t)
	a.setTelemetry(t, false)

	out := a.setTelemetry(t, false)

	assert.False(t, out.Enabled)
	assert.Equal(t, "", out.Payload.InstanceID)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
}

func TestTelemetryAPI_ForcedOffByEnv(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"TELEMETRY=off":   withTelemetryEnv("off"),
		"TELEMETRY=false": withTelemetryEnv("false"),
		"TELEMETRY=0":     withTelemetryEnv("0"),
		"TELEMETRY=No":    withTelemetryEnv("No"),
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
		"TELEMETRY=on": withTelemetryEnv("on"),
		"TELEMETRY=1":  withTelemetryEnv("1"),
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
	types := notification_channel.NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{}).Types()
	require.NotEmpty(t, types)
	for _, name := range types {
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
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"), "a rejected request stores no choice")
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
	assert.True(t, state.Enabled)

	// The choice is the admin's, so an API key can't opt the install out
	w := a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": false}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Equal(t, state, a.telemetry(t))
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
	assert.Equal(t, state.Payload.InstanceID, a.storedSetting(t, "TELEMETRY_INSTANCE_ID").Value, "and delete the instance ID")

	// Nor opt it back in after the admin opted out
	a.setTelemetry(t, false)
	w = a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": true}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.False(t, a.telemetry(t).Enabled)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
}

func TestTelemetryAPI_SettingsAPICannotReadOrWriteItsKeys(t *testing.T) {
	a := newAppWithAdmin(t)
	a.setTelemetry(t, true)
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_ENABLED"))
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_INSTANCE_ID"))
	instanceID := a.telemetry(t).Payload.InstanceID

	for _, key := range []string{"TELEMETRY_ENABLED", "TELEMETRY_INSTANCE_ID", "TELEMETRY_LAST_SENT", "TELEMETRY_NOTICE_SEEN"} {
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

const telemetryNoticePath = telemetryPath + "/notice"

func (a *app) markNoticeSeen(t *testing.T) telemetryState {
	t.Helper()
	return telemetryBody(t, a.request(t, http.MethodPost, telemetryNoticePath, nil, a.asAdmin), http.StatusOK)
}

// An install that upgrades has no stored choice and no notice flag, and starts sending: its admin
// is told once
func TestTelemetryAPI_NoticeIsPendingOnAnUpgradedInstall(t *testing.T) {
	a := newAppWithAdmin(t)

	state := a.telemetry(t)

	assert.True(t, state.Enabled)
	assert.True(t, state.NoticePending)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_NOTICE_SEEN"), "reading the state does not mark it seen")
	assert.True(t, a.telemetry(t).NoticePending, "and it stays pending until dismissed")
}

func TestTelemetryAPI_PostingTheNoticeMarksItSeenForGood(t *testing.T) {
	a := newAppWithAdmin(t)
	before := a.telemetry(t)

	seen := a.markNoticeSeen(t)

	assert.False(t, seen.NoticePending)
	assert.Equal(t, before.Payload.InstanceID, seen.Payload.InstanceID)
	assert.True(t, seen.Enabled, "dismissing the notice is not a choice")
	assert.Equal(t, seen, a.telemetry(t))
	require.NotNil(t, a.storedSetting(t, "TELEMETRY_NOTICE_SEEN"))
	assert.Equal(t, "true", a.storedSetting(t, "TELEMETRY_NOTICE_SEEN").Value)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_ENABLED"), "and stores no choice")
	assert.Equal(t, seen, a.markNoticeSeen(t), "posting it again is harmless")
	assert.False(t, a.restart(t).telemetry(t).NoticePending, "it stays seen after a restart")
}

func TestTelemetryAPI_AnyChoiceMarksTheNoticeSeen(t *testing.T) {
	for name, choose := range map[string]func(*app, *testing.T){
		"usage statistics on":  func(a *app, t *testing.T) { a.setTelemetry(t, true) },
		"usage statistics off": func(a *app, t *testing.T) { a.setTelemetry(t, false) },
		"error reports on":     func(a *app, t *testing.T) { a.setErrorReporting(t, true) },
		"error reports off":    func(a *app, t *testing.T) { a.setErrorReporting(t, false) },
	} {
		t.Run(name, func(t *testing.T) {
			reportingtest.Install(t)
			a := newAppWithAdmin(t)
			require.True(t, a.telemetry(t).NoticePending)

			choose(a, t)

			assert.False(t, a.telemetry(t).NoticePending)
			assert.Equal(t, "true", a.storedSetting(t, "TELEMETRY_NOTICE_SEEN").Value)
		})
	}
}

// The PUT answers with the state, which no longer asks for the notice
func TestTelemetryAPI_APutAnswersWithoutAPendingNotice(t *testing.T) {
	a := newAppWithAdmin(t)

	assert.False(t, a.setTelemetry(t, false).NoticePending)
}

func TestTelemetryAPI_NoNoticeWhenBothFeaturesAreForcedOff(t *testing.T) {
	for name, tc := range map[string]struct {
		options []func(*config.Config)
		pending bool
	}{
		"nothing forced off":                 {nil, true},
		"TELEMETRY=off only":                 {[]func(*config.Config){withTelemetryEnv("off")}, true},
		"ERROR_REPORTING=off only":           {[]func(*config.Config){withErrorReportingEnv("off")}, true},
		"TELEMETRY=off, ERROR_REPORTING=off": {[]func(*config.Config){withTelemetryEnv("off"), withErrorReportingEnv("off")}, false},
	} {
		t.Run(name, func(t *testing.T) {
			a := newAppWithAdmin(t, tc.options...)

			assert.Equal(t, tc.pending, a.telemetry(t).NoticePending)
		})
	}
}

func TestTelemetryAPI_TheNoticeCannotBeMarkedSeenWithoutTheAdminsJWT(t *testing.T) {
	a := newAppWithAdmin(t)

	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, telemetryNoticePath, nil).Code, "no credentials")
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, telemetryNoticePath, nil, a.withAPIKey).Code, "an API key")

	assert.True(t, a.telemetry(t).NoticePending)
	assert.Nil(t, a.storedSetting(t, "TELEMETRY_NOTICE_SEEN"))
}

// The setup form sends both choices at once. Each choice also marks the notice seen, with the
// same "true", and the two writes must not fail each other: the request that loses the race has
// stored its choice already, and answering 500 would tell the admin it had not.
func TestTelemetryAPI_BothChoicesSentAtOnceBothSucceed(t *testing.T) {
	reportingtest.Install(t)
	a := newAppWithAdmin(t)
	const rounds = 200

	for round := range rounds {
		require.NoError(t, a.container.Invoke(func(settings shared.SettingService) error {
			return settings.DeleteByKey(context.Background(), shared.TelemetryNoticeSeenKey)
		}))
		start := make(chan struct{})
		var wg sync.WaitGroup
		var usage, reports *httptest.ResponseRecorder
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			usage = a.request(t, http.MethodPut, telemetryPath, map[string]any{"enabled": false}, a.asAdmin)
		}()
		go func() {
			defer wg.Done()
			<-start
			reports = a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": false}, a.asAdmin)
		}()
		close(start)
		wg.Wait()

		require.Equal(t, http.StatusOK, usage.Code, "round %d, usage statistics: %s", round, usage.Body.String())
		require.Equal(t, http.StatusOK, reports.Code, "round %d, error reports: %s", round, reports.Body.String())
		require.NotNil(t, a.storedSetting(t, "TELEMETRY_NOTICE_SEEN"))
		assert.Equal(t, "true", a.storedSetting(t, "TELEMETRY_NOTICE_SEEN").Value)
	}
	assert.Equal(t, "false", a.storedSetting(t, "TELEMETRY_ENABLED").Value)
	assert.Equal(t, "false", a.storedSetting(t, "ERROR_REPORTING_ENABLED").Value)
}
