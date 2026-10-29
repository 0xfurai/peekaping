package apitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The notification channels, proxies, tags and maintenance windows of an install belong to a
// workspace (PP-149, docs/adr/0005 sections 2, 3 and 8), and so do its status pages (PP-150).

const missingRecordID = "00000000-0000-4000-8000-000000000000"

// recordModule is one of the kinds of record that belong to a workspace
type recordModule struct {
	name  string
	table string
	path  string
	// body is a valid body to create one, named by name
	body func(name string) map[string]any
	// routes are the calls on a record that change it or read it, on the record with this id
	routes func(id string) []call
	// shown is a field of the record's JSON that tells it from another
	shown string
}

var recordModules = []recordModule{
	{
		name: "notification channel", table: "notification_channels", path: "/api/v1/notification-channels", shown: "name",
		body: webhookChannelBody,
		routes: func(id string) []call {
			path := "/api/v1/notification-channels/" + id
			return []call{
				{"get", http.MethodGet, path, nil},
				{"update", http.MethodPut, path, webhookChannelBody("Hijacked")},
				{"partial update", http.MethodPatch, path, map[string]any{"name": "Hijacked", "active": false}},
				{"delete", http.MethodDelete, path, nil},
				{"test", http.MethodPost, "/api/v1/notification-channels/test", with(webhookChannelBody("Hijacked"), "id", id)},
			}
		},
	},
	{
		name: "proxy", table: "proxies", path: "/api/v1/proxies", shown: "host",
		body: proxyBody2,
		routes: func(id string) []call {
			path := "/api/v1/proxies/" + id
			return []call{
				{"get", http.MethodGet, path, nil},
				{"update", http.MethodPut, path, proxyBody("hijacked.example")},
				{"partial update", http.MethodPatch, path, map[string]any{"host": "hijacked.example", "port": 1}},
				{"delete", http.MethodDelete, path, nil},
			}
		},
	},
	{
		name: "tag", table: "tags", path: "/api/v1/tags", shown: "name",
		body: tagBody,
		routes: func(id string) []call {
			path := "/api/v1/tags/" + id
			return []call{
				{"get", http.MethodGet, path, nil},
				{"update", http.MethodPut, path, tagBody("hijacked")},
				{"partial update", http.MethodPatch, path, map[string]any{"name": "hijacked"}},
				{"delete", http.MethodDelete, path, nil},
			}
		},
	},
	{
		name: "status page", table: "status_pages", path: "/api/v1/status-pages", shown: "title",
		body: statusPageBody,
		routes: func(id string) []call {
			path := "/api/v1/status-pages/" + id
			return []call{
				{"get", http.MethodGet, path, nil},
				{"update", http.MethodPatch, path, map[string]any{"title": "Hijacked", "published": false}},
				{"update the monitors", http.MethodPatch, path, map[string]any{"monitor_ids": []string{}}},
				{"update the domains", http.MethodPatch, path, map[string]any{"domains": []string{"hijacked.example"}}},
				{"delete", http.MethodDelete, path, nil},
			}
		},
	},
	{
		name: "maintenance window", table: "maintenances", path: "/api/v1/maintenances", shown: "title",
		body: maintenanceBody,
		routes: func(id string) []call {
			path := "/api/v1/maintenances/" + id
			return []call{
				{"get", http.MethodGet, path, nil},
				{"update", http.MethodPut, path, maintenanceBody("Hijacked")},
				{"partial update", http.MethodPatch, path, map[string]any{"title": "Hijacked"}},
				{"delete", http.MethodDelete, path, nil},
				{"pause", http.MethodPatch, path + "/pause", nil},
				{"resume", http.MethodPatch, path + "/resume", nil},
			}
		},
	},
}

// statusPageBody is a status page named by name, with a slug made of it
func statusPageBody(name string) map[string]any {
	return map[string]any{"slug": strings.ToLower(strings.ReplaceAll(name, " ", "-")), "title": name, "published": true}
}

// proxyBody2 is a proxy named by its host, in the shape of the other modules' bodies
func proxyBody2(name string) map[string]any { return proxyBody(name + ".example") }

// create makes a record as the credentials and returns its id
func (m recordModule) create(t *testing.T, a *app, name string, creds credentials) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, m.path, m.body(name), creds), http.StatusCreated).ID
}

// workspaceOfRecord is the workspace the database stores the record in
func (a *app) workspaceOfRecord(t *testing.T, table, id string) workspace.ID {
	t.Helper()
	var workspaceID string
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table(table).Column("workspace_id").Where("id = ?", id).Scan(t.Context(), &workspaceID)
	}))
	return workspace.ID(workspaceID)
}

// atPath swaps the id in a call's path, and in its body when it names it, for another id
func (c call) withID(from, to string) call {
	c.path = strings.ReplaceAll(c.path, from, to)
	if body, ok := c.body.(map[string]any); ok {
		if _, named := body["id"]; named {
			c.body = with(body, "id", to)
		}
	}
	return c
}

// A record belongs to the workspace of the request that made it, whatever the body says (AC1)
func TestRecordWorkspaces_ACreateLandsInTheRequestsWorkspaceWhateverTheBodySays(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.firstWorkspace(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleAdmin)

	for _, m := range recordModules {
		t.Run(m.name, func(t *testing.T) {
			for field, value := range map[string]any{
				"workspace_id": string(first),
				"workspaceId":  string(first),
				"workspace":    map[string]any{"id": string(first)},
			} {
				id := decode[created](t, a.request(t, http.MethodPost, m.path, with(m.body("Claims the first "+field), field, value), bob), http.StatusCreated).ID

				assert.Equal(t, second, a.workspaceOfRecord(t, m.table, id), field)
				assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, m.path+"/"+id, nil, bob).Code, field)
				assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, m.path+"/"+id, nil, a.asAdmin).Code, "and not the first workspace's: "+field)
			}
			// and the first workspace's own create lands in the first
			assert.Equal(t, first, a.workspaceOfRecord(t, m.table, m.create(t, a, "Mine", a.asAdmin)))
		})
	}
}

// An update can't move a record to another workspace either (AC1)
func TestRecordWorkspaces_AnUpdateLeavesTheRecordInItsWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.firstWorkspace(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleAdmin)

	for _, m := range recordModules {
		t.Run(m.name, func(t *testing.T) {
			id := m.create(t, a, "Mover", bob)
			path := m.path + "/" + id

			a.request(t, http.MethodPut, path, with(m.body("Moved"), "workspace_id", string(first)), bob)
			a.request(t, http.MethodPatch, path, map[string]any{m.shown: "Moved again", "workspace_id": string(first)}, bob)

			assert.Equal(t, second, a.workspaceOfRecord(t, m.table, id))
		})
	}
}

// A member of workspace B has no way to a record of workspace A: every route answers as it does for
// an ID that doesn't exist, and the record is as it was (AC2)
func TestRecordWorkspaces_AMemberOfAnotherWorkspaceReachesNoRecordOfThisOne(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)

	for _, m := range recordModules {
		t.Run(m.name, func(t *testing.T) {
			id := m.create(t, a, "Alices", a.asAdmin)
			theirs := m.create(t, a, "Bobs", bob)
			before := a.request(t, http.MethodGet, m.path+"/"+id, nil, a.asAdmin).Body.String()
			require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, m.path+"/"+id, nil, a.asAdmin).Code)

			for _, route := range m.routes(id) {
				t.Run(route.name, func(t *testing.T) {
					w := a.request(t, route.method, route.path, route.body, bob)
					unknownCall := route.withID(id, missingRecordID)
					unknown := a.request(t, unknownCall.method, unknownCall.path, unknownCall.body, bob)

					assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
					assert.Equal(t, unknown.Code, w.Code, "the same answer as for a record that doesn't exist")
					// a message may repeat the ID the caller sent, which is the one thing that differs
					assert.JSONEq(t, strings.ReplaceAll(unknown.Body.String(), missingRecordID, "<id>"), strings.ReplaceAll(w.Body.String(), id, "<id>"),
						"with the same body, so the two can't be told apart (ADR 0005 section 8)")
					assert.NotContains(t, w.Body.String(), "Alices", "and nothing of the record")
				})
			}

			assert.JSONEq(t, before, a.request(t, http.MethodGet, m.path+"/"+id, nil, a.asAdmin).Body.String(), "the record is as it was")
			// the lists show each workspace its own
			assert.Equal(t, []string{id}, idsOfList(t, a.request(t, http.MethodGet, m.path+"?limit=100", nil, a.asAdmin)))
			assert.Equal(t, []string{theirs}, idsOfList(t, a.request(t, http.MethodGet, m.path+"?limit=100", nil, bob)))
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, m.path+"/"+theirs, nil, bob).Code, "Bob still reaches his own")
		})
	}
}

func idsOfList(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	items := decode[[]created](t, w, http.StatusOK)
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

// A search doesn't find another workspace's record either (AC2)
func TestRecordWorkspaces_ASearchFindsOnlyTheWorkspacesRecords(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)

	for _, m := range recordModules {
		t.Run(m.name, func(t *testing.T) {
			m.create(t, a, "findable", a.asAdmin)

			assert.Empty(t, idsOfList(t, a.request(t, http.MethodGet, m.path+"?q=findable", nil, bob)))
			assert.Len(t, idsOfList(t, a.request(t, http.MethodGet, m.path+"?q=findable", nil, a.asAdmin)), 1)
		})
	}
}

// The channel test endpoint takes the channel it is told to edit from the request's workspace, and
// sends nothing for another workspace's (AC2)
func TestRecordWorkspaces_TestingAnotherWorkspacesChannelSendsNothing(t *testing.T) {
	a := newSecretsApp(t)
	hook := startRequestReceiver(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	channel := secretChannelOfType(t, "webhook").withVisible(map[string]any{"webhook_url": hook.url + "/hook"})
	id := a.createSecretChannel(t, channel)
	body := map[string]any{"name": "Channel", "type": "webhook", "active": true, "config": channel.withPlaceholders(nil), "id": id}

	w := a.request(t, http.MethodPost, "/api/v1/notification-channels/test", body, bob)
	unknown := a.request(t, http.MethodPost, "/api/v1/notification-channels/test", with(body, "id", missingRecordID), bob)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Equal(t, unknown.Code, w.Code)
	assert.JSONEq(t, unknown.Body.String(), w.Body.String())
	assert.Empty(t, hook.received(), "the stored secrets went nowhere")

	// the owner's own test sends, so the receiver is reachable
	decode[any](t, a.testChannel(t, id, "webhook", channel.withPlaceholders(nil)), http.StatusOK)
	assert.Len(t, hook.received(), 1)
}

// Tag names are unique within a workspace and not across the install (AC3)
func TestRecordWorkspaces_TwoWorkspacesCanEachHaveATagWithTheSameName(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)

	mine := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("production"), a.asAdmin), http.StatusCreated).ID
	theirs := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("production"), bob), http.StatusCreated).ID

	assert.NotEqual(t, mine, theirs)
	// a second one in the same workspace is refused as it always was
	for _, creds := range []credentials{a.asAdmin, bob} {
		w := a.request(t, http.MethodPost, "/api/v1/tags", tagBody("production"), creds)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "Tag with this name already exists")
	}
	// and so is a rename into a name the workspace has, but not into one only the other has
	own := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("staging"), bob), http.StatusCreated).ID
	assert.Equal(t, http.StatusConflict, a.request(t, http.MethodPatch, "/api/v1/tags/"+own, map[string]any{"name": "production"}, bob).Code)
	assert.Equal(t, http.StatusConflict, a.request(t, http.MethodPut, "/api/v1/tags/"+own, tagBody("production"), bob).Code)
	aliceOwn := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("qa"), a.asAdmin), http.StatusCreated).ID
	decode[any](t, a.request(t, http.MethodPatch, "/api/v1/tags/"+aliceOwn, map[string]any{"name": "staging"}, a.asAdmin), http.StatusOK)
}

// A monitor links only to channels, tags and a proxy of its own workspace. Naming one of another
// workspace gets the answer an ID that doesn't exist gets, and nothing is written (AC4)
func TestRecordWorkspaces_AMonitorCannotLinkToAnotherWorkspacesRecords(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	channel, tag := a.createChannel(t, "Alices channel"), a.createTag(t, "Alices tag")
	proxy := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("alices.example"), a.asAdmin), http.StatusCreated).ID

	named := map[string]func(body map[string]any, id string) map[string]any{
		"a notification channel": func(body map[string]any, id string) map[string]any {
			return with(body, "notification_ids", []string{id})
		},
		"a tag":   func(body map[string]any, id string) map[string]any { return with(body, "tag_ids", []string{id}) },
		"a proxy": func(body map[string]any, id string) map[string]any { return with(body, "proxy_id", id) },
	}
	foreign := map[string]string{"a notification channel": channel, "a tag": tag, "a proxy": proxy}
	message := map[string]string{"a notification channel": "Notification channel not found", "a tag": "Tag not found", "a proxy": "Proxy not found"}
	bobsMonitor := decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Bobs", "bobs-token"), bob), http.StatusCreated).ID
	bobsBefore := a.request(t, http.MethodGet, monitorsPath+"/"+bobsMonitor, nil, bob).Body.String()

	for what, link := range named {
		t.Run("create naming "+what, func(t *testing.T) {
			token := "doomed-" + strings.ReplaceAll(what, " ", "-")
			w := a.request(t, http.MethodPost, monitorsPath, link(pushMonitorBody("Doomed", token), foreign[what]), bob)
			unknown := a.request(t, http.MethodPost, monitorsPath, link(pushMonitorBody("Doomed", token), missingRecordID), bob)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), message[what])
			assert.Equal(t, unknown.Code, w.Code)
			assert.JSONEq(t, unknown.Body.String(), w.Body.String())
			assert.Equal(t, []string{bobsMonitor}, monitorIDsOf(t, a.request(t, http.MethodGet, monitorsPath, nil, bob)), "no monitor is stored")
		})
		for _, update := range []struct{ name, method string }{{"full update", http.MethodPut}, {"partial update", http.MethodPatch}} {
			t.Run(update.name+" naming "+what, func(t *testing.T) {
				change := func(id string) *httptest.ResponseRecorder {
					body := link(pushMonitorBody("Bobs", "bobs-token"), id)
					if update.method == http.MethodPatch {
						body = link(map[string]any{"name": "Renamed"}, id)
					}
					return a.request(t, update.method, monitorsPath+"/"+bobsMonitor, body, bob)
				}
				w, unknown := change(foreign[what]), change(missingRecordID)

				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), message[what])
				assert.Equal(t, unknown.Code, w.Code)
				assert.JSONEq(t, unknown.Body.String(), w.Body.String())
				assert.JSONEq(t, bobsBefore, a.request(t, http.MethodGet, monitorsPath+"/"+bobsMonitor, nil, bob).Body.String(), "the monitor and its links are as they were")
			})
		}
	}

	// Alice's own records are still hers to link, and Bob's are his
	decode[created](t, a.request(t, http.MethodPost, monitorsPath, with(with(with(pushMonitorBody("Alices", "alices-token"), "notification_ids", []string{channel}), "tag_ids", []string{tag}), "proxy_id", proxy), a.asAdmin), http.StatusCreated)
	bobsChannel := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("Bobs channel"), bob), http.StatusCreated).ID
	bobsTag := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("Bobs tag"), bob), http.StatusCreated).ID
	bobsProxy := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("bobs.example"), bob), http.StatusCreated).ID
	body := with(with(with(pushMonitorBody("Bobs linked", "bobs-linked-token"), "notification_ids", []string{bobsChannel}), "tag_ids", []string{bobsTag}), "proxy_id", bobsProxy)
	decode[created](t, a.request(t, http.MethodPost, monitorsPath, body, bob), http.StatusCreated)
}

func monitorIDsOf(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	return idsOfList(t, w)
}

// A maintenance window covers only the monitors of its own workspace (AC4)
func TestRecordWorkspaces_AMaintenanceWindowCannotCoverAnotherWorkspacesMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	alices := a.createMonitor(t, "Alices", "alices-token")
	bobs := decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Bobs", "bobs-token"), bob), http.StatusCreated).ID
	window := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Bobs window"), "monitor_ids", []string{bobs}), bob), http.StatusCreated).ID
	before := a.request(t, http.MethodGet, "/api/v1/maintenances/"+window, nil, bob).Body.String()
	require.Contains(t, before, bobs)

	t.Run("create", func(t *testing.T) {
		w := a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Doomed"), "monitor_ids", []string{alices}), bob)
		unknown := a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Doomed"), "monitor_ids", []string{missingRecordID}), bob)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "Monitor not found")
		assert.Equal(t, unknown.Code, w.Code)
		assert.JSONEq(t, unknown.Body.String(), w.Body.String())
		assert.Equal(t, []string{window}, idsOfList(t, a.request(t, http.MethodGet, "/api/v1/maintenances", nil, bob)), "no window is stored")
	})
	for _, update := range []struct {
		name, method string
		body         func(ids []string) map[string]any
	}{
		{"full update", http.MethodPut, func(ids []string) map[string]any { return with(maintenanceBody("Bobs window"), "monitor_ids", ids) }},
		{"partial update", http.MethodPatch, func(ids []string) map[string]any { return map[string]any{"monitor_ids": ids} }},
	} {
		t.Run(update.name, func(t *testing.T) {
			path := "/api/v1/maintenances/" + window
			w := a.request(t, update.method, path, update.body([]string{bobs, alices}), bob)
			unknown := a.request(t, update.method, path, update.body([]string{bobs, missingRecordID}), bob)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "Monitor not found")
			assert.Equal(t, unknown.Code, w.Code)
			assert.JSONEq(t, unknown.Body.String(), w.Body.String())
			assert.JSONEq(t, before, a.request(t, http.MethodGet, path, nil, bob).Body.String(), "the window still covers what it covered")
		})
	}
	// and the other way round: Alice's window can't cover Bob's monitor
	w := a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Alices window"), "monitor_ids", []string{bobs}), a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	// while a window over its own monitors works
	decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Alices window"), "monitor_ids", []string{alices}), a.asAdmin), http.StatusCreated)
}
