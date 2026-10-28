package apitest

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"peekaping/internal/modules/workspace"
	"peekaping/internal/probeproto"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// monitorView is what a monitor's GET answers, as far as these tests look
type monitorView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Active    bool      `json:"active"`
	Status    int       `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// workspaceOf is the workspace the database stores the monitor in
func (a *app) workspaceOf(t *testing.T, monitorID string) workspace.ID {
	t.Helper()
	var id string
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("monitors").Column("workspace_id").Where("id = ?", monitorID).Scan(t.Context(), &id)
	}))
	return workspace.ID(id)
}

func viewIDs(monitors []monitorView) []string {
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

// A monitor belongs to the workspace of the request that made it, whatever the body says
func TestMonitorWorkspace_ACreateLandsInTheRequestsWorkspaceWhateverTheBodySays(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.firstWorkspace(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleAdmin)

	for name, body := range map[string]map[string]any{
		"workspace_id":  with(pushMonitorBody("Claims the first", "claim-1"), "workspace_id", string(first)),
		"workspaceId":   with(pushMonitorBody("Claims the first too", "claim-2"), "workspaceId", string(first)),
		"workspace":     with(pushMonitorBody("Claims it as an object", "claim-3"), "workspace", map[string]any{"id": string(first)}),
		"no such field": pushMonitorBody("Says nothing", "claim-4"),
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodPost, "/api/v1/monitors", body, bob)
			id := decode[monitorView](t, w, http.StatusCreated).ID

			assert.Equal(t, second, a.workspaceOf(t, id), "the workspace of the request")
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, bob).Code)
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin).Code, "and not the one the body names")
		})
	}
}

// An update can't move a monitor to another workspace either
func TestMonitorWorkspace_AnUpdateLeavesTheMonitorInItsWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.firstWorkspace(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleAdmin)
	id := decode[monitorView](t, a.request(t, http.MethodPost, "/api/v1/monitors", pushMonitorBody("Mover", "mover"), bob), http.StatusCreated).ID

	put := with(pushMonitorBody("Moved", "mover"), "workspace_id", string(first))
	decode[monitorView](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+id, put, bob), http.StatusOK)
	decode[monitorView](t, a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, map[string]any{"name": "Moved again", "workspace_id": string(first)}, bob), http.StatusOK)

	assert.Equal(t, second, a.workspaceOf(t, id))
}

// A member of workspace B has no way to a monitor of workspace A: every route answers 404 as it does
// for an ID that doesn't exist, and the monitor is as it was
func TestMonitorWorkspace_AMemberOfAnotherWorkspaceReachesNoMonitorOfThisOne(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	channelID, tagID := a.createChannel(t, "Ops webhook"), a.createTag(t, "Production")
	id := a.createMonitorWithHistory(t, "Checkout", channelID, tagID)
	path := "/api/v1/monitors/" + id
	before := decode[monitorView](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	rowsBefore := a.monitorRows(t, id)
	require.Equal(t, 1, rowsBefore.Heartbeats)
	require.Equal(t, 3, rowsBefore.Stats)
	heartbeatsBefore := decode[[]map[string]any](t, a.request(t, http.MethodGet, path+"/heartbeats", nil, a.asAdmin), http.StatusOK)
	require.Len(t, heartbeatsBefore, 1)

	since := url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	routes := []call{
		{"get", http.MethodGet, path, nil},
		{"update", http.MethodPut, path, with(pushMonitorBody("Hijacked", "hijack"), "active", false)},
		{"partial update", http.MethodPatch, path, map[string]any{"name": "Hijacked", "active": false}},
		{"pause", http.MethodPatch, path, map[string]any{"active": false}},
		{"delete", http.MethodDelete, path, nil},
		{"reset", http.MethodPost, path + "/reset", nil},
		{"heartbeats", http.MethodGet, path + "/heartbeats", nil},
		{"important heartbeats", http.MethodGet, path + "/heartbeats?important=true&reverse=true", nil},
		{"uptime stats", http.MethodGet, path + "/stats/uptime", nil},
		{"stat points", http.MethodGet, path + "/stats/points?granularity=hour&since=" + since, nil},
		{"TLS info", http.MethodGet, path + "/tls", nil},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, bob)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			unknown := a.request(t, route.method, "/api/v1/monitors/00000000-0000-4000-8000-000000000000"+route.path[len(path):], route.body, bob)
			assert.Equal(t, unknown.Code, w.Code, "the same answer as for a monitor that doesn't exist")
			assert.NotContains(t, w.Body.String(), "Checkout", "and nothing of the monitor")
		})
	}

	after := decode[monitorView](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, before, after, "name, active state, status and updated_at are as they were")
	assert.Equal(t, "Checkout", after.Name)
	assert.True(t, after.Active)
	assert.Equal(t, rowsBefore, a.monitorRows(t, id), "with its heartbeats, stats and links")
	assert.Equal(t, heartbeatsBefore, decode[[]map[string]any](t, a.request(t, http.MethodGet, path+"/heartbeats", nil, a.asAdmin), http.StatusOK))
}

func TestMonitorWorkspace_ListsAndBatchGetShowOnlyTheWorkspacesMonitors(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	mine := a.createMonitor(t, "Alice's", "alice-token")
	theirs := decode[monitorView](t, a.request(t, http.MethodPost, "/api/v1/monitors", pushMonitorBody("Bob's", "bob-token"), bob), http.StatusCreated).ID

	list := func(creds credentials, query string) []string {
		return viewIDs(decode[[]monitorView](t, a.request(t, http.MethodGet, "/api/v1/monitors"+query, nil, creds), http.StatusOK))
	}
	assert.Equal(t, []string{mine}, list(a.asAdmin, ""))
	assert.Equal(t, []string{theirs}, list(bob, ""))
	assert.Empty(t, list(bob, "?q=alice"), "a search doesn't find it")
	assert.Equal(t, []string{theirs}, list(bob, "?active=true&limit=1000"))

	batch := "?ids=" + mine + "," + theirs
	assert.ElementsMatch(t, []string{mine}, list(a.asAdmin, "/batch"+batch), "as Alice")
	assert.ElementsMatch(t, []string{theirs}, list(bob, "/batch"+batch), "as Bob")
	assert.Empty(t, list(bob, "/batch?ids="+mine))

	// the same user in both workspaces sees each one's monitors where they ask for them
	a.addMember(t, second, a.userID(t, adminEmail), workspace.RoleAdmin)
	assert.Equal(t, []string{theirs}, list(func(r *http.Request) { a.asAdmin(r); inWorkspace(second)(r) }, ""))
	assert.Equal(t, []string{mine}, list(func(r *http.Request) { a.asAdmin(r); inWorkspace(a.firstWorkspace(t))(r) }, ""))
}

// A probe belongs to the install, so it is dealt the monitors of every workspace, and the results it
// sends are stored for the monitor that was checked, whichever workspace it is in
func TestMonitorWorkspace_AProbeIsDealtAndAnsweredForTheMonitorsOfEveryWorkspace(t *testing.T) {
	a := localProbeApp(t, nil)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
	inFirst := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Alice's", ""), "local", frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	inSecond := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Bob's", ""), "local", frankfurt.ID), bob), http.StatusCreated).ID
	require.NotEqual(t, a.workspaceOf(t, inFirst), a.workspaceOf(t, inSecond))
	local, remote := uuid.NewString(), uuid.NewString()
	a.sayHello(t, localToken, local)
	a.sayHello(t, frankfurt.Token, remote)

	for name, who := range map[string]struct{ token, instance string }{"the local probe": {localToken, local}, "a remote probe": {frankfurt.Token, remote}} {
		dealt := decodeAssignments(t, a.assignments(t, who.token, who.instance, ""))
		assert.ElementsMatch(t, []string{inFirst, inSecond}, monitorIDs(dealt.Monitors), name)
	}

	got := a.outcomesOf(t, frankfurt.Token, remote, checkResult("first", inFirst, checkedAt(0)), checkResult("second", inSecond, checkedAt(1)))
	assert.Equal(t, []probeproto.ResultOutcome{{ID: "first", Accepted: true}, {ID: "second", Accepted: true}}, got)
	assert.Len(t, a.storedHeartbeats(t, inFirst), 1)
	assert.Len(t, a.storedHeartbeats(t, inSecond), 1)
	// and each workspace sees its own monitor's heartbeat only
	assert.Len(t, decode[[]map[string]any](t, a.request(t, http.MethodGet, monitorsPath+"/"+inSecond+"/heartbeats", nil, bob), http.StatusOK), 1)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, monitorsPath+"/"+inFirst+"/heartbeats", nil, bob).Code)
}
