package apitest

import (
	"encoding/json"
	"net/http"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspaceport"
	"peekaping/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-170: an owner downloads a workspace's setup as one JSON file (the workspace document)

const exportPath = "/api/v1/workspace/export"

// exportedWorkspace is a workspace with one of everything and every link between them
type exportedWorkspace struct {
	seededWorkspace
	admin credentials

	tagOps, tagWeb, proxy, telegram, webhook, httpMonitor, pushMonitor, window, page string
}

func (a *app) exportAs(t *testing.T, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodGet, exportPath, nil, creds...)
}

func (a *app) exportDocument(t *testing.T, creds ...credentials) workspaceport.Document {
	t.Helper()
	res := a.exportAs(t, creds...)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var doc workspaceport.Document
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &doc), res.Body.String())
	return doc
}

// seedExportable makes the workspace "Acme" with an owner, an admin and a member, and in it: two
// tags, a proxy with a password, a Telegram channel with a bot token and a webhook channel with a
// token in its query and a signing secret, an HTTP monitor behind the proxy and a push monitor
// that both notify the channels and carry the tags, a maintenance window on the HTTP monitor and a
// status page with a custom domain that shows both monitors.
func (a *app) seedExportable(t *testing.T) exportedWorkspace {
	t.Helper()
	w := exportedWorkspace{seededWorkspace: seededWorkspace{id: a.createWorkspace(t, "Acme")}}
	w.owner = a.userIn(t, "acme-owner@example.com", w.id, workspace.RoleOwner)
	w.admin = a.userIn(t, "acme-admin@example.com", w.id, workspace.RoleAdmin)
	w.member = a.userIn(t, "acme-member@example.com", w.id, workspace.RoleMember)

	post := func(path string, body map[string]any) string {
		return decode[created](t, a.request(t, http.MethodPost, path, body, w.owner), http.StatusCreated).ID
	}
	w.tagOps = post("/api/v1/tags", map[string]any{"name": "ops", "color": "#112233", "description": "On call"})
	w.tagWeb = post("/api/v1/tags", tagBody("web"))
	w.proxy = post("/api/v1/proxies", map[string]any{"protocol": "http", "host": "proxy.acme.example", "port": 3128, "auth": true, "username": "proxy-user", "password": "proxy-pass"})
	w.telegram = post("/api/v1/notification-channels", map[string]any{"name": "Telegram", "type": "telegram", "active": true, "config": `{"bot_token":"123456:TELEGRAM-SECRET","chat_id":"-1001"}`})
	w.webhook = post("/api/v1/notification-channels", map[string]any{"name": "Hook", "type": "webhook", "active": true, "is_default": true,
		"config": `{"webhook_url":"https://hooks.example/peekaping?token=QUERY-SECRET&room=ops","webhook_content_type":"json","webhook_signing_secret":"SIGNING-SECRET"}`})

	httpMonitor := with(httpMonitorBody("Site", w.proxy), "notification_ids", []string{w.telegram, w.webhook})
	w.httpMonitor = post(monitorsPath, with(httpMonitor, "tag_ids", []string{w.tagWeb, w.tagOps}))
	w.pushMonitor = post(monitorsPath, with(monitorWithLinksBody("Cron", []string{w.webhook}, []string{w.tagOps}), "push_token", "acme-push-token"))
	w.window = post("/api/v1/maintenances", with(weeklyMaintenanceBody("Weekly", "02:00", "03:00"), "monitor_ids", []string{w.httpMonitor}))
	w.page = a.createStatusPageAs(t, w.owner, statusPage{slug: "acme-status", published: true, monitors: []string{w.httpMonitor, w.pushMonitor}, domains: []string{"status.acme.example", "acme.example.org"}})
	return w
}

func TestWorkspaceExport_AnOwnerDownloadsOneFileWithEveryRecordAndLink(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedExportable(t)
		decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "33", "type": "int"}, w.owner), http.StatusOK)

		res := a.exportAs(t, w.owner)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		assert.Regexp(t, `^attachment; filename="peekaping-workspace-\d{4}-\d{2}-\d{2}\.json"$`, res.Header().Get("Content-Disposition"))
		assert.Contains(t, res.Header().Get("Content-Type"), "application/json")
		assert.Equal(t, "no-store", res.Header().Get("Cache-Control"))
		var doc workspaceport.Document
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &doc))

		assert.Equal(t, "peekaping-workspace", doc.Format)
		assert.Equal(t, 1, doc.Version)
		assert.Equal(t, version.Version, doc.AppVersion)
		assert.True(t, doc.ContainsSecrets)
		assert.False(t, doc.ExportedAt.IsZero())
		assert.Equal(t, "Acme", doc.Workspace.Name)
		assert.Equal(t, workspaceport.Settings{DataRetentionDays: 33, CertExpiryNotifyDays: []int{7, 14, 21}}, doc.Settings)

		description := "On call"
		assert.Equal(t, []workspaceport.Tag{
			{Ref: w.tagOps, Name: "ops", Color: "#112233", Description: &description},
			{Ref: w.tagWeb, Name: "web", Color: "#3B82F6"},
		}, doc.Tags)
		assert.Equal(t, []workspaceport.Proxy{{
			Ref: w.proxy, Protocol: "http", Host: "proxy.acme.example", Port: 3128, Auth: true,
			Username: "proxy-user", Password: "proxy-pass", SensitiveFields: []string{"password"},
		}}, doc.Proxies, "the password is in full, and listed as sensitive")

		require.Len(t, doc.NotificationChannels, 2)
		hook, telegram := doc.NotificationChannels[0], doc.NotificationChannels[1]
		assert.Equal(t, w.webhook, hook.Ref)
		assert.Equal(t, "webhook", hook.Type)
		assert.True(t, hook.IsDefault)
		assert.JSONEq(t, `{"webhook_url":"https://hooks.example/peekaping?token=QUERY-SECRET&room=ops","webhook_content_type":"json","webhook_signing_secret":"SIGNING-SECRET"}`, hook.Config.String(), "secrets in full")
		assert.Equal(t, []string{"webhook_signing_secret", "webhook_url"}, hook.SensitiveFields, "the signing secret, and the URL whose query the API masks")
		assert.Equal(t, w.telegram, telegram.Ref)
		assert.Equal(t, "telegram", telegram.Type)
		assert.True(t, telegram.Active)
		assert.JSONEq(t, `{"bot_token":"123456:TELEGRAM-SECRET","chat_id":"-1001"}`, telegram.Config.String())
		assert.Equal(t, []string{"bot_token"}, telegram.SensitiveFields)

		require.Len(t, doc.Monitors, 2)
		push, site := doc.Monitors[0], doc.Monitors[1]
		assert.Equal(t, w.pushMonitor, push.Ref)
		assert.Equal(t, "push", push.Type)
		assert.Equal(t, "acme-push-token", push.PushToken)
		assert.Empty(t, push.ProxyRef)
		assert.Equal(t, []string{w.webhook}, push.NotificationChannelRefs)
		assert.Equal(t, []string{w.tagOps}, push.TagRefs)
		assert.Equal(t, w.httpMonitor, site.Ref)
		assert.Equal(t, "Site", site.Name)
		assert.Equal(t, "http", site.Type)
		assert.True(t, site.Active)
		assert.Equal(t, [5]int{60, 16, 3, 60, 10}, [5]int{site.Interval, site.Timeout, site.MaxRetries, site.RetryInterval, site.ResendInterval})
		assert.Equal(t, "any", site.LocationRule)
		assert.Nil(t, site.LocationThreshold)
		assert.Empty(t, site.PushToken)
		assert.Equal(t, w.proxy, site.ProxyRef)
		assert.JSONEq(t, `{"url":"https://example.com/health","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`, site.Config.String(), "the config is an object, not a string")
		assert.ElementsMatch(t, []string{w.telegram, w.webhook}, site.NotificationChannelRefs)
		assert.ElementsMatch(t, []string{w.tagOps, w.tagWeb}, site.TagRefs)

		require.Len(t, doc.Maintenances, 1)
		window := doc.Maintenances[0]
		assert.Equal(t, w.window, window.Ref)
		assert.Equal(t, "Weekly", window.Title)
		assert.True(t, window.Active)
		assert.Equal(t, "recurring-weekday", window.Strategy)
		assert.Equal(t, []int{1, 3}, window.Weekdays)
		assert.Equal(t, "02:00", *window.StartTime)
		assert.Equal(t, "03:00", *window.EndTime)
		assert.Equal(t, "UTC", *window.Timezone)
		assert.Equal(t, []string{w.httpMonitor}, window.MonitorRefs)

		require.Len(t, doc.StatusPages, 1)
		page := doc.StatusPages[0]
		assert.Equal(t, w.page, page.Ref)
		assert.Equal(t, "acme-status", page.Slug)
		assert.Equal(t, "Status of acme-status", page.Title)
		assert.True(t, page.Published)
		assert.Equal(t, []string{"acme.example.org", "status.acme.example"}, page.Domains)
		assert.ElementsMatch(t, []string{w.httpMonitor, w.pushMonitor}, page.MonitorRefs)
	})
}

func TestWorkspaceExport_NoRecordOfAnotherWorkspaceIsInTheFile(t *testing.T) {
	a := newAppWithAdmin(t)
	acme := a.seedExportable(t)
	other := a.seedWorkspace(t, "Other", "other")

	body := a.exportAs(t, acme.owner).Body.String()
	for _, id := range append(append(append([]string{}, other.monitors...), other.channels...), other.pages...) {
		assert.NotContains(t, body, id)
	}
	for _, word := range []string{"other site", "other api", "other channel", "other-tag", "other-page", "other.example", "Other"} {
		assert.NotContains(t, body, word)
	}
	theirs := a.exportDocument(t, other.owner)
	assert.Equal(t, "Other", theirs.Workspace.Name)
	assert.Len(t, theirs.Monitors, 2)
	assert.Equal(t, 33, theirs.Settings.DataRetentionDays)
	assert.NotContains(t, a.exportAs(t, other.owner).Body.String(), "Acme")
}

func TestWorkspaceExport_AWorkspaceWithNothingInItExportsEmptyArrays(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createWorkspace(t, "Bare")
	owner := a.userIn(t, "bare-owner@example.com", id, workspace.RoleOwner)

	res := a.exportAs(t, owner)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &raw))
	for _, key := range []string{"tags", "proxies", "notificationChannels", "monitors", "maintenances", "statusPages"} {
		assert.JSONEq(t, "[]", string(raw[key]), key)
	}
	doc := a.exportDocument(t, owner)
	assert.Equal(t, workspaceport.Settings{DataRetentionDays: 180, CertExpiryNotifyDays: []int{7, 14, 21}}, doc.Settings, "a new workspace's own settings")
}

func TestWorkspaceExport_TwoExportsDifferInExportedAtAlone(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedExportable(t)

	first, second := a.exportDocument(t, w.owner), a.exportDocument(t, w.owner)
	first.ExportedAt = second.ExportedAt
	assert.Equal(t, first, second)

	strip := func(body string) string {
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(body), &raw))
		delete(raw, "exportedAt")
		out, err := json.Marshal(raw)
		require.NoError(t, err)
		return string(out)
	}
	assert.Equal(t, strip(a.exportAs(t, w.owner).Body.String()), strip(a.exportAs(t, w.owner).Body.String()), "byte for byte")
}

func TestWorkspaceExport_OnlyAnOwnerDownloadsIt(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedExportable(t)
	key := a.createKeyToken(t, w.owner)

	assert.Equal(t, http.StatusForbidden, a.exportAs(t, w.admin).Code, "an admin")
	assert.Equal(t, http.StatusForbidden, a.exportAs(t, w.member).Code, "a member")
	assert.Equal(t, http.StatusUnauthorized, a.exportAs(t, withHeader("X-API-Key", key)).Code, "an API key, as for the deletion")
	assert.Equal(t, http.StatusUnauthorized, a.exportAs(t).Code, "no sign-in")
	stranger := a.userIn(t, "stranger@example.com", a.createWorkspace(t, "Elsewhere"), workspace.RoleOwner)
	assert.Equal(t, http.StatusNotFound, a.exportAs(t, stranger, inWorkspace(w.id)).Code, "an owner of another workspace names it")
	for _, res := range []*httpRecorder{a.exportAs(t, w.admin), a.exportAs(t, w.member)} {
		assert.NotContains(t, res.Body.String(), "SECRET", "a refusal carries no secret")
	}
}

// createKeyToken makes an API key and returns its token
func (a *app) createKeyToken(t *testing.T, creds ...credentials) string {
	t.Helper()
	return decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "ci"}, creds...), http.StatusCreated).Token
}
