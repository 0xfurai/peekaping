package apitest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wiring "peekaping/internal/app"
	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

const errorReportingPath = "/api/v1/error-reporting"

// errorReportingBody checks the status code and returns the response body, which holds the state
// itself and not the {status, message, data} envelope the settings API wraps its data in.
func errorReportingBody(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) map[string]any {
	t.Helper()
	require.Equal(t, wantStatus, w.Code, w.Body.String())
	var state map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &state), w.Body.String())
	return state
}

func (a *app) errorReporting(t *testing.T) map[string]any {
	t.Helper()
	return errorReportingBody(t, a.request(t, http.MethodGet, errorReportingPath, nil, a.asAdmin), http.StatusOK)
}

func (a *app) setErrorReporting(t *testing.T, enabled bool) map[string]any {
	t.Helper()
	return errorReportingBody(t, a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": enabled}, a.asAdmin), http.StatusOK)
}

func withErrorReportingEnv(value string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.ErrorReporting = value }
}

func TestErrorReportingAPI_OnByDefault(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodGet, errorReportingPath, nil, a.asAdmin)

	assert.JSONEq(t, `{"enabled": true, "forced_off": false}`, w.Body.String(), "a bare state, with nothing else in it")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"), "reading the state stores nothing: the default is not the admin's choice")
}

// The api applies the stored choice at its start (cmd/api). With nothing stored that turns
// reporting on in the api, and a panic is reported.
func TestErrorReportingAPI_ReportsByDefaultOnceTheApiHasStarted(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(stale.Store) stale.Store { return panickingStore{} })
	})
	a.registerAdmin(t)

	require.NoError(t, wiring.ApplyErrorReporting(context.Background(), a.container))

	assert.True(t, reporting.Enabled())
	assert.Nil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"))
	assert.Equal(t, http.StatusInternalServerError, a.request(t, http.MethodGet, "/api/v1/health", nil).Code)
	require.Len(t, recorder.Events(), 1, "the panic is reported without the admin having chosen")
}

// An opt-out is stored, and survives a restart of the api
func TestErrorReportingAPI_AnOptOutSurvivesARestart(t *testing.T) {
	reportingtest.Install(t)
	a := newAppWithAdmin(t)
	a.setErrorReporting(t, false)

	restarted := a.restart(t)
	require.NoError(t, wiring.ApplyErrorReporting(context.Background(), restarted.container))

	assert.False(t, reporting.Enabled())
	assert.Equal(t, map[string]any{"enabled": false, "forced_off": false}, restarted.errorReporting(t))
}

func TestErrorReportingAPI_KillSwitchesWinWhenNothingIsStored(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"ERROR_REPORTING=off": withErrorReportingEnv("off"),
	} {
		t.Run(name, func(t *testing.T) {
			reportingtest.Install(t)
			reporting.SetEnabled(false)
			a := newAppWithAdmin(t, option)

			require.NoError(t, wiring.ApplyErrorReporting(context.Background(), a.container))

			assert.False(t, reporting.Enabled())
			assert.Equal(t, map[string]any{"enabled": true, "forced_off": true}, a.errorReporting(t))
		})
	}
}

func TestErrorReportingAPI_PutStoresTheChoiceAndTurnsReportingOnAndOff(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppWithAdmin(t)

	assert.JSONEq(t, `{"enabled": true, "forced_off": false}`, a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": true}, a.asAdmin).Body.String())
	assert.True(t, reporting.Enabled(), "the api's own process turns it on at once")
	assert.Equal(t, map[string]any{"enabled": true, "forced_off": false}, a.errorReporting(t), "the state survives a reload")
	require.NotNil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"))
	assert.Equal(t, "true", a.storedSetting(t, "ERROR_REPORTING_ENABLED").Value)

	assert.JSONEq(t, `{"enabled": false, "forced_off": false}`, a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": false}, a.asAdmin).Body.String())
	assert.False(t, reporting.Enabled())
	assert.Equal(t, "false", a.storedSetting(t, "ERROR_REPORTING_ENABLED").Value)
}

func TestErrorReportingAPI_ForcedOffByEnv(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"ERROR_REPORTING=off":   withErrorReportingEnv("off"),
		"ERROR_REPORTING=false": withErrorReportingEnv("false"),
		"ERROR_REPORTING=0":     withErrorReportingEnv("0"),
		"ERROR_REPORTING=No":    withErrorReportingEnv("No"),
	} {
		t.Run(name, func(t *testing.T) {
			reportingtest.Install(t)
			a := newAppWithAdmin(t, option)

			assert.Equal(t, true, a.errorReporting(t)["forced_off"])

			// The choice is still stored, and the environment still wins
			state := a.setErrorReporting(t, true)
			assert.Equal(t, map[string]any{"enabled": true, "forced_off": true}, state)
			assert.Equal(t, state, a.errorReporting(t))
			assert.Equal(t, "true", a.storedSetting(t, "ERROR_REPORTING_ENABLED").Value)
			assert.False(t, reporting.Enabled(), "the api sends nothing")

			state = a.setErrorReporting(t, false)
			assert.Equal(t, map[string]any{"enabled": false, "forced_off": true}, state)
		})
	}
}

func TestErrorReportingAPI_OtherEnvValuesDoNotForceItOff(t *testing.T) {
	for name, option := range map[string]func(*config.Config){
		"ERROR_REPORTING=on": withErrorReportingEnv("on"),
		"ERROR_REPORTING=1":  withErrorReportingEnv("1"),
		"TELEMETRY=off":      withTelemetryEnv("off"),
	} {
		t.Run(name, func(t *testing.T) {
			reportingtest.Install(t)
			a := newAppWithAdmin(t, option)

			assert.Equal(t, false, a.errorReporting(t)["forced_off"])
		})
	}
}

// The PUT body keeps a pointer, so the server can tell a missing or null enabled from false, but
// the schema promises a plain boolean: the server answers a null with 400.
func TestErrorReportingAPI_SchemaTakesNoNull(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppWithAdmin(t)
	doc, err := jsonschema.UnmarshalJSON(a.request(t, http.MethodGet, "/openapi.json", nil).Body)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	schema, err := compiler.Compile("https://peekaping.test/openapi.json#/components/schemas/error_reporting.UpdateDto")
	require.NoError(t, err)

	for _, tc := range []struct {
		body   string
		valid  bool
		status int
	}{
		{`{"enabled":false}`, true, http.StatusOK},
		{`{"enabled":null}`, false, http.StatusBadRequest},
		{`{}`, false, http.StatusBadRequest},
	} {
		t.Run(tc.body, func(t *testing.T) {
			value, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.body))
			require.NoError(t, err)
			if tc.valid {
				assert.NoError(t, schema.Validate(value))
			} else {
				assert.Error(t, schema.Validate(value))
			}
			w := a.request(t, http.MethodPut, errorReportingPath, tc.body, a.asAdmin)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
}

func TestErrorReportingAPI_RejectsInvalidInput(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
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
			w := a.request(t, http.MethodPut, errorReportingPath, body, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	assert.Nil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"), "a rejected request stores no choice")
	assert.False(t, reporting.Enabled())
}

func TestErrorReportingAPI_RequiresCredentials(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppWithAdmin(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := a.request(t, method, errorReportingPath, map[string]any{"enabled": true})
		assert.Equal(t, http.StatusUnauthorized, w.Code, method)
	}
	assert.False(t, reporting.Enabled(), "a request without credentials changes nothing")
	assert.Nil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"))
}

func TestErrorReportingAPI_APIKeyCanReadButNotChangeTheChoice(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	a := newAppWithAdmin(t)

	// An API key reads the state, like the settings routes it shares auth with
	state := errorReportingBody(t, a.request(t, http.MethodGet, errorReportingPath, nil, a.withAPIKey), http.StatusOK)
	assert.Equal(t, true, state["enabled"])

	// The choice is the admin's, so an API key can't opt the install out
	w := a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": false}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Equal(t, true, a.errorReporting(t)["enabled"])
	assert.Nil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"))

	// Nor can it opt the admin out
	a.setErrorReporting(t, true)
	w = a.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": false}, a.withAPIKey)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.True(t, reporting.Enabled())
	assert.Equal(t, map[string]any{"enabled": true, "forced_off": false}, errorReportingBody(t, a.request(t, http.MethodGet, errorReportingPath, nil, a.withAPIKey), http.StatusOK))
}

func TestErrorReportingAPI_SettingsAPICannotReadOrWriteItsKey(t *testing.T) {
	reportingtest.Install(t)
	a := newAppWithAdmin(t)
	a.setErrorReporting(t, true)
	require.NotNil(t, a.storedSetting(t, "ERROR_REPORTING_ENABLED"))

	for _, creds := range []credentials{a.asAdmin, a.withAPIKey} {
		path := "/api/v1/settings/key/ERROR_REPORTING_ENABLED"
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, creds).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPut, path, map[string]any{"value": "false", "type": "string"}, creds).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, path, nil, creds).Code)
	}

	assert.Equal(t, true, a.errorReporting(t)["enabled"], "the settings API did not opt the install out")
	assert.True(t, reporting.Enabled())
}

// AC5: only a panic is reported. A validation error, any other 4xx and a 500 that is not a panic
// send nothing.
func TestErrorReportingAPI_ClientErrorsAndFailedRequestsAreNotReported(t *testing.T) {
	recorder := reportingtest.Install(t)
	a := newAppWithAdmin(t)
	a.setErrorReporting(t, true)
	require.True(t, reporting.Enabled())

	for name, req := range map[string]struct {
		method, path string
		body         any
		creds        []credentials
		want         int
	}{
		"a validation error":          {http.MethodPost, "/api/v1/monitors", map[string]any{"type": "http"}, []credentials{a.asAdmin}, http.StatusBadRequest},
		"a body that is not JSON":     {http.MethodPut, errorReportingPath, "{", []credentials{a.asAdmin}, http.StatusBadRequest},
		"a missing record":            {http.MethodGet, "/api/v1/monitors/no-such-monitor", nil, []credentials{a.asAdmin}, http.StatusNotFound},
		"no credentials":              {http.MethodGet, "/api/v1/monitors", nil, nil, http.StatusUnauthorized},
		"a wrong password":            {http.MethodPost, "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": "Wrong-horse-1"}, nil, http.StatusUnauthorized},
		"a route that does not exist": {http.MethodGet, "/api/v1/no-such-route", nil, []credentials{a.asAdmin}, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, req.method, req.path, req.body, req.creds...)

			assert.GreaterOrEqual(t, w.Code, 400)
			assert.Less(t, w.Code, 500)
			assert.Equal(t, req.want, w.Code, w.Body.String())
		})
	}
	assert.Empty(t, recorder.Events())

	// A 500 that is not a panic: the setting can't be written, and the handler answers with an error
	failing := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(settings shared.SettingService) shared.SettingService {
			return settingsThatCannotBeWritten{settings}
		})
	})
	failing.registerAdmin(t)
	w := failing.request(t, http.MethodPut, errorReportingPath, map[string]any{"enabled": false}, failing.asAdmin)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Empty(t, recorder.Events(), "an error returned is not a crash: only panics are reported")
}

// settingsThatCannotBeWritten is the settings service of a database that rejects writes of the
// error reporting choice
type settingsThatCannotBeWritten struct{ shared.SettingService }

func (s settingsThatCannotBeWritten) SetByKey(ctx context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	if key == "ERROR_REPORTING_ENABLED" {
		return nil, errors.New("database is locked")
	}
	return s.SettingService.SetByKey(ctx, key, entity)
}

// panickingStore is a stale.Store whose every method panics, as a bug in a handler would
type panickingStore struct{ stale.Store }

func (panickingStore) All(context.Context) (map[string]stale.Entry, error) {
	panic("health handler bug")
}

func (panickingStore) Count(context.Context) (int64, error) { panic("health handler bug") }

func (panickingStore) RanAt(context.Context) (*time.Time, error) { panic("health handler bug") }

// AC3, for an HTTP handler: the panic is reported with where it happened, the request is still
// answered with a 500, and the server goes on
func TestErrorReportingAPI_APanicInAHandlerIsReportedAndStillAnswers500(t *testing.T) {
	for name, enabled := range map[string]bool{"on": true, "off": false} {
		t.Run(name, func(t *testing.T) {
			recorder := reportingtest.Install(t)
			a := newAppDecorated(t, func(container *dig.Container) error {
				return container.Decorate(func(stale.Store) stale.Store { return panickingStore{} })
			})
			a.registerAdmin(t)
			reporting.SetEnabled(enabled)

			w := a.request(t, http.MethodGet, "/api/v1/health", nil)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, errorReportingPath, nil, a.asAdmin).Code, "the server goes on")
			if !enabled {
				assert.Empty(t, recorder.Events())
				return
			}
			events := recorder.Events()
			require.Len(t, events, 1)
			assert.Equal(t, "http", events[0].Tags["component"])
			assert.Equal(t, reportingtest.Release, events[0].Release)
			assert.Equal(t, "string", events[0].Exception[0].Type)
			assert.Empty(t, events[0].Exception[0].Value)
			var functions []string
			for _, frame := range events[0].Exception[0].Stacktrace.Frames {
				functions = append(functions, frame.Function)
			}
			assert.Contains(t, functions[len(functions)-1], "panickingStore.", "the frame where it panicked is the last")
		})
	}
}
