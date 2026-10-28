package apitest

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxyOf is the proxy a monitor uses, empty for none
func (a *app) proxyOf(t *testing.T, monitorID string, creds credentials) string {
	t.Helper()
	return decode[struct {
		ProxyID string `json:"proxy_id"`
	}](t, a.request(t, http.MethodGet, monitorsPath+"/"+monitorID, nil, creds), http.StatusOK).ProxyID
}

// Deleting a proxy takes it off the monitors of the workspace that used it, and tells each one it
// changed, so the scheduler loads its config again. Another workspace's monitors are left alone.
func TestRecordWorkspaces_DeletingAProxyTellsTheMonitorsThatUsedIt(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	mine := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("shared.example"), a.asAdmin), http.StatusCreated).ID
	theirs := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("shared.example"), bob), http.StatusCreated).ID
	onProxy := func(name, token, proxy string) map[string]any {
		return with(pushMonitorBody(name, token), "proxy_id", proxy)
	}
	first := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProxy("First", "first-token", mine), a.asAdmin), http.StatusCreated).ID
	other := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProxy("Second", "second-token", mine), a.asAdmin), http.StatusCreated).ID
	bobs := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProxy("Bobs", "bobs-token", theirs), bob), http.StatusCreated).ID
	control := a.createMonitor(t, "Control", "control-token")
	updated := a.watchMonitors(t, "updated", events.MonitorUpdated)
	before := a.request(t, http.MethodGet, monitorsPath+"/"+first, nil, a.asAdmin)
	require.Equal(t, mine, a.proxyOf(t, first, a.asAdmin))

	// Bob can't delete it, and it changes nothing
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, "/api/v1/proxies/"+mine, nil, bob).Code)
	assert.Equal(t, mine, a.proxyOf(t, first, a.asAdmin))

	decode[any](t, a.request(t, http.MethodDelete, "/api/v1/proxies/"+mine, nil, a.asAdmin), http.StatusOK)

	assert.Empty(t, a.proxyOf(t, first, a.asAdmin))
	assert.Empty(t, a.proxyOf(t, other, a.asAdmin))
	assert.Equal(t, theirs, a.proxyOf(t, bobs, bob), "the other workspace's monitor keeps its own proxy, though it has the same host")
	decode[any](t, a.request(t, http.MethodGet, "/api/v1/proxies/"+theirs, nil, bob), http.StatusOK)
	// Delivery is asynchronous: a change that works proves events flow, and once its event has
	// arrived, the proxy's would have too
	decode[any](t, a.request(t, http.MethodPatch, monitorsPath+"/"+control, map[string]any{"interval": 90}, a.asAdmin), http.StatusOK)
	updated.waitForMonitor(t, "Control")
	require.Eventually(t, func() bool {
		names := updated.names()
		return slices.Contains(names, "First") && slices.Contains(names, "Second")
	}, 5*time.Second, 5*time.Millisecond, "each monitor that used the proxy is told it changed")
	assert.ElementsMatch(t, []string{"First", "Second", "Control"}, updated.names(), "and no other is")
	assert.NotEqual(t, before.Body.String(), a.request(t, http.MethodGet, monitorsPath+"/"+first, nil, a.asAdmin).Body.String())
}

// A monitor's notifications go only to the channels linked to it (AC5)
func TestRecordWorkspaces_AMonitorGoingDownNotifiesOnlyItsLinkedChannels(t *testing.T) {
	a := newSecretsApp(t)
	a.takeNotificationsFromTheQueue(t)
	hook := startRequestReceiver(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	channel := func(name, path string, creds credentials) string {
		body := map[string]any{"name": name, "type": "webhook", "active": true, "config": fmt.Sprintf(`{"webhook_url":%q,"webhook_content_type":"json"}`, hook.url+path)}
		return decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", body, creds), http.StatusCreated).ID
	}
	linked := channel("Linked", "/linked", a.asAdmin)
	channel("Same workspace, not linked", "/unlinked", a.asAdmin)
	bobsChannel := channel("Bobs", "/bobs", bob)
	decode[created](t, a.request(t, http.MethodPost, monitorsPath, with(pushMonitorBody("Alices", "alices-token"), "notification_ids", []string{linked}), a.asAdmin), http.StatusCreated)
	decode[created](t, a.request(t, http.MethodPost, monitorsPath, with(pushMonitorBody("Bobs", "bobs-token"), "notification_ids", []string{bobsChannel}), bob), http.StatusCreated)

	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/alices-token?status=down&msg=disk%20full", nil).Code)

	require.Eventually(t, func() bool { return len(hook.received()) >= 1 }, 10*time.Second, 20*time.Millisecond, "the notification is sent")
	time.Sleep(time.Second) // long enough for a send to a channel it shouldn't reach
	var paths []string
	for _, request := range hook.received() {
		paths = append(paths, request.path)
	}
	assert.Equal(t, []string{"/linked"}, paths)
}

// An instance admin links a probe only to the channels of a workspace they are a member of; a
// channel of another is refused as one that doesn't exist (AC5)
func TestRecordWorkspaces_AProbeIsLinkedOnlyToChannelsOfAWorkspaceTheAdminIsIn(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	mine := a.createChannel(t, "Alices")
	theirs := decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", webhookChannelBody("Bobs"), bob), http.StatusCreated).ID
	probe := a.createProbe(t, map[string]any{"name": "Frankfurt", "notification_ids": []string{mine}})
	path := probesPath + "/" + probe.ID

	t.Run("create", func(t *testing.T) {
		w := a.request(t, http.MethodPost, probesPath, map[string]any{"name": "Paris", "notification_ids": []string{theirs}}, a.asAdmin)
		unknown := a.request(t, http.MethodPost, probesPath, map[string]any{"name": "Paris", "notification_ids": []string{missingRecordID}}, a.asAdmin)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Equal(t, unknown.Code, w.Code)
		assert.Equal(t, strings.ReplaceAll(unknown.Body.String(), missingRecordID, "<id>"), strings.ReplaceAll(w.Body.String(), theirs, "<id>"))
		assert.Len(t, decode[[]probeResponse](t, a.request(t, http.MethodGet, probesPath, nil, a.asAdmin), http.StatusOK), 2, "the failed create left no probe (the local one and Frankfurt)")
	})
	t.Run("update", func(t *testing.T) {
		w := a.request(t, http.MethodPut, path, map[string]any{"name": "Renamed", "notification_ids": []string{mine, theirs}}, a.asAdmin)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		unchanged := decode[probeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
		assert.Equal(t, "Frankfurt", unchanged.Name)
		assert.Equal(t, []string{mine}, unchanged.NotificationIDs)
	})
	t.Run("an API key links its own workspace's channels only", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{theirs}}, a.withAPIKey).Code)
		decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{mine}}, a.withAPIKey), http.StatusOK)
	})
	t.Run("a member of both workspaces links either", func(t *testing.T) {
		a.addMember(t, second, a.userID(t, adminEmail), workspace.RoleMember)

		linked := decode[probeResponse](t, a.request(t, http.MethodPut, path, map[string]any{"notification_ids": []string{mine, theirs}}, a.asAdmin), http.StatusOK)

		assert.ElementsMatch(t, []string{mine, theirs}, linked.NotificationIDs)
	})
}
