package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-157: DISABLED_MONITOR_TYPES turns monitor types off. The API refuses to create a monitor of
// one or to switch a monitor to one, and a monitor that already has one is kept, listed and
// editable, but nothing checks it.

// withDisabledMonitorTypes sets DISABLED_MONITOR_TYPES, as the environment variable gives it
func withDisabledMonitorTypes(list string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.DisabledMonitorTypes = list }
}

// typedMonitor is the part of a monitor the tests read
type typedMonitor struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Active       bool   `json:"active"`
	TypeDisabled bool   `json:"type_disabled"`
}

// disabledTypeError is the 400 for a monitor type the install turns off
type disabledTypeError struct {
	Message string `json:"message"`
	Error   struct {
		Code string `json:"code"`
		Type string `json:"type"`
	} `json:"error"`
}

func assertTypeDisabledRefusal(t *testing.T, w *httpRecorder, monitorType string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var got disabledTypeError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	assert.Equal(t, "MONITOR_TYPE_DISABLED", got.Error.Code)
	assert.Equal(t, monitorType, got.Error.Type)
	assert.Equal(t, monitorType+" monitors are turned off on this install", got.Message)
}

// storeMonitor writes a monitor straight into the workspace, as one stored while its type was still
// allowed is
func (a *app) storeMonitor(t *testing.T, name, monitorType, monitorConfig, pushToken string) string {
	t.Helper()
	var id string
	require.NoError(t, a.container.Invoke(func(repo monitor.MonitorRepository) error {
		stored, err := repo.Create(context.Background(), a.firstWorkspace(t), &shared.Monitor{
			Type: monitorType, Name: name, Interval: 60, Timeout: 16, RetryInterval: 60, ResendInterval: 10,
			Active: true, Status: shared.MonitorStatusPending, Config: monitorConfig, PushToken: pushToken,
		})
		if err != nil {
			return err
		}
		id = stored.ID
		return nil
	}))
	return id
}

const dockerMonitorConfig = `{"container_id":"web","connection_type":"socket","docker_daemon":"/var/run/docker.sock"}`

func (a *app) typedMonitorOf(t *testing.T, id string) typedMonitor {
	t.Helper()
	return decode[typedMonitor](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK)
}

func TestDisabledMonitorTypes_ACreateOfADisabledTypeIsRefused(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("docker"))

	w := a.request(t, http.MethodPost, monitorsPath, dockerMonitorBody("Containers"), a.asAdmin)

	assertTypeDisabledRefusal(t, w, "docker")
	listed := decode[[]typedMonitor](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	assert.Empty(t, listed, "nothing was stored")
	decode[typedMonitor](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Site", ""), a.asAdmin), http.StatusCreated)
}

func TestDisabledMonitorTypes_AMonitorCannotBeSwitchedToADisabledType(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("docker"))
	id := a.createHTTPMonitor(t, "Site", "")
	path := monitorsPath + "/" + id

	put := a.request(t, http.MethodPut, path, dockerMonitorBody("Site"), a.asAdmin)
	patch := a.request(t, http.MethodPatch, path, map[string]any{"type": "docker", "config": dockerMonitorConfig}, a.asAdmin)

	assertTypeDisabledRefusal(t, put, "docker")
	assertTypeDisabledRefusal(t, patch, "docker")
	got := a.typedMonitorOf(t, id)
	assert.Equal(t, "http", got.Type, "it stays what it was")
	assert.False(t, got.TypeDisabled)
}

func TestDisabledMonitorTypes_AMonitorStoredBeforeIsKeptEditableAndSaysItsTypeIsDisabled(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("docker"))
	id := a.storeMonitor(t, "Containers", "docker", dockerMonitorConfig, "")
	path := monitorsPath + "/" + id

	got := a.typedMonitorOf(t, id)
	assert.True(t, got.TypeDisabled)
	assert.True(t, got.Active, "its own setting is left as it is")
	listed := decode[[]typedMonitor](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].TypeDisabled)
	batch := decode[[]typedMonitor](t, a.request(t, http.MethodGet, monitorsPath+"/batch?ids="+id, nil, a.asAdmin), http.StatusOK)
	require.Len(t, batch, 1)
	assert.True(t, batch[0].TypeDisabled)

	// an edit that keeps the type works, by PATCH and by PUT, and the answer says so too
	renamed := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Containers v2"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Containers v2", renamed.Name)
	assert.True(t, renamed.TypeDisabled)
	retyped := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"type": "docker", "config": dockerMonitorConfig}, a.asAdmin), http.StatusOK)
	assert.True(t, retyped.TypeDisabled, "naming the type it already has is not a change")
	replaced := decode[typedMonitor](t, a.request(t, http.MethodPut, path, with(dockerMonitorBody("Containers v3"), "active", false), a.asAdmin), http.StatusOK)
	assert.Equal(t, "Containers v3", replaced.Name)
	assert.True(t, replaced.TypeDisabled)
	assert.False(t, replaced.Active)

	// and it can leave the type
	moved := decode[typedMonitor](t, a.request(t, http.MethodPatch, path, map[string]any{"type": "tcp", "config": tcpMonitorBody("")["config"]}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "tcp", moved.Type)
	assert.False(t, moved.TypeDisabled)
}

func TestDisabledMonitorTypes_AMonitorOfADisabledTypeIsNotChecked(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("docker"), func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })
	dockerID := a.storeMonitor(t, "Containers", "docker", dockerMonitorConfig, "")
	httpID := a.createHTTPMonitor(t, "Site", "")
	source := a.schedulerSource(t)

	// the scheduler has nothing to check for it
	assert.Equal(t, []string{httpID}, scheduled(t, source))
	found, err := source.Find(context.Background(), dockerID)
	require.NoError(t, err)
	assert.Nil(t, found)
	_, err = source.Load(context.Background(), dockerID)
	assert.ErrorIs(t, err, check.ErrMonitorTypeDisabled)

	// a probe that runs docker is not dealt it
	a.sayHelloWith(t, localToken, "00000000-0000-4000-8000-000000000001", fullBuildTypes)
	dealt := decodeAssignments(t, a.assignments(t, localToken, "00000000-0000-4000-8000-000000000001", ""))
	assert.Equal(t, []string{httpID}, monitorIDs(dealt.Monitors))

	// a result that was on its way is dropped
	require.NoError(t, a.container.Invoke(func(service ingester.Service) error {
		now := time.Now().UTC()
		return service.Ingest(context.Background(), &ingester.Result{
			MonitorID: dockerID, MonitorName: "Containers", MonitorType: "docker", Status: shared.MonitorStatusDown,
			Message: "container is not running", StartTime: now, EndTime: now,
		})
	}))
	assert.Empty(t, a.storedHeartbeats(t, dockerID))
	assert.Equal(t, shared.MonitorStatusPending, shared.MonitorStatus(a.monitorStatus(t, dockerID)), "and its status is not judged")
}

func (a *app) monitorStatus(t *testing.T, id string) int {
	t.Helper()
	var status int
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		found, err := monitors.FindByIDAnyWorkspace(context.Background(), id)
		if err != nil {
			return err
		}
		status = int(found.Status)
		return nil
	}))
	return status
}

func TestDisabledMonitorTypes_APushToAMonitorOfADisabledTypeIsRefused(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("push"))
	id := a.storeMonitor(t, "Cron", "push", `{"pushToken":"cron-token"}`, "cron-token")

	w := a.request(t, http.MethodGet, "/api/v1/push/cron-token?status=up&msg=hello", nil)

	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "turned off")
	assert.Empty(t, a.storedHeartbeats(t, id))
}

func TestDisabledMonitorTypes_MeListsTheTypes(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes(" Docker , snmp,,docker"))

	me := a.meOf(t, a.asAdmin)

	assert.Equal(t, []string{"docker", "snmp"}, me.DisabledMonitorTypes, "trimmed, in lower case and each once")
}

func TestDisabledMonitorTypes_MeListsNoneByDefault(t *testing.T) {
	a := newAppWithAdmin(t, withAllMonitorTypes)

	w := a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"disabledMonitorTypes":[]`, "an empty list and not null")
}

// AC6: with nothing disabled, a Docker monitor is created, runs and reads as before
func TestDisabledMonitorTypes_WithTheVariableUnsetNothingChanges(t *testing.T) {
	a := newAppWithAdmin(t, withAllMonitorTypes, func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })

	made := decode[typedMonitor](t, a.request(t, http.MethodPost, monitorsPath, dockerMonitorBody("Containers"), a.asAdmin), http.StatusCreated)

	assert.False(t, made.TypeDisabled)
	assert.False(t, a.typedMonitorOf(t, made.ID).TypeDisabled)
	source := a.schedulerSource(t)
	assert.Equal(t, []string{made.ID}, scheduled(t, source))
	_, err := source.Load(context.Background(), made.ID)
	assert.NoError(t, err)
	patched := a.request(t, http.MethodPatch, monitorsPath+"/"+made.ID, map[string]any{"type": "tcp", "config": tcpMonitorBody("")["config"]}, a.asAdmin)
	assert.Equal(t, http.StatusOK, patched.Code, patched.Body.String())
}

func TestDisabledMonitorTypes_APublicStatusPageShowsAMonitorOfADisabledTypeAsNotActive(t *testing.T) {
	a := newAppWithAdmin(t, withDisabledMonitorTypes("docker"))
	containers := a.storeMonitor(t, "Containers", "docker", dockerMonitorConfig, "")
	site := a.createHTTPMonitor(t, "Site", "")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{containers, site}})

	for _, path := range []string{"/api/v1/status-pages/slug/shop/monitors", "/api/v1/status-pages/slug/shop/monitors/homepage"} {
		t.Run(path, func(t *testing.T) {
			monitors := decode[[]typedMonitor](t, a.request(t, http.MethodGet, path, nil), http.StatusOK)
			active := map[string]bool{}
			for _, m := range monitors {
				active[m.ID] = m.Active
			}
			assert.Equal(t, map[string]bool{containers: false, site: true}, active, "nothing checks the docker monitor, so the page shows it paused")
		})
	}
	assert.True(t, a.typedMonitorOf(t, containers).Active, "its own setting is left as it is")
}
