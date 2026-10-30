package apitest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/limits"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspaceport"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// PP-170: an owner imports a workspace document into a workspace, in one transaction. What the
// workspace or the install can't take as it is gets changed or left out, and is listed in the answer.

const importPath = "/api/v1/workspace/import"

// importTarget is a workspace with an owner and nothing else, for a file to be imported into
type importTarget struct {
	id    workspace.ID
	owner credentials
}

func (a *app) emptyWorkspace(t *testing.T, name, email string) importTarget {
	t.Helper()
	id := a.createWorkspace(t, name)
	return importTarget{id: id, owner: a.userIn(t, email, id, workspace.RoleOwner)}
}

func (a *app) importAs(t *testing.T, doc any, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, importPath, doc, creds...)
}

// imported imports the document and returns the answer, which is the report itself and not the API's envelope
func (a *app) imported(t *testing.T, doc any, creds ...credentials) workspaceport.ImportResult {
	t.Helper()
	res := a.importAs(t, doc, creds...)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var result workspaceport.ImportResult
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result), res.Body.String())
	return result
}

// refused imports the document, expects the 400 for an invalid file, and returns its problems
func (a *app) refused(t *testing.T, doc any, creds ...credentials) []workspaceport.Problem {
	t.Helper()
	res := a.importAs(t, doc, creds...)
	require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	var rejected workspaceport.ImportRejectedResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &rejected), res.Body.String())
	assert.Equal(t, "INVALID_WORKSPACE_FILE", rejected.Error.Code)
	return rejected.Error.Problems
}

// docOf is a valid document with nothing in it
func docOf() workspaceport.Document {
	return workspaceport.Document{
		Format: workspaceport.FormatName, Version: workspaceport.Version, ExportedAt: time.Now().UTC(), ContainsSecrets: true,
		Workspace: workspaceport.Workspace{Name: "Source"},
		Tags:      []workspaceport.Tag{}, Proxies: []workspaceport.Proxy{}, NotificationChannels: []workspaceport.NotificationChannel{},
		Monitors: []workspaceport.Monitor{}, Maintenances: []workspaceport.Maintenance{}, StatusPages: []workspaceport.StatusPage{},
	}
}

const httpConfig = `{"url":"https://example.com/health","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`

func docHTTPMonitor(ref, name string) workspaceport.Monitor {
	return workspaceport.Monitor{
		Ref: ref, Name: name, Type: "http", Active: true, Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 60, ResendInterval: 10,
		LocationRule: "any", Config: workspaceport.ConfigJSON(httpConfig), NotificationChannelRefs: []string{}, TagRefs: []string{},
	}
}

func docPushMonitor(ref, name, token string) workspaceport.Monitor {
	m := docHTTPMonitor(ref, name)
	m.Type, m.PushToken = "push", token
	m.Config = workspaceport.ConfigJSON(`{"pushToken":"` + token + `"}`)
	return m
}

func docTag(ref, name string) workspaceport.Tag {
	return workspaceport.Tag{Ref: ref, Name: name, Color: "#3B82F6"}
}

func docPage(ref, slug string, monitors ...string) workspaceport.StatusPage {
	return workspaceport.StatusPage{
		Ref: ref, Slug: slug, Title: "Status of " + slug, Published: true, Domains: []string{}, MonitorRefs: append([]string{}, monitors...),
	}
}

// changeOf finds the change of a kind about a record, and fails the test when there is none
func changeOf(t *testing.T, result workspaceport.ImportResult, kind, ref string) workspaceport.Change {
	t.Helper()
	for _, c := range result.Changes {
		if c.Kind == kind && c.Ref == ref {
			return c
		}
	}
	require.Failf(t, "no such change", "no %s for %q in %+v", kind, ref, result.Changes)
	return workspaceport.Change{}
}

func problemMessages(found []workspaceport.Problem) []string {
	messages := make([]string, len(found))
	for i, p := range found {
		messages[i] = p.Record + "/" + p.Ref + ": " + p.Message
	}
	return messages
}

// assertProblem checks that a record has a problem that starts with the text
func assertProblem(t *testing.T, found []string, record, ref, startsWith string) {
	t.Helper()
	prefix := record + "/" + ref + ": " + startsWith
	assert.True(t, slices.ContainsFunc(found, func(message string) bool { return strings.HasPrefix(message, prefix) }), "no problem starting %q in %v", prefix, found)
}

func TestWorkspaceImport_OnlyAnOwnerImports(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedExportable(t)
	doc := docOf()
	doc.Tags = []workspaceport.Tag{docTag("t1", "imported")}
	key := a.createKeyToken(t, w.owner)

	assert.Equal(t, http.StatusForbidden, a.importAs(t, doc, w.admin).Code, "an admin")
	assert.Equal(t, http.StatusForbidden, a.importAs(t, doc, w.member).Code, "a member")
	assert.Equal(t, http.StatusForbidden, a.importAs(t, "not even JSON", w.member).Code, "the role is checked before the body is read")
	assert.Equal(t, http.StatusUnauthorized, a.importAs(t, doc, withHeader("X-API-Key", key)).Code, "an API key, as for the export")
	assert.Equal(t, http.StatusUnauthorized, a.importAs(t, doc).Code, "no sign-in")
	stranger := a.userIn(t, "stranger@example.com", a.createWorkspace(t, "Elsewhere"), workspace.RoleOwner)
	assert.Equal(t, http.StatusNotFound, a.importAs(t, doc, stranger, inWorkspace(w.id)).Code, "an owner of another workspace names it")
	assert.Len(t, a.exportDocument(t, w.owner).Tags, 2, "nothing was imported")
}

func TestWorkspaceImport_AnEmptyDocumentImportsNothing(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")

	result := a.imported(t, docOf(), target.owner)

	assert.Equal(t, workspaceport.Created{}, result.Created)
	assert.Equal(t, []workspaceport.Change{}, result.Changes, "an empty list, not null")
}

func TestWorkspaceImport_AFileWithProblemsWritesNothingAndListsThemAll(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	before := a.footprint(t, seededWorkspace{id: target.id})
	published := a.watchMonitors(t, "created", events.MonitorCreated)

	broken := docHTTPMonitor("m-bad", "Broken")
	broken.Config = workspaceport.ConfigJSON(`{"url":"not a url","method":"GET"}`)
	doc := docOf()
	doc.Format, doc.Version = "something-else", workspaceport.Version+1
	doc.Tags = []workspaceport.Tag{docTag("t1", "web"), docTag("t1", "ops")}
	doc.Monitors = []workspaceport.Monitor{docHTTPMonitor("m1", "Good"), broken, {Ref: "", Name: "No ref"}}
	doc.Monitors[0].TagRefs = []string{"t-missing"}
	doc.Maintenances = []workspaceport.Maintenance{{Ref: "w1", Title: "Window", Strategy: "recurring-weekday", MonitorRefs: []string{"m-missing"}}}
	doc.StatusPages = []workspaceport.StatusPage{docPage("p1", "x")}

	found := problemMessages(a.refused(t, doc, target.owner))

	assert.Contains(t, found, `file/: format is "something-else", not "peekaping-workspace"`)
	assert.Contains(t, found, "file/: version 2 is not one this server reads (1 to 1)")
	assert.Contains(t, found, "tag/t1: ref is used by more than one tag")
	assert.Contains(t, found, `monitor/m1: links to tag "t-missing", which is not in the file`)
	assert.Contains(t, found, "monitor/: monitor 3 has no ref")
	assert.Contains(t, found, `maintenance/w1: links to monitor "m-missing", which is not in the file`)
	assert.Contains(t, found, "statusPage/p1: Slug fails min=3")
	assertProblem(t, found, "monitor", "m-bad", "")
	assert.Equal(t, before, a.footprint(t, seededWorkspace{id: target.id}), "nothing was written")
	a.createMonitor(t, "Control", "control-token")
	published.waitForMonitor(t, "Control")
	assert.Equal(t, []string{"Control"}, published.names(), "and no monitor was announced")
}

// monitorNamed finds a monitor of an exported document by name
func monitorNamed(t *testing.T, doc workspaceport.Document, name string) workspaceport.Monitor {
	t.Helper()
	for _, m := range doc.Monitors {
		if m.Name == name {
			return m
		}
	}
	require.Failf(t, "no such monitor", "no monitor %q in %d monitors", name, len(doc.Monitors))
	return workspaceport.Monitor{}
}

func TestWorkspaceImport_AddsEveryRecordAndLinkItHolds(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withBlockPrivateTargets(false))
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		doc.Settings = workspaceport.Settings{DataRetentionDays: 33, CertExpiryNotifyDays: []int{3, 30}}
		doc.Tags = []workspaceport.Tag{docTag("t-ops", "ops"), docTag("t-web", "web")}
		doc.Proxies = []workspaceport.Proxy{{Ref: "px", Protocol: "http", Host: "proxy.example", Port: 3128, Auth: true, Username: "user", Password: "pass", SensitiveFields: []string{"password"}}}
		doc.NotificationChannels = []workspaceport.NotificationChannel{{
			Ref: "c-hook", Name: "Hook", Type: "webhook", Active: true, IsDefault: true,
			Config:          workspaceport.ConfigJSON(`{"webhook_url":"https://hooks.example/p?token=QUERY-SECRET","webhook_content_type":"json","webhook_signing_secret":"SIGNING-SECRET"}`),
			SensitiveFields: []string{"webhook_signing_secret", "webhook_url"},
		}}
		site := docHTTPMonitor("m-site", "Site")
		site.ProxyRef, site.NotificationChannelRefs, site.TagRefs = "px", []string{"c-hook"}, []string{"t-ops", "t-web"}
		cron := docPushMonitor("m-cron", "Cron", "cron-token")
		doc.Monitors = []workspaceport.Monitor{site, cron}
		doc.Maintenances = []workspaceport.Maintenance{{
			Ref: "w1", Title: "Weekly", Active: true, Strategy: "recurring-weekday", StartTime: new("02:00"), EndTime: new("03:00"),
			Weekdays: []int{1, 3}, DaysOfMonth: []int{}, Timezone: new("UTC"), MonitorRefs: []string{"m-site"},
		}}
		page := docPage("p1", "target-status", "m-cron", "m-site")
		page.Domains = []string{"Status.Target.Example "}
		doc.StatusPages = []workspaceport.StatusPage{page}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, workspaceport.Created{Tags: 2, Proxies: 1, NotificationChannels: 1, Monitors: 2, Maintenances: 1, StatusPages: 1}, result.Created)
		assert.Empty(t, result.Changes)
		stored := a.exportDocument(t, target.owner)
		assert.Equal(t, workspaceport.Settings{DataRetentionDays: 33, CertExpiryNotifyDays: []int{3, 30}}, stored.Settings)
		assert.Equal(t, "Target", stored.Workspace.Name, "the workspace keeps its name")
		require.Len(t, stored.Tags, 2)
		require.Len(t, stored.Proxies, 1)
		assert.Equal(t, "pass", stored.Proxies[0].Password, "a saved secret comes in full")
		require.Len(t, stored.NotificationChannels, 1)
		assert.JSONEq(t, string(doc.NotificationChannels[0].Config), stored.NotificationChannels[0].Config.String(), "and so does a channel's")
		assert.True(t, stored.NotificationChannels[0].IsDefault)
		storedSite := monitorNamed(t, stored, "Site")
		assert.Equal(t, stored.Proxies[0].Ref, storedSite.ProxyRef)
		assert.Equal(t, []string{stored.NotificationChannels[0].Ref}, storedSite.NotificationChannelRefs)
		assert.ElementsMatch(t, []string{stored.Tags[0].Ref, stored.Tags[1].Ref}, storedSite.TagRefs)
		assert.Equal(t, "cron-token", monitorNamed(t, stored, "Cron").PushToken, "a token nobody holds is kept")
		require.Len(t, stored.Maintenances, 1)
		assert.Equal(t, []string{storedSite.Ref}, stored.Maintenances[0].MonitorRefs)
		assert.Equal(t, "0 2 * * 1,3", *stored.Maintenances[0].Cron)
		require.Len(t, stored.StatusPages, 1)
		assert.Equal(t, "target-status", stored.StatusPages[0].Slug)
		assert.Equal(t, []string{"status.target.example"}, stored.StatusPages[0].Domains, "a domain is normalized")
		assert.ElementsMatch(t, []string{monitorNamed(t, stored, "Cron").Ref, storedSite.Ref}, stored.StatusPages[0].MonitorRefs, "the server stores no order for a page's monitors")
	})
}

func TestWorkspaceImport_ATagTheWorkspaceHasIsReusedAndNotCreatedAgain(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	existing := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", map[string]any{"name": "web", "color": "#FF0000"}, target.owner), http.StatusCreated).ID
	doc := docOf()
	doc.Tags = []workspaceport.Tag{docTag("t-web", "web"), docTag("t-ops", "ops")}
	site := docHTTPMonitor("m1", "Site")
	site.TagRefs = []string{"t-web", "t-ops"}
	doc.Monitors = []workspaceport.Monitor{site}

	result := a.imported(t, doc, target.owner)

	assert.Equal(t, 1, result.Created.Tags, "only ops is new")
	assert.Equal(t, workspaceport.Change{Kind: "tag_reused", Record: "tag", Ref: "t-web", Name: "web", To: existing}, changeOf(t, result, "tag_reused", "t-web"))
	assert.Len(t, result.Changes, 1)
	stored := a.exportDocument(t, target.owner)
	require.Len(t, stored.Tags, 2)
	byName := map[string]workspaceport.Tag{stored.Tags[0].Name: stored.Tags[0], stored.Tags[1].Name: stored.Tags[1]}
	assert.Equal(t, existing, byName["web"].Ref, "the monitor points at the workspace's own tag")
	assert.Equal(t, "#FF0000", byName["web"].Color, "which keeps its colour")
	assert.ElementsMatch(t, []string{byName["web"].Ref, byName["ops"].Ref}, monitorNamed(t, stored, "Site").TagRefs)
}

func TestWorkspaceImport_AProxyAtABlockedAddressIsSkippedAndItsMonitorsRunWithoutOne(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withBlockPrivateTargets(true))
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		doc.Proxies = []workspaceport.Proxy{
			{Ref: "px-bad", Protocol: "http", Host: "10.0.0.5", Port: 3128, SensitiveFields: []string{}},
			{Ref: "px-ok", Protocol: "http", Host: "proxy.example", Port: 3128, SensitiveFields: []string{}},
		}
		behindBad, behindOK := docHTTPMonitor("m-bad", "Behind bad"), docHTTPMonitor("m-ok", "Behind ok")
		behindBad.ProxyRef, behindOK.ProxyRef = "px-bad", "px-ok"
		doc.Monitors = []workspaceport.Monitor{behindBad, behindOK}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, workspaceport.Created{Proxies: 1, Monitors: 2}, result.Created)
		assert.Equal(t, workspaceport.Change{Kind: "proxy_skipped_target_not_allowed", Record: "proxy", Ref: "px-bad", Name: "http://10.0.0.5:3128", Reason: "TARGET_NOT_ALLOWED"},
			changeOf(t, result, "proxy_skipped_target_not_allowed", "px-bad"))
		assert.Equal(t, workspaceport.Change{Kind: "monitor_proxy_dropped", Record: "monitor", Ref: "m-bad", Name: "Behind bad", From: "http://10.0.0.5:3128"},
			changeOf(t, result, "monitor_proxy_dropped", "m-bad"))
		assert.Len(t, result.Changes, 2)
		stored := a.exportDocument(t, target.owner)
		require.Len(t, stored.Proxies, 1)
		assert.Equal(t, "proxy.example", stored.Proxies[0].Host)
		assert.Empty(t, monitorNamed(t, stored, "Behind bad").ProxyRef)
		assert.Equal(t, stored.Proxies[0].Ref, monitorNamed(t, stored, "Behind ok").ProxyRef)
	})
}

func TestWorkspaceImport_AChannelTheInstallRefusesIsSkippedWithItsReasonAndItsLinksDropped(t *testing.T) {
	forEachDatabaseWith(t, []func(*config.Config){selfHosted, withBlockPrivateTargets(true)}, func(t *testing.T, a *app) {
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		webhook := func(ref, url string) workspaceport.NotificationChannel {
			return workspaceport.NotificationChannel{Ref: ref, Name: ref, Type: "webhook", Active: true,
				Config: workspaceport.ConfigJSON(`{"webhook_url":"` + url + `","webhook_content_type":"json"}`), SensitiveFields: []string{}}
		}
		doc.NotificationChannels = []workspaceport.NotificationChannel{
			webhook("c-metadata", "http://169.254.169.254/hook"), webhook("c-ok", "https://hooks.example/p"),
			{Ref: "c-mail", Name: "Mail", Type: "install_email", Active: true, Config: workspaceport.ConfigJSON(`{"to":"someone@example.com"}`), SensitiveFields: []string{}},
		}
		site := docHTTPMonitor("m1", "Site")
		site.NotificationChannelRefs = []string{"c-metadata", "c-ok", "c-mail"}
		doc.Monitors = []workspaceport.Monitor{site}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, workspaceport.Created{NotificationChannels: 1, Monitors: 1}, result.Created)
		assert.Equal(t, workspaceport.Change{Kind: "channel_skipped", Record: "notificationChannel", Ref: "c-metadata", Name: "c-metadata", Reason: "target_not_allowed"},
			changeOf(t, result, "channel_skipped", "c-metadata"))
		assert.Equal(t, "no_install_sender", changeOf(t, result, "channel_skipped", "c-mail").Reason, "an install without a sender for account emails")
		assert.Len(t, result.Changes, 2)
		stored := a.exportDocument(t, target.owner)
		require.Len(t, stored.NotificationChannels, 1)
		assert.Equal(t, "c-ok", stored.NotificationChannels[0].Name)
		assert.Equal(t, []string{stored.NotificationChannels[0].Ref}, monitorNamed(t, stored, "Site").NotificationChannelRefs, "the links to the others are dropped")
	})
}

func TestWorkspaceImport_AnInstallEmailChannelNeedsAMemberOfTheTargetAsItsRecipient(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		installMail := func(ref, to string) workspaceport.NotificationChannel {
			return workspaceport.NotificationChannel{Ref: ref, Name: ref, Type: "install_email", Active: true, Config: workspaceport.ConfigJSON(`{"to":"` + to + `"}`), SensitiveFields: []string{}}
		}
		doc.NotificationChannels = []workspaceport.NotificationChannel{installMail("c-member", "target-owner@example.com"), installMail("c-stranger", "stranger@example.com")}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, 1, result.Created.NotificationChannels)
		assert.Equal(t, "recipient_not_member", changeOf(t, result, "channel_skipped", "c-stranger").Reason)
		assert.Len(t, result.Changes, 1)
		stored := a.exportDocument(t, target.owner)
		require.Len(t, stored.NotificationChannels, 1)
		assert.Equal(t, "c-member", stored.NotificationChannels[0].Name)
	})
}

func TestWorkspaceImport_AMonitorOfADisabledTypeOrAtABlockedTargetIsSkippedWithItsLinks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withDisabledMonitorTypes("tcp"), withBlockPrivateTargets(true))
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		tcp := docHTTPMonitor("m-tcp", "Database")
		tcp.Type, tcp.Config = "tcp", workspaceport.ConfigJSON(`{"host":"db.example.com","port":5432}`)
		internal := docHTTPMonitor("m-internal", "Internal")
		internal.Config = workspaceport.ConfigJSON(`{"url":"http://10.1.2.3/health","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`)
		doc := docOf()
		doc.Monitors = []workspaceport.Monitor{tcp, internal, docHTTPMonitor("m-ok", "Site")}
		doc.Maintenances = []workspaceport.Maintenance{{
			Ref: "w1", Title: "Window", Active: true, Strategy: "manual", Weekdays: []int{}, DaysOfMonth: []int{}, MonitorRefs: []string{"m-tcp", "m-internal", "m-ok"},
		}}
		doc.StatusPages = []workspaceport.StatusPage{docPage("p1", "status", "m-tcp", "m-internal", "m-ok")}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, workspaceport.Created{Monitors: 1, Maintenances: 1, StatusPages: 1}, result.Created)
		assert.Equal(t, workspaceport.Change{Kind: "monitor_skipped_type_disabled", Record: "monitor", Ref: "m-tcp", Name: "Database", Reason: "tcp"},
			changeOf(t, result, "monitor_skipped_type_disabled", "m-tcp"))
		assert.Equal(t, workspaceport.Change{Kind: "monitor_skipped_target_not_allowed", Record: "monitor", Ref: "m-internal", Name: "Internal", Reason: "10.1.2.3"},
			changeOf(t, result, "monitor_skipped_target_not_allowed", "m-internal"))
		assert.Len(t, result.Changes, 2)
		stored := a.exportDocument(t, target.owner)
		require.Len(t, stored.Monitors, 1)
		assert.Equal(t, []string{stored.Monitors[0].Ref}, stored.Maintenances[0].MonitorRefs, "the window covers the monitor that came in")
		assert.Equal(t, []string{stored.Monitors[0].Ref}, stored.StatusPages[0].MonitorRefs, "and so does the page")
	})
}

func TestWorkspaceImport_AnIntervalBelowTheMinimumIsRaisedToIt(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(60))
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	a.setOwnLimits(t, target.id, &limits.Limits{MinCheckInterval: limitOf(120)})
	short, ok := docHTTPMonitor("m-short", "Short"), docHTTPMonitor("m-ok", "Long")
	short.Interval, short.RetryInterval = 30, 90
	ok.Interval, ok.RetryInterval = 120, 120
	doc := docOf()
	doc.Monitors = []workspaceport.Monitor{short, ok}

	result := a.imported(t, doc, target.owner)

	assert.Equal(t, workspaceport.Change{Kind: "monitor_interval_raised", Record: "monitor", Ref: "m-short", Name: "Short", From: "30", To: "120", Reason: "interval"},
		changeOf(t, result, "monitor_interval_raised", "m-short"))
	assert.Len(t, result.Changes, 2, "the retry interval is raised too, and the monitor at the minimum is left alone")
	retry := result.Changes[1]
	assert.Equal(t, [4]string{"90", "120", "retryInterval", "m-short"}, [4]string{retry.From, retry.To, retry.Reason, retry.Ref})
	stored := monitorNamed(t, a.exportDocument(t, target.owner), "Short")
	assert.Equal(t, [2]int{120, 120}, [2]int{stored.Interval, stored.RetryInterval}, "the stricter of the install's minimum and the plan's")
}

func TestWorkspaceImport_APushTokenAnotherMonitorHoldsIsReplacedByANewOne(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		a.createMonitor(t, "Holder", "held-token")
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		doc.Monitors = []workspaceport.Monitor{
			docPushMonitor("m-held", "Held", "held-token"), docPushMonitor("m-free", "Free", "free-token"), docPushMonitor("m-twin", "Twin", "free-token"),
		}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, workspaceport.Change{Kind: "push_token_replaced", Record: "monitor", Ref: "m-held", Name: "Held"}, changeOf(t, result, "push_token_replaced", "m-held"))
		changeOf(t, result, "push_token_replaced", "m-twin")
		assert.Len(t, result.Changes, 2, "the token of the file's first monitor is kept, and no token is echoed")
		stored := a.exportDocument(t, target.owner)
		held, free, twin := monitorNamed(t, stored, "Held"), monitorNamed(t, stored, "Free"), monitorNamed(t, stored, "Twin")
		assert.Equal(t, "free-token", free.PushToken)
		for _, m := range []workspaceport.Monitor{held, twin} {
			assert.NotEmpty(t, m.PushToken)
			assert.NotContains(t, []string{"held-token", "free-token"}, m.PushToken)
			assert.JSONEq(t, `{"pushToken":"`+m.PushToken+`"}`, m.Config.String(), "and the config carries the new one")
		}
		assert.NotEqual(t, held.PushToken, twin.PushToken)
	})
}

func TestWorkspaceImport_ActiveMonitorsPastThePlansLimitComePausedInTheOrderOfTheFile(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		a.setOwnLimits(t, target.id, &limits.Limits{Monitors: limitOf(3)})
		decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Existing", "existing-token"), target.owner), http.StatusCreated)
		paused := docHTTPMonitor("m-paused", "Already paused")
		paused.Active = false
		doc := docOf()
		doc.Monitors = []workspaceport.Monitor{docHTTPMonitor("m1", "First"), paused, docHTTPMonitor("m2", "Second"), docHTTPMonitor("m3", "Third")}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, 4, result.Created.Monitors)
		assert.Equal(t, workspaceport.Change{Kind: "monitor_paused_over_limit", Record: "monitor", Ref: "m3", Name: "Third"}, changeOf(t, result, "monitor_paused_over_limit", "m3"))
		assert.Len(t, result.Changes, 1, "two fit beside the one the workspace has, and a paused monitor takes no room")
		stored := a.exportDocument(t, target.owner)
		active := map[string]bool{}
		for _, m := range stored.Monitors {
			active[m.Name] = m.Active
		}
		assert.Equal(t, map[string]bool{"Existing": true, "First": true, "Already paused": false, "Second": true, "Third": false}, active)
	})
}

func TestWorkspaceImport_MonitorsRunOnTheInstallsDefaultProbes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		frankfurt := a.createProbe(t, map[string]any{"name": "Frankfurt"})
		a.makeDefault(t, frankfurt.ID, true)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		doc.Monitors = []workspaceport.Monitor{docHTTPMonitor("m1", "Site"), docPushMonitor("m2", "Cron", "probe-token")}

		a.imported(t, doc, target.owner)

		probesOf := func(name string) []string {
			for _, m := range decode[[]monitorWithProbes](t, a.request(t, http.MethodGet, monitorsPath, nil, target.owner), http.StatusOK) {
				if m.Name == name {
					return sorted(m.ProbeIDs)
				}
			}
			return nil
		}
		assert.Equal(t, sorted([]string{"local", frankfurt.ID}), probesOf("Site"), "as a monitor created without probes does")
		assert.Equal(t, []string{"local"}, probesOf("Cron"), "and a push monitor runs on the local probe alone")
	})
}

func TestWorkspaceImport_ADataRetentionPeriodAboveThePlansCapIsSetToIt(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	a.setOwnLimits(t, target.id, &limits.Limits{RetentionDays: limitOf(30)})
	doc := docOf()
	doc.Settings = workspaceport.Settings{DataRetentionDays: 365, CertExpiryNotifyDays: []int{5, 10}}

	result := a.imported(t, doc, target.owner)

	assert.Equal(t, []workspaceport.Change{{Kind: "retention_capped", Record: "settings", Name: "dataRetentionDays", From: "365", To: "30"}}, result.Changes)
	assert.Equal(t, workspaceport.Settings{DataRetentionDays: 30, CertExpiryNotifyDays: []int{5, 10}}, a.exportDocument(t, target.owner).Settings, "the certificate days come as given")
}

func TestWorkspaceImport_ATakenSlugGetsTheFirstFreeNumberAndATakenDomainIsDropped(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		other := a.emptyWorkspace(t, "Other", "other-owner@example.com")
		a.createStatusPageAs(t, other.owner, statusPage{slug: "status", published: true, domains: []string{"status.example.org"}})
		a.createStatusPageAs(t, other.owner, statusPage{slug: "status-2", published: true})
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		first, second, third := docPage("p1", "status"), docPage("p2", "status"), docPage("p3", "fresh")
		first.Domains = []string{"STATUS.example.org", "mine.example.org"}
		second.Domains = []string{"mine.example.org", "second.example.org"}
		doc := docOf()
		doc.StatusPages = []workspaceport.StatusPage{first, second, third}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, 3, result.Created.StatusPages)
		assert.Equal(t, workspaceport.Change{Kind: "status_page_slug_changed", Record: "statusPage", Ref: "p1", Name: "Status of status", From: "status", To: "status-3"},
			changeOf(t, result, "status_page_slug_changed", "p1"))
		assert.Equal(t, "status-4", changeOf(t, result, "status_page_slug_changed", "p2").To, "the file's own earlier page takes status-3")
		assert.Equal(t, workspaceport.Change{Kind: "custom_domain_dropped", Record: "customDomain", Ref: "p1", Name: "status.example.org"},
			changeOf(t, result, "custom_domain_dropped", "p1"), "a domain another workspace's page holds, in any case")
		assert.Equal(t, "mine.example.org", changeOf(t, result, "custom_domain_dropped", "p2").Name, "and one an earlier page of the file took")
		assert.Len(t, result.Changes, 4)
		pages := map[string]workspaceport.StatusPage{}
		for _, p := range a.exportDocument(t, target.owner).StatusPages {
			pages[p.Title+"/"+p.Slug] = p
		}
		assert.Equal(t, []string{"mine.example.org"}, pages["Status of status/status-3"].Domains)
		assert.Equal(t, []string{"second.example.org"}, pages["Status of status/status-4"].Domains)
		assert.Contains(t, pages, "Status of fresh/fresh")
	})
}

func TestWorkspaceImport_ASlugAtTheLongestLengthThatIsTakenIsTrimmedForItsNumberToFit(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		longest := strings.Repeat("a", 255)
		other := a.emptyWorkspace(t, "Other", "other-owner@example.com")
		a.createStatusPageAs(t, other.owner, statusPage{slug: longest, published: true})
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		doc := docOf()
		doc.StatusPages = []workspaceport.StatusPage{docPage("p1", longest), docPage("p2", longest)}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, 2, result.Created.StatusPages)
		assert.Equal(t, strings.Repeat("a", 253)+"-2", changeOf(t, result, "status_page_slug_changed", "p1").To, "the column holds 255 characters")
		assert.Equal(t, strings.Repeat("a", 253)+"-3", changeOf(t, result, "status_page_slug_changed", "p2").To)
		slugs := []string{}
		for _, p := range a.exportDocument(t, target.owner).StatusPages {
			slugs = append(slugs, p.Slug)
		}
		assert.ElementsMatch(t, []string{strings.Repeat("a", 253) + "-2", strings.Repeat("a", 253) + "-3"}, slugs)
	})
}

func TestWorkspaceImport_StatusPagesAndDomainsPastThePlansLimitsAreLeftOut(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		a.setOwnLimits(t, target.id, &limits.Limits{StatusPages: limitOf(2), CustomDomains: limitOf(2)})
		a.createStatusPageAs(t, target.owner, statusPage{slug: "existing", published: true, domains: []string{"existing.example.org"}})
		first, second := docPage("p1", "first"), docPage("p2", "second")
		first.Domains = []string{"a.example.org", "b.example.org"}
		second.Domains = []string{"c.example.org"}
		doc := docOf()
		doc.StatusPages = []workspaceport.StatusPage{first, second}

		result := a.imported(t, doc, target.owner)

		assert.Equal(t, 1, result.Created.StatusPages)
		assert.Equal(t, workspaceport.Change{Kind: "status_page_skipped_over_limit", Record: "statusPage", Ref: "p2", Name: "Status of second", Reason: "second"},
			changeOf(t, result, "status_page_skipped_over_limit", "p2"))
		assert.Equal(t, workspaceport.Change{Kind: "custom_domain_dropped_over_limit", Record: "customDomain", Ref: "p1", Name: "b.example.org"},
			changeOf(t, result, "custom_domain_dropped_over_limit", "p1"))
		assert.Len(t, result.Changes, 2)
		stored := a.exportDocument(t, target.owner)
		require.Len(t, stored.StatusPages, 2)
		for _, p := range stored.StatusPages {
			if p.Slug == "first" {
				assert.Equal(t, []string{"a.example.org"}, p.Domains)
			}
		}
	})
}

func TestWorkspaceImport_AMaintenanceWindowTheServiceWouldRefuseMakesTheFileInvalid(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	doc := docOf()
	doc.Maintenances = []workspaceport.Maintenance{
		{Ref: "w-cron", Title: "Cron without a duration", Active: true, Strategy: "cron", Cron: new("0 2 * * 1"), Weekdays: []int{}, DaysOfMonth: []int{}, MonitorRefs: []string{}},
		{Ref: "w-day", Title: "Weekday out of range", Active: true, Strategy: "recurring-weekday", Weekdays: []int{9}, DaysOfMonth: []int{}, StartTime: new("02:00"), EndTime: new("03:00"), MonitorRefs: []string{}},
	}

	found := problemMessages(a.refused(t, doc, target.owner))

	assert.Contains(t, found, "maintenance/w-cron: duration is required when cron expression is provided")
	assert.Contains(t, found, "maintenance/w-day: Weekdays[0] fails max=6")
}

func TestWorkspaceImport_ARecordInvalidInItselfMakesTheFileInvalid(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	doc := docOf()
	doc.Tags = []workspaceport.Tag{{Ref: "t1", Name: "ops", Color: "red"}, docTag("t2", "ops")}
	doc.Proxies = []workspaceport.Proxy{{Ref: "px", Protocol: "ftp", Host: "p.example", Port: 70000, SensitiveFields: []string{}}}
	doc.NotificationChannels = []workspaceport.NotificationChannel{
		{Ref: "c1", Name: "Unknown", Type: "carrier-pigeon", Config: workspaceport.ConfigJSON(`{}`)},
		{Ref: "c2", Name: "Hook", Type: "webhook", Config: workspaceport.ConfigJSON(`{"webhook_content_type":"json"}`)},
		{Ref: "c3", Name: "Not an object", Type: "webhook", Config: workspaceport.ConfigJSON(`"text"`)},
	}
	typeless := docHTTPMonitor("m1", "Typeless")
	typeless.Type = "no-such-type"
	short := docHTTPMonitor("m2", "ab")
	doc.Monitors = []workspaceport.Monitor{typeless, short}
	doc.Settings.DataRetentionDays = -4

	found := problemMessages(a.refused(t, doc, target.owner))

	for _, want := range []string{"tag/t1: Color fails hexcolor", "tag/t2: name \"ops\" is used by more than one tag", "proxy/px: Protocol fails oneof=http https socks socks4 socks5 socks5h, Port fails max=65535",
		"notificationChannel/c1: unsupported notification type: \"carrier-pigeon\"", "notificationChannel/c3: config must be a JSON object", "monitor/m2: Name fails min=3"} {
		assert.Contains(t, found, want)
	}
	assertProblem(t, found, "notificationChannel", "c2", "")
	assertProblem(t, found, "monitor", "m1", "")
	assertProblem(t, found, "settings", "", "dataRetentionDays")
	assert.Len(t, found, 9, "every problem is listed: %v", found)
}

func TestWorkspaceImport_ALocationRuleThatCanNotBeMetIsRefusedAndNothingIsWritten(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	before := a.footprint(t, seededWorkspace{id: target.id})
	bogus, tooMany := docHTTPMonitor("m1", "Bogus rule"), docHTTPMonitor("m2", "Too many")
	bogus.LocationRule = "most"
	tooMany.LocationRule, tooMany.LocationThreshold = "at_least", new(2)

	found := problemMessages(a.refused(t, with0(docOf(), bogus), target.owner))
	assertProblem(t, found, "monitor", "m1", `invalid location_rule: "most" is not a rule`)

	// The shape is fine; the target's default probes are what it can't be met on, which only the write finds
	found = problemMessages(a.refused(t, with0(docOf(), tooMany), target.owner))
	assertProblem(t, found, "monitor", "m2", "invalid location_rule: at_least 2 is out of range")
	assert.Equal(t, before, a.footprint(t, seededWorkspace{id: target.id}), "nothing was written")
}

// with0 is the document with the monitors
func with0(doc workspaceport.Document, monitors ...workspaceport.Monitor) workspaceport.Document {
	doc.Monitors = monitors
	return doc
}

func TestWorkspaceImport_ABodyOfUpTo16MiBIsTaken(t *testing.T) {
	a := newAppWithAdmin(t)
	target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
	padded := func(size int) string {
		doc := docOf()
		doc.Workspace.Name = ""
		raw, err := json.Marshal(doc)
		require.NoError(t, err)
		body := string(raw[:len(raw)-1]) + `,"padding":"` + strings.Repeat("a", size-len(raw)-len(`,"padding":""`)) + `"}`
		require.Len(t, body, size)
		return body
	}

	assert.Equal(t, http.StatusOK, a.importAs(t, padded(workspaceport.MaxImportBody), target.owner).Code, "16 MiB, a size no other route takes")
	res := a.importAs(t, padded(workspaceport.MaxImportBody+1), target.owner)
	assert.Equal(t, http.StatusRequestEntityTooLarge, res.Code, res.Body.String())
	assert.Equal(t, http.StatusRequestEntityTooLarge, a.importAs(t, padded(workspaceport.MaxImportBody+1)).Code, "before the sign-in is checked, as for any route")
	assert.Equal(t, http.StatusRequestEntityTooLarge, a.request(t, http.MethodPost, "/api/v1/tags", padded(oneMiB+1), target.owner).Code, "and every other route keeps 1 MiB")
}

// failingMaintenances is a maintenance repository whose create fails, so that an import fails
// after it has written everything that comes before the windows
type failingMaintenances struct{ maintenance.Repository }

func (failingMaintenances) Create(context.Context, workspace.ID, *maintenance.Model) (*maintenance.Model, error) {
	return nil, errors.New("the database went away")
}

func TestWorkspaceImport_AFailureWhileWritingLeavesNothingAndPublishesNothing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppDecorated(t, func(c *dig.Container) error {
			return c.Decorate(func(repository maintenance.Repository) maintenance.Repository { return failingMaintenances{repository} })
		}, backend)
		a.registerAdmin(t)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		published := a.watchMonitors(t, "created", events.MonitorCreated)
		before := a.footprint(t, seededWorkspace{id: target.id})
		doc := docOf()
		doc.Settings = workspaceport.Settings{DataRetentionDays: 33, CertExpiryNotifyDays: []int{3}}
		doc.Tags = []workspaceport.Tag{docTag("t1", "ops")}
		doc.Proxies = []workspaceport.Proxy{{Ref: "px", Protocol: "http", Host: "proxy.example", Port: 3128, SensitiveFields: []string{}}}
		site := docHTTPMonitor("m1", "Site")
		site.ProxyRef, site.TagRefs = "px", []string{"t1"}
		doc.Monitors = []workspaceport.Monitor{site, docPushMonitor("m2", "Cron", "atomic-token")}
		doc.Maintenances = []workspaceport.Maintenance{{Ref: "w1", Title: "Window", Active: true, Strategy: "manual", Weekdays: []int{}, DaysOfMonth: []int{}, MonitorRefs: []string{"m1"}}}
		doc.StatusPages = []workspaceport.StatusPage{docPage("p1", "atomic", "m1")}

		res := a.importAs(t, doc, target.owner)

		assert.Equal(t, http.StatusInternalServerError, res.Code, res.Body.String())
		assert.Equal(t, before, a.footprint(t, seededWorkspace{id: target.id}), "the tag, proxy, monitors and the rest were rolled back")
		assert.Equal(t, workspaceport.Settings{DataRetentionDays: 180, CertExpiryNotifyDays: []int{7, 14, 21}}, a.exportDocument(t, target.owner).Settings, "and so were the settings")
		a.createMonitor(t, "Control", "control-token")
		published.waitForMonitor(t, "Control")
		assert.Equal(t, []string{"Control"}, published.names(), "the monitors were not announced")
	})
}

func TestWorkspaceImport_TheMonitorsAreAnnouncedAsCreatedOnceTheirRowsAndLinksExist(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		published := a.watchMonitors(t, "created", events.MonitorCreated)
		links := a.readChannelsOnEvent(t, events.MonitorCreated)
		doc := docOf()
		doc.NotificationChannels = []workspaceport.NotificationChannel{{
			Ref: "c1", Name: "Hook", Type: "webhook", Active: true, Config: workspaceport.ConfigJSON(`{"webhook_url":"https://hooks.example/p","webhook_content_type":"json"}`), SensitiveFields: []string{},
		}}
		site := docHTTPMonitor("m1", "Site")
		site.NotificationChannelRefs = []string{"c1"}
		paused := docHTTPMonitor("m2", "Paused")
		paused.Active = false
		doc.Monitors = []workspaceport.Monitor{site, paused, docPushMonitor("m3", "Cron", "announce-token")}

		a.imported(t, doc, target.owner)

		for _, name := range []string{"Site", "Paused", "Cron"} {
			published.waitForMonitor(t, name)
		}
		assert.ElementsMatch(t, []string{"Site", "Paused", "Cron"}, published.names(), "once each, as a create announces it")
		stored := a.exportDocument(t, target.owner)
		require.Eventually(t, func() bool { return len(links.get("Site")) == 1 }, 5*time.Second, 5*time.Millisecond)
		assert.Equal(t, []string{stored.NotificationChannels[0].Ref}, links.get("Site"), "a listener finds the monitor's links")
		var announced, storedIDs []string
		for _, m := range published.seenMonitors() {
			announced = append(announced, m.ID)
		}
		for _, m := range stored.Monitors {
			storedIDs = append(storedIDs, m.Ref)
		}
		assert.ElementsMatch(t, storedIDs, announced, "the events are about the monitors that were stored")
	})
}

// refMapping maps the refs of one export to the refs the same records have in another export of
// the same setup: the records of a kind are in the same order in both, since an export orders them
// by name
func refMapping(t *testing.T, from, to workspaceport.Document) map[string]string {
	t.Helper()
	require.Len(t, to.Tags, len(from.Tags))
	require.Len(t, to.Proxies, len(from.Proxies))
	require.Len(t, to.NotificationChannels, len(from.NotificationChannels))
	require.Len(t, to.Monitors, len(from.Monitors))
	require.Len(t, to.Maintenances, len(from.Maintenances))
	require.Len(t, to.StatusPages, len(from.StatusPages))
	mapping := map[string]string{}
	for i := range from.Tags {
		mapping[from.Tags[i].Ref] = to.Tags[i].Ref
	}
	for i := range from.Proxies {
		mapping[from.Proxies[i].Ref] = to.Proxies[i].Ref
	}
	for i := range from.NotificationChannels {
		mapping[from.NotificationChannels[i].Ref] = to.NotificationChannels[i].Ref
	}
	for i := range from.Monitors {
		mapping[from.Monitors[i].Ref] = to.Monitors[i].Ref
	}
	for i := range from.Maintenances {
		mapping[from.Maintenances[i].Ref] = to.Maintenances[i].Ref
	}
	for i := range from.StatusPages {
		mapping[from.StatusPages[i].Ref] = to.StatusPages[i].Ref
	}
	return mapping
}

// comparable is the document as a test compares two exports of the same setup: the refs of
// from are the refs of to, the time of the export and the workspace's name (which an import leaves
// alone) are gone, and the lists of links, which an export sorts by ref, are sorted again
func comparable(t *testing.T, doc workspaceport.Document, mapping map[string]string) workspaceport.Document {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	text := string(raw)
	for from, to := range mapping {
		text = strings.ReplaceAll(text, `"`+from+`"`, `"`+to+`"`)
	}
	var changed workspaceport.Document
	require.NoError(t, json.Unmarshal([]byte(text), &changed))
	changed.ExportedAt, changed.Workspace.Name = time.Time{}, ""
	for i := range changed.Monitors {
		slices.Sort(changed.Monitors[i].NotificationChannelRefs)
		slices.Sort(changed.Monitors[i].TagRefs)
	}
	for i := range changed.Maintenances {
		slices.Sort(changed.Maintenances[i].MonitorRefs)
	}
	for i := range changed.StatusPages {
		slices.Sort(changed.StatusPages[i].MonitorRefs)
	}
	return changed
}

// asJSON makes two documents comparable with a diff that reads
func asJSON(t *testing.T, doc workspaceport.Document) string {
	t.Helper()
	raw, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	return string(raw)
}

func TestWorkspaceImport_ExportImportExportOnTheSameInstallDiffersOnlyInTheListedChanges(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		source := a.seedExportable(t)
		decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "33", "type": "int"}, source.owner), http.StatusOK)
		target := a.emptyWorkspace(t, "Target", "target-owner@example.com")
		exported := a.exportDocument(t, source.owner)

		result := a.imported(t, exported, target.owner)
		again := a.exportDocument(t, target.owner)

		// The slug and the custom domains of the page, and the push token, belong to the source
		assert.ElementsMatch(t, []workspaceport.Change{
			{Kind: "status_page_slug_changed", Record: "statusPage", Ref: source.page, Name: "Status of acme-status", From: "acme-status", To: "acme-status-2"},
			{Kind: "custom_domain_dropped", Record: "customDomain", Ref: source.page, Name: "acme.example.org"},
			{Kind: "custom_domain_dropped", Record: "customDomain", Ref: source.page, Name: "status.acme.example"},
			{Kind: "push_token_replaced", Record: "monitor", Ref: source.pushMonitor, Name: "Cron"},
		}, result.Changes)
		assert.Equal(t, workspaceport.Created{Tags: 2, Proxies: 1, NotificationChannels: 2, Monitors: 2, Maintenances: 1, StatusPages: 1}, result.Created)

		want := comparable(t, exported, refMapping(t, exported, again))
		want.StatusPages[0].Slug, want.StatusPages[0].Domains = "acme-status-2", []string{}
		cron := &want.Monitors[0]
		require.Equal(t, "Cron", cron.Name)
		got := comparable(t, again, nil)
		newToken := got.Monitors[0].PushToken
		require.NotEqual(t, "acme-push-token", newToken)
		cron.PushToken, cron.Config = newToken, workspaceport.ConfigJSON(`{"pushToken":"`+newToken+`"}`)
		assert.Equal(t, asJSON(t, want), asJSON(t, got))
	})
}

func TestWorkspaceImport_ExportImportExportAcrossInstallsDiffersInNothing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		first := newAppWithAdmin(t, backend)
		source := first.seedExportable(t)
		decode[settingResponse](t, first.request(t, http.MethodPut, retentionPath, map[string]any{"value": "33", "type": "int"}, source.owner), http.StatusOK)
		exported := first.exportDocument(t, source.owner)
		second := newAppWithAdmin(t, backend)
		target := second.emptyWorkspace(t, "Target", "target-owner@example.com")

		result := second.imported(t, exported, target.owner)
		again := second.exportDocument(t, target.owner)

		assert.Empty(t, result.Changes, "a fresh install holds nothing that conflicts")
		assert.Equal(t, asJSON(t, comparable(t, exported, refMapping(t, exported, again))), asJSON(t, comparable(t, again, nil)))
		assert.Equal(t, again.Settings, exported.Settings)
		assert.Equal(t, "Target", again.Workspace.Name, "the target keeps its own name")
	})
}
