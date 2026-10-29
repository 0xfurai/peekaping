package apitest

import (
	"net/http"
	"slices"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-371: the default probes. An instance admin marks the probes that a monitor created without
// choosing probes runs on.

// makeDefault marks the probe as a default (or not) as the instance admin
func (a *app) makeDefault(t *testing.T, probeID string, isDefault bool) {
	t.Helper()
	w := a.request(t, http.MethodPut, probesPath+"/"+probeID, map[string]any{"is_default": isDefault}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// createdOn creates a monitor with the body, which names no probes unless it says so, and returns
// the probes it runs on, sorted
func (a *app) createdOn(t *testing.T, body map[string]any) []string {
	t.Helper()
	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin), http.StatusCreated)
	stored := a.monitorProbes(t, made.ID)
	assert.Equal(t, sorted(made.ProbeIDs), sorted(stored), "the create answers with what is stored")
	return sorted(stored)
}

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func TestDefaultProbes_TheLocalProbeIsTheDefaultOnAnInstallAndNoOtherProbeIs(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)

		assert.True(t, a.getProbe(t, "local").IsDefault, "the migration marks the local probe")
		assert.False(t, a.createProbe(t, map[string]any{"name": "Frankfurt"}).IsDefault, "a new probe is not a default")
		for _, p := range decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK) {
			assert.Equal(t, p.ID == "local", p.IsDefault, p.Name)
		}
	})
}

func TestDefaultProbes_AMonitorWithoutProbesRunsOnTheDefaultProbes(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
		paris := a.createProbe(t, map[string]any{"name": "Paris"})

		assert.Equal(t, []string{"local"}, a.createdOn(t, httpMonitorBody("Before", "")), "only the local probe is a default")

		a.makeDefault(t, frankfurt.ID, true)
		assert.Equal(t, sorted([]string{"local", frankfurt.ID}), a.createdOn(t, httpMonitorBody("Two defaults", "")), "two default probes: two")

		a.makeDefault(t, paris.ID, true)
		a.makeDefault(t, "local", false)
		assert.Equal(t, sorted([]string{frankfurt.ID, paris.ID}), a.createdOn(t, httpMonitorBody("Remote only", "")), "the local probe is no longer one")

		// choosing probes is still the way to run elsewhere
		assert.Equal(t, []string{"local"}, a.createdOn(t, onProbes(httpMonitorBody("Chosen", ""), "local")))
		w := a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("None chosen", ""), []string{}...), a.asAdmin)
		assert.Equal(t, http.StatusBadRequest, w.Code, "an empty list is not the same as none: a monitor runs on at least one probe")
	})
}

func TestDefaultProbes_ExistingMonitorsAreNotMoved(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	before := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Before", ""), a.asAdmin), http.StatusCreated).ID

	a.makeDefault(t, frankfurt.ID, true)
	a.makeDefault(t, "local", false)

	assert.Equal(t, []string{"local"}, a.monitorProbes(t, before))
	// and an update that names no probes keeps them
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPatch, monitorsPath+"/"+before, map[string]any{"name": "Renamed"}, a.asAdmin).Code)
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, before))
}

func TestDefaultProbes_ADisabledDefaultIsSkipped(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	paris := a.createProbe(t, map[string]any{"name": "Paris"})
	a.makeDefault(t, frankfurt.ID, true)
	a.makeDefault(t, paris.ID, true)
	a.makeDefault(t, "local", false)
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPut, probesPath+"/"+paris.ID, map[string]any{"disabled": true}, a.asAdmin).Code)

	assert.Equal(t, []string{frankfurt.ID}, a.createdOn(t, httpMonitorBody("Skips Paris", "")))
	assert.True(t, a.getProbe(t, paris.ID).IsDefault, "it stays marked, and counts again once it is enabled")

	require.Equal(t, http.StatusOK, a.request(t, http.MethodPut, probesPath+"/"+paris.ID, map[string]any{"disabled": false}, a.asAdmin).Code)
	assert.Equal(t, sorted([]string{frankfurt.ID, paris.ID}), a.createdOn(t, httpMonitorBody("Both", "")))
}

func TestDefaultProbes_OnlyTheDefaultsThatRunTheMonitorTypeCount(t *testing.T) {
	a := newAppWithAdmin(t)
	httpOnly := a.probeThatRuns(t, "HTTP only", "http")
	tcpToo := a.probeThatRuns(t, "TCP too", "http", "tcp")
	a.makeDefault(t, httpOnly.ID, true)
	a.makeDefault(t, tcpToo.ID, true)
	a.makeDefault(t, "local", false)

	assert.Equal(t, sorted([]string{httpOnly.ID, tcpToo.ID}), a.createdOn(t, httpMonitorBody("HTTP", "")))
	assert.Equal(t, []string{tcpToo.ID}, a.createdOn(t, tcpMonitorBody("TCP")), "the probe that doesn't run TCP is skipped")
}

func TestDefaultProbes_WhenNoDefaultFitsTheMonitorRunsOnTheLocalProbeAsBefore(t *testing.T) {
	a := newAppWithAdmin(t)
	httpOnly := a.probeThatRuns(t, "HTTP only", "http")
	a.makeDefault(t, httpOnly.ID, true)
	a.makeDefault(t, "local", false)

	assert.Equal(t, []string{"local"}, a.createdOn(t, tcpMonitorBody("TCP")), "no default probe runs TCP")
	// a push monitor runs on the local probe alone, and a remote default doesn't take it
	assert.Equal(t, []string{"local"}, a.createdOn(t, pushMonitorBody("Heartbeat", "default-probe-push-token")))

	// no default probe at all is the same
	a.makeDefault(t, httpOnly.ID, false)
	assert.Equal(t, []string{"local"}, a.createdOn(t, httpMonitorBody("HTTP", "")))
}

func TestDefaultProbes_APushMonitorStaysOnTheLocalProbeBesideRemoteDefaults(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	a.makeDefault(t, frankfurt.ID, true)

	assert.Equal(t, []string{"local"}, a.createdOn(t, pushMonitorBody("Heartbeat", "default-probe-push-token")))
}

func TestDefaultProbes_OnlyAnInstanceAdminMarksThemAndEveryoneSeesThem(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	owner := a.userIn(t, "owner@example.com", a.firstWorkspace(t), workspace.RoleOwner)
	path := probesPath + "/" + frankfurt.ID

	body := map[string]any{"is_default": true}
	assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodPut, path, body, owner).Code)
	assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodPut, path, body, a.withAPIKey).Code)
	assert.False(t, a.getProbe(t, frankfurt.ID).IsDefault)

	a.makeDefault(t, frankfurt.ID, true)
	for name, creds := range map[string]credentials{"an owner": owner, "an API key": a.withAPIKey} {
		t.Run(name, func(t *testing.T) {
			seen := decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, creds), http.StatusOK)
			assert.True(t, seen.IsDefault)
			for _, p := range decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, creds), http.StatusOK) {
				assert.Equal(t, p.ID == frankfurt.ID || p.ID == "local", p.IsDefault, p.Name)
			}
		})
	}
	// a workspace owner's monitor without probes runs on the defaults too
	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Owner's", ""), owner), http.StatusCreated)
	assert.Equal(t, sorted([]string{"local", frankfurt.ID}), sorted(made.ProbeIDs))
}

func TestDefaultProbes_ChangingTheFlagLeavesTheRestOfTheProbeAlone(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt", "location": "DE"})

	a.makeDefault(t, frankfurt.ID, true)

	after := a.getProbe(t, frankfurt.ID)
	assert.Equal(t, "Frankfurt", after.Name)
	assert.Equal(t, "DE", after.Location)
	assert.False(t, after.Disabled)
	assert.True(t, after.IsDefault)
}
