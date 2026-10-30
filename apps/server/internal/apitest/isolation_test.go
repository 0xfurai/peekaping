package apitest

// The isolation suite (PP-153, docs/adr/0005-workspaces-and-data-isolation.md sections 3 and 8)
// proves that workspaces can't see each other's data. Its parts:
//
//   - This file walks every route the router registers (AC1). A route is either a workspace route
//     with a case in isolationCases, called by a member of workspace B with workspace A's IDs, or
//     it has a reasoned entry in the exemptions. A route that is in neither fails the suite.
//   - internal/isolation_test.go finds every call of a method named ...AnyWorkspace and fails
//     for a caller that ADR 0005 section 3 doesn't list (AC3).
//
// The paths that don't go through a request were proven with the two workspaces they involve where
// they live, and are not repeated here (AC2):
//
//   - the websocket: internal/modules/websocket, TestWorkspaceRoomGetsOnlyThatWorkspacesHeartbeats,
//     TestMonitorRoomIsOpenToMembersOfItsWorkspace and TestJoinsOfRoomsTheUserMayNotEnterAreRefused
//     (B joins none of A's rooms and gets none of A's heartbeats)
//   - notifications: TestRecordWorkspaces_AMonitorGoingDownNotifiesOnlyItsLinkedChannels in this
//     package, and TestListener_ALinkToAChannelOfAnotherWorkspaceSendsNothing in
//     internal/modules/notification_channel
//   - the probe protocol: TestMonitorWorkspace_AProbeIsDealtAndAnsweredForTheMonitorsOfEveryWorkspace
//   - the cleanup job: TestCleanupHeartbeatsKeepsEachWorkspacesOwnHistory in internal/modules/cleanup
//
// The tests run on SQLite, and on PostgreSQL when PEE_TEST_POSTGRES_DSN names a server (AC4), and CI
// runs the package again with the settings of the cloud, PEE_TEST_CLOUD=1 (AC5).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// isolationKinds are the kinds of record that a path or a body names by ID
var isolationKinds = []string{"monitor", "channel", "proxy", "tag", "maintenance", "status page", "API key", "member", "invite", "invite token"}

// isolationIDs are the IDs of the records of one kind each, by kind
type isolationIDs map[string]string

// isolationRequest is one call of a case
type isolationRequest struct {
	name  string
	query string
	body  any
	// status is what the answer is: 404 for an ID in the path, and 400 for an ID in the body, unless it
	// says otherwise
	status int
	// own marks a call that B answers from its own data. There is no unknown ID to compare it with:
	// it must hold nothing of A's. A call that changes something runs after the others, since B's
	// own records change the database.
	own bool
	// forbid are strings that B's answer must not hold, besides A's IDs and names
	forbid []string
	// sameName is a create that gives B's record the name or value of A's, which its answer then
	// repeats: only A's IDs are forbidden in it
	sameName bool
}

// isolationCase is the walk of one route: the kind of record its path names, and its calls
type isolationCase struct {
	kind     string
	requests func(ids isolationIDs) []isolationRequest
}

// aliceOnly is in the name of everything workspace A owns, and in none of B's answers
const aliceOnly = "aliceonly"

// aliceRetention is workspace A's data retention period, which no answer for B may show
const aliceRetention = "77"

func notFound(name string, body any) isolationRequest {
	return isolationRequest{name: name, body: body, status: http.StatusNotFound}
}

func plain(ids isolationIDs) []isolationRequest {
	return []isolationRequest{{status: http.StatusNotFound}}
}

func withBody(body any) func(isolationIDs) []isolationRequest {
	return func(isolationIDs) []isolationRequest { return []isolationRequest{notFound("", body)} }
}

// listOf is a list of a workspace's own records, and a search that would find A's
func listOf() func(isolationIDs) []isolationRequest {
	return func(isolationIDs) []isolationRequest {
		return []isolationRequest{
			{name: "list", own: true, status: http.StatusOK},
			{name: "search", query: "?q=" + aliceOnly, own: true, status: http.StatusOK},
		}
	}
}

func byID(kind string, requests func(isolationIDs) []isolationRequest) isolationCase {
	return isolationCase{kind: kind, requests: requests}
}

// isolationSince is the start of the stat points a monitor's route asks for
var isolationSince = url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))

// monitorNaming is a monitor create that links to one record, by the field of the body that does
func monitorNaming(name, field string, kind string, list bool) func(isolationIDs) []isolationRequest {
	return func(ids isolationIDs) []isolationRequest {
		var link any = ids[kind]
		if list {
			link = []string{ids[kind]}
		}
		return []isolationRequest{{name: name, body: with(pushMonitorBody("Doomed", "doomed-"+field), field, link), status: http.StatusBadRequest}}
	}
}

// isolationCases are the workspace routes: every route of the router that serves the records of a
// workspace, by method and path. A new route of that kind needs a case here (docs/adr/0005 section 8):
// B calls it with A's IDs and gets what it gets for an ID that no record has.
var isolationCases = map[string]isolationCase{
	"GET /api/v1/monitors": byID("", listOf()),
	"GET /api/v1/monitors/batch": byID("", func(ids isolationIDs) []isolationRequest {
		return []isolationRequest{{query: "?ids=" + ids["monitor"], status: http.StatusOK}}
	}),
	"POST /api/v1/monitors": byID("", func(ids isolationIDs) []isolationRequest {
		var requests []isolationRequest
		for _, link := range []struct {
			name, field, kind string
			list              bool
		}{{"naming a channel", "notification_ids", "channel", true}, {"naming a tag", "tag_ids", "tag", true}, {"naming a proxy", "proxy_id", "proxy", false}} {
			requests = append(requests, monitorNaming(link.name, link.field, link.kind, link.list)(ids)...)
		}
		return requests
	}),
	"GET /api/v1/monitors/{id}":        byID("monitor", plain),
	"PUT /api/v1/monitors/{id}":        byID("monitor", withBody(with(pushMonitorBody("Hijacked", "hijack"), "active", false))),
	"PATCH /api/v1/monitors/{id}":      byID("monitor", withBody(map[string]any{"name": "Hijacked", "active": false})),
	"DELETE /api/v1/monitors/{id}":     byID("monitor", plain),
	"POST /api/v1/monitors/{id}/reset": byID("monitor", plain),
	"GET /api/v1/monitors/{id}/heartbeats": byID("monitor", func(isolationIDs) []isolationRequest {
		return []isolationRequest{notFound("", nil), {name: "important", query: "?important=true&reverse=true", status: http.StatusNotFound}}
	}),
	"GET /api/v1/monitors/{id}/stats/points": byID("monitor", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{query: "?granularity=hour&since=" + isolationSince, status: http.StatusNotFound}}
	}),
	"GET /api/v1/monitors/{id}/stats/uptime": byID("monitor", plain),
	"GET /api/v1/monitors/{id}/tls":          byID("monitor", plain),

	"GET /api/v1/notification-channels": byID("", listOf()),
	"POST /api/v1/notification-channels": byID("", func(isolationIDs) []isolationRequest {
		// the same name as A's channel: names are not unique, and B's channel is B's
		return []isolationRequest{{name: "named like A's", body: webhookChannelBody(aliceOnly + " channel"), status: http.StatusCreated, own: true, sameName: true}}
	}),
	"GET /api/v1/notification-channels/{id}":    byID("channel", plain),
	"PUT /api/v1/notification-channels/{id}":    byID("channel", withBody(webhookChannelBody("Hijacked"))),
	"PATCH /api/v1/notification-channels/{id}":  byID("channel", withBody(map[string]any{"name": "Hijacked", "active": false})),
	"DELETE /api/v1/notification-channels/{id}": byID("channel", plain),
	"POST /api/v1/notification-channels/test": byID("", func(ids isolationIDs) []isolationRequest {
		return []isolationRequest{notFound("", with(webhookChannelBody("Hijacked"), "id", ids["channel"]))}
	}),

	"GET /api/v1/proxies": byID("", listOf()),
	"POST /api/v1/proxies": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{name: "with A's host", body: proxyBody(aliceOnly + ".example"), status: http.StatusCreated, own: true, sameName: true}}
	}),
	"GET /api/v1/proxies/{id}":    byID("proxy", plain),
	"PUT /api/v1/proxies/{id}":    byID("proxy", withBody(proxyBody("hijacked.example"))),
	"PATCH /api/v1/proxies/{id}":  byID("proxy", withBody(map[string]any{"host": "hijacked.example", "port": 1})),
	"DELETE /api/v1/proxies/{id}": byID("proxy", plain),

	"GET /api/v1/tags": byID("", listOf()),
	"POST /api/v1/tags": byID("", func(isolationIDs) []isolationRequest {
		// tag names are unique within a workspace and not across the install
		return []isolationRequest{{name: "named like A's", body: tagBody(aliceOnly + "-tag"), status: http.StatusCreated, own: true, sameName: true}}
	}),
	"GET /api/v1/tags/{id}":    byID("tag", plain),
	"PUT /api/v1/tags/{id}":    byID("tag", withBody(tagBody("hijacked"))),
	"PATCH /api/v1/tags/{id}":  byID("tag", withBody(map[string]any{"name": "hijacked"})),
	"DELETE /api/v1/tags/{id}": byID("tag", plain),

	"GET /api/v1/maintenances": byID("", listOf()),
	"POST /api/v1/maintenances": byID("", func(ids isolationIDs) []isolationRequest {
		return []isolationRequest{{name: "covering A's monitor", body: with(maintenanceBody("Doomed"), "monitor_ids", []string{ids["monitor"]}), status: http.StatusBadRequest}}
	}),
	"GET /api/v1/maintenances/{id}":          byID("maintenance", plain),
	"PUT /api/v1/maintenances/{id}":          byID("maintenance", withBody(maintenanceBody("Hijacked"))),
	"PATCH /api/v1/maintenances/{id}":        byID("maintenance", withBody(map[string]any{"title": "Hijacked"})),
	"DELETE /api/v1/maintenances/{id}":       byID("maintenance", plain),
	"PATCH /api/v1/maintenances/{id}/pause":  byID("maintenance", plain),
	"PATCH /api/v1/maintenances/{id}/resume": byID("maintenance", plain),

	"GET /api/v1/status-pages": byID("", listOf()),
	"POST /api/v1/status-pages": byID("", func(ids isolationIDs) []isolationRequest {
		return []isolationRequest{{name: "showing A's monitor", body: with(statusPageBody("Doomed"), "monitor_ids", []string{ids["monitor"]}), status: http.StatusBadRequest}}
	}),
	"GET /api/v1/status-pages/{id}": byID("status page", plain),
	"PATCH /api/v1/status-pages/{id}": byID("status page", func(isolationIDs) []isolationRequest {
		return []isolationRequest{
			notFound("title", map[string]any{"title": "Hijacked", "published": false}),
			notFound("monitors", map[string]any{"monitor_ids": []string{}}),
			notFound("domains", map[string]any{"domains": []string{"hijacked.example"}}),
		}
	}),
	"DELETE /api/v1/status-pages/{id}": byID("status page", plain),

	"GET /api/v1/settings/key/{key}": byID("", func(isolationIDs) []isolationRequest {
		// B has a period of its own, which is not A's
		return []isolationRequest{{status: http.StatusOK, own: true, forbid: []string{`"` + aliceRetention + `"`}}}
	}),
	"PUT /api/v1/settings/key/{key}": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{body: map[string]any{"value": "5", "type": "int"}, status: http.StatusOK, own: true}}
	}),
	"DELETE /api/v1/settings/key/{key}": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{status: http.StatusOK, own: true}}
	}),

	"GET /api/v1/api-keys": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{own: true, status: http.StatusOK}}
	}),
	"POST /api/v1/api-keys": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{body: map[string]any{"name": "Bob's second"}, status: http.StatusCreated, own: true}}
	}),
	"GET /api/v1/api-keys/{id}":    byID("API key", plain),
	"PUT /api/v1/api-keys/{id}":    byID("API key", withBody(map[string]any{"name": "Hijacked"})),
	"DELETE /api/v1/api-keys/{id}": byID("API key", plain),

	"PATCH /api/v1/workspace": byID("", func(isolationIDs) []isolationRequest {
		// B renames its own workspace: A's keeps its name, which the walk's snapshot of A checks
		return []isolationRequest{{body: map[string]any{"name": "Bobs renamed"}, status: http.StatusOK, own: true}}
	}),
	"GET /api/v1/workspace/members": byID("", func(isolationIDs) []isolationRequest {
		// B's members are B's own: A's owner is not among them
		return []isolationRequest{{own: true, status: http.StatusOK, forbid: []string{adminEmail}}}
	}),
	"GET /api/v1/workspace/limits": byID("", func(isolationIDs) []isolationRequest {
		// B's limits and usage are counted in B: what A has is not in them
		return []isolationRequest{{own: true, status: http.StatusOK}}
	}),
	"PATCH /api/v1/workspace/members/{userId}":  byID("member", withBody(map[string]any{"role": "member"})),
	"DELETE /api/v1/workspace/members/{userId}": byID("member", plain),
	"GET /api/v1/workspace/invites": byID("", func(isolationIDs) []isolationRequest {
		return []isolationRequest{{own: true, status: http.StatusOK}}
	}),
	"POST /api/v1/workspace/invites": byID("", func(isolationIDs) []isolationRequest {
		// the same email as A's invite: B's invite is B's, and must not revoke A's
		return []isolationRequest{{name: "for A's invitee", body: map[string]any{"email": aliceOnly + "-invitee@example.com", "role": "member"}, status: http.StatusCreated, own: true, sameName: true}}
	}),
	"DELETE /api/v1/workspace/invites/{id}": byID("invite", plain),
}

// The routes that serve no workspace's records, each with the reason. A route is here only if what
// it reaches is public, the user's own, the install's or a probe's, never a workspace's.
var (
	// isolationAccountRoutes belong to the user, who is not in one workspace (ADR 0005 section 1)
	isolationAccountRoutes = map[string]string{
		"GET /api/v1/me":                "the signed-in user's own account and the workspaces they are in",
		"POST /api/v1/auth/2fa/setup":   "the signed-in user's own two-factor secret",
		"POST /api/v1/auth/2fa/verify":  "the signed-in user's own two-factor secret",
		"POST /api/v1/auth/2fa/disable": "the signed-in user's own two-factor secret",
		"PUT /api/v1/auth/password":     "the signed-in user's own password",
		"DELETE /api/v1/auth/me":        "the signed-in user deletes their own account after entering their password; it takes no ID and reads no workspace's records (PP-170)",

		"POST /api/v1/auth/verify-email/resend": "the signed-in user's own verification link, sent to their own address",

		"POST /api/v1/workspaces":             "the signed-in user makes a workspace of their own and reads none",
		"POST /api/v1/invites/{token}/accept": "the signed-in user joins the workspace an invite's token names, if their account's email is the invited one; no workspace of the caller is read",
		"POST /api/v1/workspace/leave":        "the signed-in user leaves the workspace the request runs in, which only a member of it can name (404 otherwise, as for every route); it takes no ID and removes the caller's own place alone",

		"POST /api/v1/workspace/deletion":   "an owner schedules the deletion of the workspace the request runs in, which only a member of it can name (404 otherwise, as for every route); it takes no ID, reads none of the workspace's records and sets the workspace's own deletion date (PP-170)",
		"DELETE /api/v1/workspace/deletion": "an owner cancels the deletion of the workspace the request runs in, which only a member of it can name (404 otherwise, as for every route); it takes no ID and clears the workspace's own deletion date (PP-170)",
	}

	// isolationInstallRoutes belong to the install, and the instance admin manages them (ADR 0005 section 10)
	isolationInstallRoutes = map[string]string{
		"GET /api/v1/telemetry":                        "the install's usage ping choice",
		"PUT /api/v1/telemetry":                        "the install's usage ping choice, instance admin only",
		"POST /api/v1/telemetry/notice":                "the install's usage ping notice, instance admin only",
		"GET /api/v1/error-reporting":                  "the install's error reporting choice",
		"PUT /api/v1/error-reporting":                  "the install's error reporting choice, instance admin only",
		"GET /api/v1/probes":                           "probes belong to the install, and every member sees a probe's name, location and status",
		"GET /api/v1/probes/availability":              "probes belong to the install, and every member sees their availability",
		"GET /api/v1/probes/{id}":                      "probes belong to the install, and every member sees a probe's name, location and status",
		"GET /api/v1/probes/{id}/availability":         "probes belong to the install, and every member sees their availability",
		"POST /api/v1/probes":                          "probes belong to the install: instance admin only",
		"PUT /api/v1/probes/{id}":                      "probes belong to the install: instance admin only",
		"DELETE /api/v1/probes/{id}":                   "probes belong to the install: instance admin only",
		"POST /api/v1/probes/{id}/assign-all-monitors": "probes belong to the install, and an instance admin adds one to every monitor of it by design (ADR 0005 section 10)",
		"POST /api/v1/probes/{id}/rotate-token":        "probes belong to the install: instance admin only",

		"GET /api/v1/admin/users":                   "the install's users, instance admin only: each user's workspaces are shown by name and role, never a workspace's records",
		"PATCH /api/v1/admin/users/{id}":            "the install's users, instance admin only: the instance admin flag and disabling reach the user, not a workspace",
		"POST /api/v1/admin/users/{id}/reset-link":  "the install's users, instance admin only: a password reset link reaches the user, not a workspace",
		"POST /api/v1/admin/users/{id}/disable-2fa": "the install's users, instance admin only: the user's own 2FA, not a workspace",
		"DELETE /api/v1/admin/users/{id}":           "the install's users, instance admin only: a user's memberships go with them, no workspace's records do",
		"GET /api/v1/admin/workspaces":              "the install's workspaces, instance admin only: counts and metadata, never a workspace's records",
		"PATCH /api/v1/admin/workspaces/{id}":       "the install's workspaces, instance admin only: suspending or resuming reaches the workspace's hold, not its records",
		"DELETE /api/v1/admin/workspaces/{id}":      "the install's workspaces, instance admin only: deleting reaches the workspace and everything in it by cascade, no record is read",
		"GET /api/v1/admin/settings":                "the install settings, instance admin only: the signup mode, the sender and the public URL belong to the install, not a workspace",
		"PUT /api/v1/admin/settings":                "the install settings, instance admin only: a change reaches the install, not a workspace",
		"POST /api/v1/admin/settings/test-email":    "the install settings, instance admin only: a test email through the install's sender, no workspace is read",
	}
)

// isolationExemptions are every route that has no case, with its reason: the public ones, those of
// the user's account, those of the install and the probe protocol
func isolationExemptions() map[string]string {
	exempt := map[string]string{}
	add := func(kind string, routes map[string]string) {
		for key, reason := range routes {
			if _, twice := exempt[key]; twice {
				panic("route " + key + " is in two exemption lists")
			}
			exempt[key] = kind + ": " + reason
		}
	}
	add("public", publicRoutes)
	add("account", isolationAccountRoutes)
	add("install", isolationInstallRoutes)
	probe := map[string]string{}
	for key := range probeRoutes {
		probe[key] = "authenticated by the probe's token, and the monitors it is dealt are the install's by design (ADR 0005 section 10)"
	}
	add("probe protocol", probe)
	return exempt
}

// classifyRoutes returns what is wrong with the way the registered routes are classified. Each route
// is in exactly one of the cases and the exemptions, and no entry names a route that isn't there.
func classifyRoutes(registered []string, cases map[string]isolationCase, exempt map[string]string) []string {
	var problems []string
	known := map[string]bool{}
	for _, key := range registered {
		known[key] = true
		_, isCase := cases[key]
		_, isExempt := exempt[key]
		switch {
		case isCase && isExempt:
			problems = append(problems, fmt.Sprintf("route %q is both in isolationCases and in the exemptions: it is one or the other", key))
		case !isCase && !isExempt:
			problems = append(problems, fmt.Sprintf("route %q is in neither: add a case to isolationCases (it serves a workspace's records), or a reasoned exemption (it is public, the user's account, the install's or the probe protocol's)", key))
		}
	}
	for key := range cases {
		if !known[key] {
			problems = append(problems, fmt.Sprintf("isolationCases has a case for %q, which the router doesn't register: remove it", key))
		}
	}
	for key, reason := range exempt {
		if !known[key] {
			problems = append(problems, fmt.Sprintf("the exemptions list %q, which the router doesn't register: remove it", key))
		}
		if strings.TrimSpace(reason) == "" {
			problems = append(problems, fmt.Sprintf("the exemption of %q has no reason", key))
		}
	}
	sort.Strings(problems)
	return problems
}

func registeredKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, r := range newApp(t, withMetrics).routes() {
		keys = append(keys, r.key())
	}
	return keys
}

// Every route of the router is a workspace route with a case, or has a reason not to (AC1)
func TestIsolation_EveryRouteHasACaseOrAReasonedExemption(t *testing.T) {
	assert.Empty(t, classifyRoutes(registeredKeys(t), isolationCases, isolationExemptions()))
}

// A route the suite doesn't know is reported, with what to do about it (AC1)
func TestIsolation_ANewRouteInNeitherListIsReported(t *testing.T) {
	registered := append(registeredKeys(t), "GET /api/v1/reports/{id}", "DELETE /api/v1/reports/{id}")
	exempt := isolationExemptions()
	exempt["DELETE /api/v1/reports/{id}"] = "public: for the test"
	cases := map[string]isolationCase{"DELETE /api/v1/reports/{id}": byID("monitor", plain)}
	for key, c := range isolationCases {
		cases[key] = c
	}

	problems := classifyRoutes(registered, cases, exempt)

	require.Len(t, problems, 2, "the new route in neither list, and the one in both")
	assert.Contains(t, problems[0], `route "DELETE /api/v1/reports/{id}" is both in isolationCases and in the exemptions`)
	assert.Contains(t, problems[1], `route "GET /api/v1/reports/{id}" is in neither: add a case to isolationCases`)
	assert.Contains(t, problems[1], "or a reasoned exemption")

	// and an entry for a route that was removed or renamed
	problems = classifyRoutes(registeredKeys(t), map[string]isolationCase{"GET /api/v1/gone": byID("", plain)}, map[string]string{"GET /api/v1/also-gone": "x", "GET /health": " "})
	assert.Contains(t, strings.Join(problems, "\n"), `isolationCases has a case for "GET /api/v1/gone", which the router doesn't register`)
	assert.Contains(t, strings.Join(problems, "\n"), `the exemptions list "GET /api/v1/also-gone", which the router doesn't register`)
	assert.Contains(t, strings.Join(problems, "\n"), `the exemption of "GET /health" has no reason`)
}

// Every exemption has a route that isn't a workspace case, and the sets don't overlap (AC1)
func TestIsolation_NoRouteIsExemptedAndCovered(t *testing.T) {
	parallel(t)
	exempt := isolationExemptions()
	for key := range isolationCases {
		assert.NotContains(t, exempt, key)
	}
	for key, reason := range exempt {
		assert.NotEmpty(t, reason, key)
	}
}

// isolationWorld is an install with workspace A, who owns a record of each kind, and workspace B
type isolationWorld struct {
	a          *app
	workspaceA workspace.ID
	ids        isolationIDs
	missing    isolationIDs
	// bob is B's owner, and bobsKey an API key of B's
	bob, bobsKey credentials
	// aliceKeys are the IDs of every API key of A
	aliceKeys []string
}

func newIsolationWorld(t *testing.T, a *app) *isolationWorld {
	t.Helper()
	w := &isolationWorld{a: a, workspaceA: a.firstWorkspace(t), ids: isolationIDs{}, missing: isolationIDs{}}
	second := a.createWorkspace(t, "Second")
	w.bob = a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
	token := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "Bob's first"}, w.bob), http.StatusCreated).Token
	w.bobsKey = withHeader("X-API-Key", token)

	// A's records, linked as a workspace's are
	channel := recordModules[0].create(t, a, aliceOnly+" channel", a.asAdmin)
	proxy := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody(aliceOnly+".example"), a.asAdmin), http.StatusCreated).ID
	tag := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody(aliceOnly+"-tag"), a.asAdmin), http.StatusCreated).ID
	monitor := a.createMonitorWithHistory(t, aliceOnly+" monitor", channel, tag)
	require.NoError(t, a.container.Invoke(func(tls monitor_tls_info.Repository) error {
		_, err := tls.Upsert(context.Background(), monitor, `{"valid":true}`)
		return err
	}))
	maintenance := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody(aliceOnly+" window"), "monitor_ids", []string{monitor}), a.asAdmin), http.StatusCreated).ID
	page := a.createStatusPage(t, statusPage{slug: aliceOnly + "-page", published: true, monitors: []string{monitor}, domains: []string{aliceOnly + ".example.com"}})
	key := a.createKey(t, aliceOnly+" key", a.asAdmin)
	invited := a.invite(t, aliceOnly+"-invitee@example.com", "member", a.asAdmin)
	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": aliceRetention, "type": "int"}, a.asAdmin), http.StatusOK)
	for _, k := range decode[[]apiKeyResponse](t, a.request(t, http.MethodGet, "/api/v1/api-keys", nil, a.asAdmin), http.StatusOK) {
		w.aliceKeys = append(w.aliceKeys, k.ID)
	}
	for kind, id := range map[string]string{"monitor": monitor, "channel": channel, "proxy": proxy, "tag": tag, "maintenance": maintenance, "status page": page, "API key": key, "member": a.userID(t, adminEmail), "invite": invited.ID, "invite token": invited.Token} {
		w.ids[kind] = id
		w.missing[kind] = uuid.NewString()
	}
	require.ElementsMatch(t, isolationKinds, keysOf(w.ids))

	// B's records, so that B's lists are not empty
	for _, m := range recordModules {
		m.create(t, a, "bobs "+m.name, w.bob)
	}
	a.createMonitorAs(t, w.bob, "Bobs", "bobs-token")
	a.invite(t, "bobs-invitee@example.com", "member", w.bob)
	return w
}

func keysOf(m isolationIDs) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// forbidden are the strings no answer for B may hold
func (w *isolationWorld) forbidden(withName bool) []string {
	var forbidden []string
	if withName {
		forbidden = append(forbidden, aliceOnly)
	}
	for _, id := range w.ids {
		forbidden = append(forbidden, id)
	}
	return append(forbidden, w.aliceKeys...)
}

// build fills the route's path with the IDs and returns the call
func (w *isolationWorld) build(t *testing.T, key string, c isolationCase, r isolationRequest, ids isolationIDs) call {
	method, path, _ := strings.Cut(key, " ")
	path = strings.ReplaceAll(path, "{key}", "KEEP_DATA_PERIOD_DAYS")
	if c.kind != "" {
		path = strings.ReplaceAll(strings.ReplaceAll(path, "{id}", ids[c.kind]), "{userId}", ids[c.kind])
	}
	require.NotContains(t, path, "{", "every parameter of %s is filled", key)
	name := key
	if r.name != "" {
		name += " " + r.name
	}
	return call{name: name, method: method, path: path + r.query, body: r.body}
}

// normalize makes an answer comparable: the IDs the caller named become the name of their kind
func normalize(body string, ids isolationIDs) string {
	for _, kind := range isolationKinds {
		body = strings.ReplaceAll(body, ids[kind], "<"+kind+">")
	}
	return body
}

// sameAnswer checks that the two answers have the same status and, but for the IDs named, the same body
func sameAnswer(t *testing.T, w *isolationWorld, theirs, unknown *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, unknown.Code, theirs.Code, "the same status as for an ID that no record has")
	got, want := normalize(theirs.Body.String(), w.ids), normalize(unknown.Body.String(), w.missing)
	if json.Valid([]byte(got)) && json.Valid([]byte(want)) {
		assert.JSONEq(t, want, got, "the same body, so the two can't be told apart (ADR 0005 section 8)")
	} else {
		assert.Equal(t, want, got, "the same body, so the two can't be told apart (ADR 0005 section 8)")
	}
}

// walk calls the cases that read or are refused (phase one), or the own ones that change B's data
// (phase two), as the credentials
func (w *isolationWorld) walk(t *testing.T, creds credentials, mutating bool, skip func(key string) bool) {
	t.Helper()
	keys := make([]string, 0, len(isolationCases))
	for key := range isolationCases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if skip(key) {
			continue
		}
		c := isolationCases[key]
		for _, r := range c.requests(w.ids) {
			if (r.own && !strings.HasPrefix(key, "GET ")) != mutating {
				continue
			}
			theirs := w.build(t, key, c, r, w.ids)
			t.Run(theirs.name, func(t *testing.T) {
				answer := w.a.request(t, theirs.method, theirs.path, theirs.body, creds)
				assert.Equal(t, r.status, answer.Code, answer.Body.String())
				assert.Less(t, answer.Code, 500)
				if r.own {
					for _, forbidden := range append(w.forbidden(!r.sameName), r.forbid...) {
						assert.NotContains(t, answer.Body.String(), forbidden, "B's own answer holds nothing of A's")
					}
					return
				}
				assert.NotContains(t, answer.Body.String(), aliceOnly, "nothing of A's records")
				unknownRequests := c.requests(w.missing)
				var unknown call
				for _, u := range unknownRequests {
					if u.name == r.name {
						unknown = w.build(t, key, c, u, w.missing)
					}
				}
				other := w.a.request(t, unknown.method, unknown.path, unknown.body, creds)
				assert.Equal(t, r.status, other.Code, other.Body.String())
				sameAnswer(t, w, answer, other)
			})
		}
	}
}

// snapshot is what workspace A holds, as the API shows it to A and as the database stores it
type snapshot struct {
	api map[string]string
	db  map[string][]string
}

// apiView is A's read of everything it owns
func (w *isolationWorld) apiView(t *testing.T) map[string]string {
	t.Helper()
	view := map[string]string{}
	paths := []string{
		"/api/v1/monitors", "/api/v1/monitors/" + w.ids["monitor"], "/api/v1/monitors/" + w.ids["monitor"] + "/heartbeats",
		"/api/v1/monitors/" + w.ids["monitor"] + "/stats/uptime", "/api/v1/monitors/" + w.ids["monitor"] + "/tls",
		"/api/v1/notification-channels", "/api/v1/notification-channels/" + w.ids["channel"],
		"/api/v1/proxies", "/api/v1/proxies/" + w.ids["proxy"],
		"/api/v1/tags", "/api/v1/tags/" + w.ids["tag"],
		"/api/v1/maintenances", "/api/v1/maintenances/" + w.ids["maintenance"],
		"/api/v1/status-pages", "/api/v1/status-pages/" + w.ids["status page"],
		"/api/v1/api-keys", "/api/v1/api-keys/" + w.ids["API key"],
		"/api/v1/workspace/members", "/api/v1/workspace/invites",
		retentionPath,
	}
	for _, path := range paths {
		answer := w.a.request(t, http.MethodGet, path, nil, w.a.asAdmin)
		require.Equal(t, http.StatusOK, answer.Code, path+" "+answer.Body.String())
		view[path] = answer.Body.String()
	}
	return view
}

// dbView is the rows of A in the database. With everything, it is every row of every table that has
// no workspace of its own, besides the rows of A in the tables that have.
func (w *isolationWorld) dbView(t *testing.T, everything bool) map[string][]string {
	t.Helper()
	view := map[string][]string{}
	require.NoError(t, w.a.container.Invoke(func(db *bun.DB) error {
		ctx := context.Background()
		var tables []string
		query := "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
		if w.a.cfg.DBType == "sqlite" {
			query = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
		}
		if err := db.NewRaw(query).Scan(ctx, &tables); err != nil {
			return err
		}
		sort.Strings(tables)
		for _, table := range tables {
			rows, err := db.DB.QueryContext(ctx, `SELECT * FROM "`+table+`"`)
			if err != nil {
				return err
			}
			stored, err := readRows(rows, string(w.workspaceA), everything)
			if err != nil {
				return err
			}
			if len(stored) > 0 {
				view[table] = stored
			}
		}
		return nil
	}))
	return view
}

// readRows returns the rows as JSON, in order. A table with a workspace_id column gives A's rows,
// and the others all of theirs when everything is set and none otherwise.
func readRows(rows *sql.Rows, workspaceID string, everything bool) ([]string, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	scoped := slices.Contains(columns, "workspace_id")
	var stored []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, column := range columns {
			switch v := values[i].(type) {
			case []byte:
				row[column] = string(v)
			case time.Time:
				row[column] = v.UTC().Format(time.RFC3339Nano)
			default:
				row[column] = v
			}
		}
		if scoped && fmt.Sprint(row["workspace_id"]) != workspaceID {
			continue
		}
		if !scoped && !everything {
			continue
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		stored = append(stored, string(encoded))
	}
	sort.Strings(stored)
	return stored, rows.Err()
}

func (w *isolationWorld) snapshot(t *testing.T, everything bool) snapshot {
	t.Helper()
	return snapshot{api: w.apiView(t), db: w.dbView(t, everything)}
}

func isolationBackends(t *testing.T, fn func(t *testing.T, a *app)) {
	t.Helper()
	forEachDatabase(t, fn)
}

// A member of workspace B calls every route of the router that serves a workspace's records with
// workspace A's IDs. It answers as it does for an ID that no record has, and A's data is as it was (AC1)
func TestIsolation_AMemberOfAnotherWorkspaceReachesNothingOfThisOne(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		w := newIsolationWorld(t, a)
		before := w.snapshot(t, true)
		scopedBefore := w.dbView(t, false)
		require.NotEmpty(t, before.db["monitors"])
		require.NotEmpty(t, before.db["heartbeats"])
		require.NotEmpty(t, before.db["monitor_tls_info"])

		t.Run("as a member", func(t *testing.T) {
			w.walk(t, w.bob, false, func(string) bool { return false })
		})
		t.Run("as an API key", func(t *testing.T) {
			// the routes of an account's own keys take no API key
			w.walk(t, w.bobsKey, false, func(key string) bool { return jwtOnlyRoutes[key] })
		})
		after := w.snapshot(t, true)
		assert.Equal(t, before.api, after.api, "what A reads is as it was")
		assert.Equal(t, before.db, after.db, "and so is every row the database holds")

		// B's own changes come last, since they change the database
		t.Run("B's own changes", func(t *testing.T) {
			w.walk(t, w.bob, true, func(string) bool { return false })
		})
		afterChanges := w.snapshot(t, false)
		assert.Equal(t, scopedBefore, afterChanges.db, "A's rows are as they were after B's own creates, changes and deletes")
		assert.Equal(t, before.api, afterChanges.api)
	})
}

// The IDs of the cases are distinct kinds, and a case names a kind that is there
func TestIsolation_EveryCaseNamesAKindOfRecord(t *testing.T) {
	parallel(t)
	for key, c := range isolationCases {
		method, path, _ := strings.Cut(key, " ")
		assert.NotEmpty(t, method, key)
		assert.Equal(t, c.kind != "", strings.Contains(path, "{id}") || strings.Contains(path, "{userId}"), "%s takes an ID in its path exactly when the case names a kind", key)
		if c.kind != "" {
			assert.Contains(t, isolationKinds, c.kind, key)
		}
		assert.NotEmpty(t, c.requests(isolationIDs{}), key)
	}
}
