package apitest

import (
	"net/http"
	"slices"
	"testing"

	"peekaping/internal/modules/events"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type monitorWithProbes struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	ProbeIDs []string `json:"probe_ids"`
}

const monitorsPath = "/api/v1/monitors"

// tcpMonitorBody is a TCP monitor, a type a probe may not run
func tcpMonitorBody(name string) map[string]any {
	body := httpMonitorBody(name, "")
	body["type"], body["config"] = "tcp", `{"host":"db.example.com","port":5432}`
	return body
}

// onProbes is a monitor body that runs on the given probes
func onProbes(body map[string]any, probeIDs ...string) map[string]any {
	return with(body, "probe_ids", probeIDs)
}

// probeThatRuns is a probe that has said hello, and so runs only the given monitor types
func (a *app) probeThatRuns(t *testing.T, name string, types ...string) probeWithToken {
	t.Helper()
	made := a.createProbe(t, map[string]any{"name": name})
	instanceID := uuid.NewString()
	hello := probeproto.HelloRequest{
		InstanceID: instanceID, ProtocolVersion: probeproto.ProtocolVersion, ProbeVersion: "0.1.0",
		SupportedTypes: types, Hostname: "edge-1.example.com", OS: "linux", Arch: "arm64",
	}
	w := a.request(t, http.MethodPost, helloPath, hello, asProbe(made.Token, instanceID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return made
}

func (a *app) monitorProbes(t *testing.T, id string) []string {
	t.Helper()
	return decode[monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath+"/"+id, nil, a.asAdmin), http.StatusOK).ProbeIDs
}

func TestMonitorProbes_DefaultsToLocal(t *testing.T) {
	a := newAppWithAdmin(t)

	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Checkout", ""), a.asAdmin), http.StatusCreated)

	assert.Equal(t, []string{"local"}, made.ProbeIDs, "the response to the create")
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, made.ID), "and what is stored")
}

func TestMonitorProbes_CreateOnARemoteProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})

	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated)

	assert.Equal(t, []string{frankfurt.ID}, made.ProbeIDs)
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, made.ID))
}

func TestMonitorProbes_AcceptsSeveralProbes(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	paris := a.createProbe(t, map[string]any{"name": "Paris"})
	want := func(ids ...string) []string { slices.Sort(ids); return ids }

	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID), a.asAdmin), http.StatusCreated)
	assert.Equal(t, want("local", frankfurt.ID), made.ProbeIDs, "the response to the create")
	assert.Equal(t, want("local", frankfurt.ID), a.monitorProbes(t, made.ID), "and what is stored")
	path := monitorsPath + "/" + made.ID

	put := decode[monitorWithProbes](t, a.request(t, http.MethodPut, path, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID, paris.ID, "local"), a.asAdmin), http.StatusOK)
	assert.Equal(t, want("local", frankfurt.ID, paris.ID), put.ProbeIDs, "a full update")

	patch := decode[monitorWithProbes](t, a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{paris.ID, frankfurt.ID}}, a.asAdmin), http.StatusOK)
	assert.Equal(t, want(frankfurt.ID, paris.ID), patch.ProbeIDs, "a partial update, which can leave local out")
}

func TestMonitorProbes_RejectsDuplicatesAndNone(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Checkout", ""), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	for name, tc := range map[string]struct {
		probeIDs []string
		message  string
	}{
		"the same twice":           {[]string{frankfurt.ID, frankfurt.ID}, `Invalid probe_ids: probe "` + frankfurt.ID + `" is listed twice`},
		"local twice among others": {[]string{"local", frankfurt.ID, "local"}, `Invalid probe_ids: probe "local" is listed twice`},
		"no probes at all":         {[]string{}, "Invalid probe_ids: a monitor runs on at least one probe"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, c := range []call{
				{"create", http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Search", ""), tc.probeIDs...)},
				{"full update", http.MethodPut, path, onProbes(httpMonitorBody("Checkout", ""), tc.probeIDs...)},
				{"partial update", http.MethodPatch, path, map[string]any{"probe_ids": tc.probeIDs}},
			} {
				w := a.request(t, c.method, c.path, c.body, a.asAdmin)

				assert.Equal(t, http.StatusBadRequest, w.Code, c.name+": "+w.Body.String())
				assert.Equal(t, tc.message, errorMessage(t, w.Body.Bytes()), c.name)
			}
		})
	}
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, id), "the monitor stays where it was")
	assert.Len(t, decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK), 1, "and no monitor is created")
}

func TestMonitorProbes_EveryProbeMustRunTheType(t *testing.T) {
	a := newAppWithAdmin(t)
	tcpOnly := a.probeThatRuns(t, "Paris", "tcp")
	httpOnly := a.probeThatRuns(t, "Oslo", "http")

	w := a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), httpOnly.ID, tcpOnly.ID, "local"), a.asAdmin)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, `Invalid probe_ids: probe "Paris" doesn't run http monitors`, errorMessage(t, w.Body.Bytes()))
	assert.Empty(t, decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK), "no monitor is created")

	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), httpOnly.ID, "local"), a.asAdmin), http.StatusCreated).ID
	patch := a.request(t, http.MethodPatch, monitorsPath+"/"+id, map[string]any{"probe_ids": []string{httpOnly.ID, tcpOnly.ID}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, patch.Code, patch.Body.String())
	assert.Equal(t, `Invalid probe_ids: probe "Paris" doesn't run http monitors`, errorMessage(t, patch.Body.Bytes()))
}

func TestMonitorProbes_RejectsUnknownProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Checkout", ""), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	for _, tc := range []call{
		{"create", http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Search", ""), "no-such-probe")},
		{"full update", http.MethodPut, path, onProbes(httpMonitorBody("Checkout", ""), "no-such-probe")},
		{"partial update", http.MethodPatch, path, map[string]any{"probe_ids": []string{"no-such-probe"}}},
	} {
		w := a.request(t, tc.method, tc.path, tc.body, a.asAdmin)

		assert.Equal(t, http.StatusBadRequest, w.Code, tc.name+": "+w.Body.String())
		assert.Equal(t, `Invalid probe_ids: probe "no-such-probe" doesn't exist`, errorMessage(t, w.Body.Bytes()), tc.name)
	}
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, id))
}

func TestMonitorProbes_RejectsUnsupportedType(t *testing.T) {
	a := newAppWithAdmin(t)
	tcpOnly := a.probeThatRuns(t, "Paris", "tcp")
	anything := a.createProbe(t, map[string]any{"name": "Fresh"})

	w := a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), tcpOnly.ID), a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, `Invalid probe_ids: probe "Paris" doesn't run http monitors`, errorMessage(t, w.Body.Bytes()))
	assert.Empty(t, decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK), "no monitor is created")

	decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(tcpMonitorBody("Database"), tcpOnly.ID), a.asAdmin), http.StatusCreated)
	decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), anything.ID), a.asAdmin), http.StatusCreated)
}

func TestMonitorProbes_ADisabledProbeCanBeAssigned(t *testing.T) {
	a := newAppWithAdmin(t)
	paris := a.createProbe(t, map[string]any{"name": "Paris"})
	decode[probeResponse](t, a.request(t, http.MethodPut, probesPath+"/"+paris.ID, map[string]any{"disabled": true}, a.asAdmin), http.StatusOK)

	made := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), paris.ID), a.asAdmin), http.StatusCreated)

	assert.Equal(t, []string{paris.ID}, made.ProbeIDs)
}

func TestMonitorProbes_RejectsPushOnRemote(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	pushOnFrankfurt := onProbes(pushMonitorBody("Nightly cron", "nightly-cron-token"), frankfurt.ID)
	pushID := a.createMonitor(t, "Existing cron", "existing-cron-token")
	httpID := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID

	for _, tc := range []call{
		{"create", http.MethodPost, monitorsPath, pushOnFrankfurt},
		{"full update moving it", http.MethodPut, monitorsPath + "/" + pushID, with(pushOnFrankfurt, "push_token", "existing-cron-token")},
		{"partial update moving it", http.MethodPatch, monitorsPath + "/" + pushID, map[string]any{"probe_ids": []string{frankfurt.ID}}},
		{"partial update making it push", http.MethodPatch, monitorsPath + "/" + httpID, map[string]any{"type": "push", "config": `{"pushToken":"x"}`, "push_token": "x"}},
		{"create on local and a remote probe", http.MethodPost, monitorsPath, onProbes(pushMonitorBody("Nightly cron", "nightly-cron-token"), "local", frankfurt.ID)},
	} {
		w := a.request(t, tc.method, tc.path, tc.body, a.asAdmin)

		assert.Equal(t, http.StatusBadRequest, w.Code, tc.name+": "+w.Body.String())
		assert.Equal(t, "Invalid probe_ids: a push monitor runs on the local probe only", errorMessage(t, w.Body.Bytes()), tc.name)
	}
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, pushID))
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, httpID))

	onLocal := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(pushMonitorBody("Local cron", "local-cron-token"), "local"), a.asAdmin), http.StatusCreated)
	assert.Equal(t, []string{"local"}, onLocal.ProbeIDs, "a push monitor can say local itself")
}

// An instance of the local probe says which monitor types it can run, and it leaves push out: pushes
// arrive at the api, so a probe runs no push monitor. That must not stop the web app, which sends
// probe_ids ["local"] for every monitor, from creating a push monitor.
func TestMonitorProbes_APushMonitorOnTheLocalProbeWhoseInstancesLeavePushOut(t *testing.T) {
	a := localProbeApp(t, nil)
	a.sayHello(t, localToken, uuid.NewString())
	httpID := a.createHTTPMonitor(t, "Checkout", "")

	created := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(pushMonitorBody("Nightly cron", "nightly-cron-token"), "local"), a.asAdmin), http.StatusCreated)
	assert.Equal(t, []string{"local"}, created.ProbeIDs)

	w := a.request(t, http.MethodPatch, monitorsPath+"/"+httpID, map[string]any{"type": "push", "config": `{"pushToken":"x"}`, "push_token": "x"}, a.asAdmin)
	assert.Equal(t, http.StatusOK, w.Code, "a monitor becomes a push monitor: "+w.Body.String())

	w = a.request(t, http.MethodPost, monitorsPath, onProbes(with(httpMonitorBody("Containers", ""), "type", "docker"), "local"), a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a type the instances don't run is still refused: "+w.Body.String())
}

func TestMonitorProbes_PatchTypeCheckedAgainstProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	httpOnly := a.probeThatRuns(t, "Paris", "http")
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), httpOnly.ID), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	w := a.request(t, http.MethodPatch, path, map[string]any{"type": "tcp", "config": tcpMonitorBody("")["config"]}, a.asAdmin)

	assert.Equal(t, http.StatusBadRequest, w.Code, "the type alone moves the monitor to a type its probe doesn't run: "+w.Body.String())
	assert.Equal(t, `Invalid probe_ids: probe "Paris" doesn't run tcp monitors`, errorMessage(t, w.Body.Bytes()))
	got := decode[monitorWithProbes](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "http", got.Type, "so it stays http")

	renamed := a.request(t, http.MethodPatch, path, map[string]any{"name": "Checkout API"}, a.asAdmin)
	assert.Equal(t, http.StatusOK, renamed.Code, "a change that leaves the type alone isn't checked: "+renamed.Body.String())

	// The probe moves with the type in one request
	local := a.request(t, http.MethodPatch, path, map[string]any{"type": "tcp", "config": tcpMonitorBody("")["config"], "probe_ids": []string{"local"}}, a.asAdmin)
	assert.Equal(t, http.StatusOK, local.Code, local.Body.String())
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, id))
}

func TestMonitorProbes_PutTypeCheckedAgainstProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	httpOnly := a.probeThatRuns(t, "Paris", "http")
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), httpOnly.ID), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	// A full update without probe_ids that only changes the type
	w := a.request(t, http.MethodPut, path, tcpMonitorBody("Checkout"), a.asAdmin)

	assert.Equal(t, http.StatusBadRequest, w.Code, "the type alone moves the monitor to a type its probe doesn't run: "+w.Body.String())
	assert.Equal(t, `Invalid probe_ids: probe "Paris" doesn't run tcp monitors`, errorMessage(t, w.Body.Bytes()))
	got := decode[monitorWithProbes](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "http", got.Type, "so it stays http")
	assert.Equal(t, []string{httpOnly.ID}, got.ProbeIDs, "on its probe")

	kept := a.request(t, http.MethodPut, path, httpMonitorBody("Checkout, renamed", ""), a.asAdmin)
	assert.Equal(t, http.StatusOK, kept.Code, "a full update that keeps the type is fine: "+kept.Body.String())

	// The probe moves with the type in one request
	local := a.request(t, http.MethodPut, path, onProbes(tcpMonitorBody("Checkout"), "local"), a.asAdmin)
	assert.Equal(t, http.StatusOK, local.Code, local.Body.String())
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, id))
}

func TestMonitorProbes_PutWithoutProbeIDsKeepsProbe(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	paris := a.createProbe(t, map[string]any{"name": "Paris"})
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id

	put := decode[monitorWithProbes](t, a.request(t, http.MethodPut, path, httpMonitorBody("Checkout, renamed", ""), a.asAdmin), http.StatusOK)
	assert.Equal(t, "Checkout, renamed", put.Name)
	assert.Equal(t, []string{frankfurt.ID}, put.ProbeIDs, "a full update without probe_ids keeps the probe")
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, id))

	patch := decode[monitorWithProbes](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Checkout, again"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{frankfurt.ID}, patch.ProbeIDs, "and so does a partial update")

	nulled := decode[monitorWithProbes](t, a.request(t, http.MethodPatch, path, `{"name":"Checkout, null","probe_ids":null}`, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{frankfurt.ID}, nulled.ProbeIDs, "a null is no more than leaving it out")

	moved := decode[monitorWithProbes](t, a.request(t, http.MethodPut, path, onProbes(httpMonitorBody("Checkout", ""), paris.ID), a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{paris.ID}, moved.ProbeIDs, "a full update with probe_ids moves it")
	assert.Equal(t, []string{paris.ID}, a.monitorProbes(t, id))
	back := decode[monitorWithProbes](t, a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{"local"}, back.ProbeIDs, "and so does a partial update")
}

// An instance says which types it runs each time it says hello, and a probe runs the types its
// online instances run. A monitor of a type the probe no longer runs is not dealt, but the admin can
// still rename, pause or resave it as it is, with the probe_ids the web app read.
func TestMonitorProbes_AMonitorWhoseProbeStoppedRunningItsTypeCanStillBeEdited(t *testing.T) {
	a := newAppWithAdmin(t)
	paris := a.probeThatRuns(t, "Paris", "http")
	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), paris.ID), a.asAdmin), http.StatusCreated).ID
	path := monitorsPath + "/" + id
	// The probe's only instance says hello again, now with another build's types
	instanceID := a.getProbe(t, paris.ID).Instances[0].ID
	hello := helloFrom(instanceID)
	hello.SupportedTypes = []string{"dns"}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, helloPath, hello, asProbe(paris.Token, instanceID)).Code)

	renamed := a.request(t, http.MethodPut, path, onProbes(httpMonitorBody("Checkout, renamed", ""), paris.ID), a.asAdmin)
	assert.Equal(t, http.StatusOK, renamed.Code, renamed.Body.String())
	paused := a.request(t, http.MethodPatch, path, map[string]any{"active": false, "probe_ids": []string{paris.ID}}, a.asAdmin)
	assert.Equal(t, http.StatusOK, paused.Code, paused.Body.String())

	moved := a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin)
	assert.Equal(t, http.StatusOK, moved.Code, "and it can be moved to a probe that runs it: "+moved.Body.String())
	back := a.request(t, http.MethodPatch, path, map[string]any{"probe_ids": []string{paris.ID}}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, back.Code, "but not back: "+back.Body.String())
}

func TestMonitorProbes_ListAndGetCarryProbeIDs(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	remoteID := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Remote", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	localID := a.createMonitor(t, "Local", "local-token")

	byID := func(monitors []monitorWithProbes) map[string][]string {
		got := map[string][]string{}
		for _, m := range monitors {
			got[m.ID] = m.ProbeIDs
		}
		return got
	}
	want := map[string][]string{remoteID: {frankfurt.ID}, localID: {"local"}}

	assert.Equal(t, want, byID(decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath, nil, a.asAdmin), http.StatusOK)), "the list")
	assert.Equal(t, want, byID(decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath+"/batch?ids="+remoteID+","+localID, nil, a.asAdmin), http.StatusOK)), "the batch")
	assert.Equal(t, []string{frankfurt.ID}, a.monitorProbes(t, remoteID), "one monitor")
	assert.Equal(t, []string{"local"}, a.monitorProbes(t, localID))
}

func TestMonitorProbes_EventsCarryProbeIDs(t *testing.T) {
	a := newAppWithAdmin(t)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	created := a.watchMonitors(t, "created", events.MonitorCreated)
	updated := a.watchMonitors(t, "updated", events.MonitorUpdated)

	id := decode[monitorWithProbes](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	decode[monitorWithProbes](t, a.request(t, http.MethodPatch, monitorsPath+"/"+id, map[string]any{"probe_ids": []string{"local"}}, a.asAdmin), http.StatusOK)

	created.waitForMonitor(t, "Checkout")
	updated.waitForMonitor(t, "Checkout")
	created.mu.Lock()
	defer created.mu.Unlock()
	updated.mu.Lock()
	defer updated.mu.Unlock()
	require.Len(t, created.seen, 1)
	assert.Equal(t, []string{frankfurt.ID}, created.seen[0].ProbeIDs)
	require.Len(t, updated.seen, 1)
	assert.Equal(t, []string{"local"}, updated.seen[0].ProbeIDs, "the update event has the probe the monitor was moved to")
}
