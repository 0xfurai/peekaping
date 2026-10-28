package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-372: the admin area's workspaces (docs/adr/0005 section 10). An instance admin sees every
// workspace by its counts and metadata, never what is inside, and suspends or resumes one.

const adminWorkspacesPath = "/api/v1/admin/workspaces"

type adminWorkspace struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	Owners    []string `json:"owners"`
	// MemberCount counts every member, owners included
	MemberCount int `json:"memberCount"`
	Monitors    struct {
		Active int `json:"active"`
		Paused int `json:"paused"`
	} `json:"monitors"`
	StatusPages     int     `json:"statusPages"`
	CustomDomains   int     `json:"customDomains"`
	Suspended       bool    `json:"suspended"`
	SuspendedReason *string `json:"suspendedReason"`
	SuspendedAt     *string `json:"suspendedAt"`
}

type adminWorkspacesPage struct {
	Workspaces []adminWorkspace `json:"workspaces"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
}

func (a *app) listWorkspaces(t *testing.T, query string) adminWorkspacesPage {
	t.Helper()
	return decode[adminWorkspacesPage](t, a.request(t, http.MethodGet, adminWorkspacesPath+query, nil, a.asAdmin), http.StatusOK)
}

// adminWorkspaceView is the workspace as the admin list shows it
func (a *app) adminWorkspaceView(t *testing.T, id workspace.ID) adminWorkspace {
	t.Helper()
	for _, w := range a.listWorkspaces(t, "?limit=100").Workspaces {
		if w.ID == string(id) {
			return w
		}
	}
	t.Fatalf("the admin list does not show workspace %s", id)
	return adminWorkspace{}
}

func (a *app) patchWorkspace(t *testing.T, id string, body map[string]any, creds ...credentials) *httpRecorder {
	t.Helper()
	if len(creds) == 0 {
		creds = []credentials{a.asAdmin}
	}
	return a.request(t, http.MethodPatch, adminWorkspacesPath+"/"+id, body, creds...)
}

func (a *app) setSuspended(t *testing.T, id workspace.ID, suspended bool) adminWorkspace {
	t.Helper()
	return decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"suspended": suspended}), http.StatusOK)
}

// createUnverifiedWorkspace makes a workspace that waits for its owner's email verification, owned
// by the user, as an open signup does
func (a *app) createUnverifiedWorkspace(t *testing.T, name, ownerEmail string) (workspace.ID, credentials) {
	t.Helper()
	var id workspace.ID
	ownerID := a.createUser(t, ownerEmail)
	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository) error {
		created, err := workspaces.CreateSuspended(context.Background(), name, workspace.SuspendedUnverified)
		if err != nil {
			return err
		}
		id = created.ID
		return workspaces.AddMember(context.Background(), id, ownerID, workspace.RoleOwner)
	}))
	return id, a.loginAs(t, ownerEmail)
}

// monitorUpdatedAt is when the monitor was last updated, as the watchdog counts from it
func (a *app) monitorUpdatedAt(t *testing.T, monitorID string) time.Time {
	t.Helper()
	var updated time.Time
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("monitors").Column("updated_at").Where("id = ?", monitorID).Scan(context.Background(), &updated)
	}))
	return updated
}

// acme is a workspace with two owners, a member, two active monitors and a paused one, and two
// status pages of which one has a custom domain
type acme struct {
	id                  workspace.ID
	alice, bob          credentials
	activeA, activeB    string
	paused              string
	slugWithDomain      string
	domain              string
	pushTokenOfActiveA  string
	statusPageOfDomain  string
	statusPageNoDomain  string
	memberEmail         string
	ownerEmails         []string
	monitorsOfWorkspace []string
}

func (a *app) createAcme(t *testing.T) acme {
	t.Helper()
	w := acme{id: a.createWorkspace(t, "Acme Corp"), slugWithDomain: "shop-page", domain: "status.acme.example", pushTokenOfActiveA: "acme-push-a"}
	w.alice = a.userIn(t, "alice@example.com", w.id, workspace.RoleOwner)
	w.bob = a.userIn(t, "bob@acme.test", w.id, workspace.RoleOwner)
	w.memberEmail = "carol@example.com"
	a.addMember(t, w.id, a.createUser(t, w.memberEmail), workspace.RoleMember)
	w.ownerEmails = []string{"alice@example.com", "bob@acme.test"}

	w.activeA = a.createMonitorAs(t, w.alice, "Site", w.pushTokenOfActiveA)
	w.activeB = a.createMonitorAs(t, w.alice, "API", "acme-push-b")
	w.paused = a.createMonitorAs(t, w.alice, "Old", "acme-push-c")
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPatch, monitorsPath+"/"+w.paused, map[string]any{"active": false}, w.alice).Code)
	w.monitorsOfWorkspace = []string{w.activeA, w.activeB, w.paused}

	w.statusPageOfDomain = a.createStatusPageAs(t, w.alice, statusPage{slug: w.slugWithDomain, published: true, monitors: []string{w.activeA}, domains: []string{w.domain}})
	w.statusPageNoDomain = a.createStatusPageAs(t, w.alice, statusPage{slug: "acme-draft", monitors: []string{w.activeB}})
	return w
}

// MARK: Access

func adminWorkspaceRoutes(id string) []call {
	return []call{
		{"list workspaces", http.MethodGet, adminWorkspacesPath, nil},
		{"change a workspace", http.MethodPatch, adminWorkspacesPath + "/" + id, map[string]any{"suspended": true}},
	}
}

func TestAdminWorkspaces_OnlyAnInstanceAdminSignedInAsAUserReachesThem(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.createAcme(t)
	member := a.loginAs(t, w.memberEmail)

	for _, route := range adminWorkspaceRoutes(string(w.id)) {
		t.Run(route.name+"/owner", func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, a.request(t, route.method, route.path, route.body, w.alice).Code)
		})
		t.Run(route.name+"/member", func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, a.request(t, route.method, route.path, route.body, member).Code)
		})
		t.Run(route.name+"/API key", func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, a.request(t, route.method, route.path, route.body, a.withAPIKey).Code)
		})
		t.Run(route.name+"/unauthenticated", func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, a.request(t, route.method, route.path, route.body).Code)
		})
	}
	assert.False(t, a.adminWorkspaceView(t, w.id).Suspended, "a refused change changes nothing")
}

// MARK: List

func TestAdminWorkspaces_ListShowsEveryWorkspaceWithItsCountsAndNothingFromInside(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		first := a.firstWorkspace(t)

		page := a.listWorkspaces(t, "")
		assert.Equal(t, 2, page.Total)
		require.Len(t, page.Workspaces, 2)
		assert.Equal(t, 0, page.Page)
		assert.Equal(t, 20, page.Limit)
		assert.Equal(t, string(first), page.Workspaces[0].ID, "the oldest first")

		got := a.adminWorkspaceView(t, w.id)
		assert.Equal(t, "Acme Corp", got.Name)
		assert.NotEmpty(t, got.CreatedAt)
		assert.Equal(t, []string{"alice@example.com", "bob@acme.test"}, got.Owners, "the owners' emails, in order")
		assert.Equal(t, 3, got.MemberCount)
		assert.Equal(t, 2, got.Monitors.Active)
		assert.Equal(t, 1, got.Monitors.Paused)
		assert.Equal(t, 2, got.StatusPages)
		assert.Equal(t, 1, got.CustomDomains)
		assert.False(t, got.Suspended)
		assert.Nil(t, got.SuspendedReason)
		assert.Nil(t, got.SuspendedAt)

		firstView := a.adminWorkspaceView(t, first)
		assert.Equal(t, []string{adminEmail}, firstView.Owners)
		assert.Equal(t, 1, firstView.MemberCount)
		assert.Equal(t, 0, firstView.Monitors.Active+firstView.Monitors.Paused+firstView.StatusPages+firstView.CustomDomains)
	})
}

// a later field must not leak by being added to the answer
func TestAdminWorkspaces_AnItemCarriesExactlyTheseKeys(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.createAcme(t)

	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	list := a.request(t, http.MethodGet, adminWorkspacesPath, nil, a.asAdmin)
	require.Equal(t, http.StatusOK, list.Code)
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &envelope))
	assert.ElementsMatch(t, []string{"workspaces", "total", "page", "limit"}, keys(envelope.Data))

	var items []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Data["workspaces"], &items))
	require.NotEmpty(t, items)
	wantItem := []string{"id", "name", "createdAt", "owners", "memberCount", "monitors", "statusPages", "customDomains", "suspended", "suspendedReason", "suspendedAt"}
	for _, item := range items {
		assert.ElementsMatch(t, wantItem, keys(item))
		var monitors map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(item["monitors"], &monitors))
		assert.ElementsMatch(t, []string{"active", "paused"}, keys(monitors))
	}

	// the answer of a change is the same item
	var changed struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	patch := a.patchWorkspace(t, string(w.id), map[string]any{"suspended": true})
	require.Equal(t, http.StatusOK, patch.Code)
	require.NoError(t, json.Unmarshal(patch.Body.Bytes(), &changed))
	assert.ElementsMatch(t, wantItem, keys(changed.Data))

	// and nothing from inside a workspace is in either answer
	for _, secret := range []string{w.pushTokenOfActiveA, w.domain, w.slugWithDomain, "Site", "acme-push"} {
		assert.NotContains(t, list.Body.String(), secret)
		assert.NotContains(t, patch.Body.String(), secret)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestAdminWorkspaces_SearchFindsAWorkspaceByNameOrAnOwnersEmailIgnoringCase(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		acmeWorkspace := a.createAcme(t)
		other := a.createWorkspace(t, "100% Legit_Co")
		a.userIn(t, "dora@elsewhere.test", other, workspace.RoleOwner)
		plain := a.createWorkspace(t, "Plain")
		a.userIn(t, "erin@plain.test", plain, workspace.RoleOwner)
		// a member's email is not an owner's
		a.addMember(t, plain, a.createUser(t, "zed@member.test"), workspace.RoleMember)

		names := func(query string) []string {
			var out []string
			for _, w := range a.listWorkspaces(t, "?q="+query).Workspaces {
				out = append(out, w.Name)
			}
			return out
		}
		assert.Equal(t, []string{"Acme Corp"}, names("ACME"), "by name, ignoring case")
		assert.Equal(t, []string{"Acme Corp"}, names("corp"))
		assert.Equal(t, []string{"Acme Corp"}, names("ALICE%40Example.COM"), "by an owner's email, ignoring case")
		assert.Equal(t, []string{"Acme Corp"}, names("BOB%40ACME"), "by any of the owners")
		assert.ElementsMatch(t, []string{"My workspace"}, names("my+workspace"))
		assert.Equal(t, []string{"Plain"}, names("erin%40plain"))
		assert.Empty(t, names("zed%40member"), "only an owner's email is searched")
		assert.Equal(t, []string{"100% Legit_Co"}, names("100%25"), "a percent sign is literal")
		assert.Equal(t, []string{"100% Legit_Co"}, names("t_c"), "an underscore is literal")
		assert.Empty(t, names("%25"+"nothing"), "a percent sign is no wildcard")
		assert.Equal(t, 1, a.listWorkspaces(t, "?q=acme").Total)
		assert.Equal(t, 4, a.listWorkspaces(t, "").Total, "an empty search lists all")
		_ = acmeWorkspace
	})
}

func TestAdminWorkspaces_PagesInTheOrderOfCreation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		for i := 0; i < 4; i++ {
			a.createWorkspace(t, fmt.Sprintf("Extra %d", i))
			time.Sleep(2 * time.Millisecond)
		}

		var all []adminWorkspace
		for page := 0; page < 3; page++ {
			got := a.listWorkspaces(t, fmt.Sprintf("?page=%d&limit=2", page))
			assert.Equal(t, 5, got.Total)
			assert.Equal(t, page, got.Page)
			assert.Equal(t, 2, got.Limit)
			all = append(all, got.Workspaces...)
		}
		require.Len(t, all, 5, "2 + 2 + 1")
		seen := map[string]bool{}
		for i, w := range all {
			assert.False(t, seen[w.ID], "no workspace twice")
			seen[w.ID] = true
			if i > 0 {
				assert.LessOrEqual(t, mustParse(t, all[i-1].CreatedAt), mustParse(t, w.CreatedAt))
			}
		}
		assert.Empty(t, a.listWorkspaces(t, "?page=3&limit=2").Workspaces)

		for _, bad := range []string{"?page=-1", "?limit=0", "?limit=101", "?page=x"} {
			assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodGet, adminWorkspacesPath+bad, nil, a.asAdmin).Code, bad)
		}
	})
}

// MARK: Suspend

func TestAdminWorkspaces_SuspendingHoldsBackEverythingThatRunsAndShowsMembersWhy(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		member := a.loginAs(t, w.memberEmail)
		source := a.schedulerSource(t)
		before := time.Now().UTC().Add(-time.Second)
		require.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(scheduled(t, source), w.monitorsOfWorkspace), "they run before")

		got := a.setSuspended(t, w.id, true)

		assert.True(t, got.Suspended)
		require.NotNil(t, got.SuspendedReason)
		assert.Equal(t, "admin", *got.SuspendedReason)
		require.NotNil(t, got.SuspendedAt)
		assert.WithinDuration(t, time.Now(), mustParse(t, *got.SuspendedAt), time.Minute)
		assert.True(t, mustParse(t, *got.SuspendedAt).After(before))
		assert.Equal(t, 2, got.Monitors.Active, "nothing is deleted or changed")
		assert.Equal(t, 1, got.Monitors.Paused)
		assert.Equal(t, 2, got.StatusPages)
		assert.Equal(t, got, a.adminWorkspaceView(t, w.id), "the list says the same")

		// members see it on /me
		for _, creds := range []credentials{member, w.alice} {
			me := a.meOf(t, creds)
			require.Len(t, me.Workspaces, 1)
			assert.True(t, me.Workspaces[0].Suspended)
			require.NotNil(t, me.Workspaces[0].SuspendedReason)
			assert.Equal(t, "admin", *me.Workspaces[0].SuspendedReason)
		}

		// the monitors are kept and listed, but none runs
		listed := decode[[]monitorViewWithFlag](t, a.request(t, http.MethodGet, monitorsPath, nil, w.alice), http.StatusOK)
		assert.Len(t, listed, 3)
		assert.Empty(t, intersect(scheduled(t, source), w.monitorsOfWorkspace), "the scheduler's list")
		found, err := source.Find(context.Background(), w.activeA)
		require.NoError(t, err)
		assert.Nil(t, found)
		_, err = source.Load(context.Background(), w.activeA)
		assert.ErrorIs(t, err, check.ErrWorkspaceSuspended, "the loader")
		require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
			active, err := monitors.FindActiveAnyWorkspace(context.Background())
			require.NoError(t, err)
			assert.Empty(t, intersect(idsOf(active), w.monitorsOfWorkspace))
			paged, err := monitors.FindActivePaginatedAnyWorkspace(context.Background(), 0, 1000)
			require.NoError(t, err)
			assert.Empty(t, intersect(idsOf(paged), w.monitorsOfWorkspace))
			byProbe, err := monitors.FindActiveByProbeAnyWorkspace(context.Background(), "local", 0, 1000)
			require.NoError(t, err)
			assert.Empty(t, intersect(idsOf(byProbe), w.monitorsOfWorkspace))
			return nil
		}))

		// a push is refused, and the answer names the administrator, not an email verification
		push := a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil)
		assert.Equal(t, http.StatusForbidden, push.Code, push.Body.String())
		assert.Contains(t, push.Body.String(), "suspended by the install's administrator")
		assert.NotContains(t, push.Body.String(), "verifies")
		assert.Empty(t, a.storedHeartbeats(t, w.activeA))
	})
}

func idsOf(monitors []*monitor.Model) []string {
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

// intersect is the IDs of list that are in only
func intersect(list, only []string) []string {
	in := map[string]bool{}
	for _, id := range only {
		in[id] = true
	}
	var out []string
	for _, id := range list {
		if in[id] {
			out = append(out, id)
		}
	}
	return out
}

// the response of a status page or a badge that is unavailable: no reason, no name
type unavailable struct {
	Message string `json:"message"`
	Error   struct {
		Code string `json:"code"`
	} `json:"error"`
}

func assertUnavailable(t *testing.T, w *httpRecorder, what string) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, w.Code, what+": "+w.Body.String())
	var got unavailable
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), what)
	assert.Equal(t, "This status page is unavailable", got.Message, what)
	assert.Equal(t, "STATUS_PAGE_UNAVAILABLE", got.Error.Code, what)
	for _, leak := range []string{"suspend", "admin", "verif", "Acme"} {
		assert.NotContains(t, w.Body.String(), leak, what)
	}
}

var publicStatusPageRoutes = func(slug, domain string) []string {
	return []string{
		"/api/v1/status-pages/slug/" + slug,
		"/api/v1/status-pages/domain/" + domain,
		"/api/v1/status-pages/slug/" + slug + "/monitors",
		"/api/v1/status-pages/slug/" + slug + "/monitors/homepage",
	}
}

func TestAdminWorkspaces_ASuspendedWorkspacesStatusPagesAndBadgesAnswerUnavailable(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		routes := publicStatusPageRoutes(w.slugWithDomain, w.domain)
		for _, path := range routes {
			require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, "it answers before: "+path)
		}
		for path := range badgePaths(w.activeA) {
			require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, "it answers before: "+path)
		}

		a.setSuspended(t, w.id, true)

		for _, path := range routes {
			assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
		}
		for path := range badgePaths(w.activeA) {
			assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
		}
		// a page that is not published, and a monitor that is on none, answer what they always did
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/acme-draft", nil).Code)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/badge/"+w.activeB+"/status", nil).Code)
		// the members keep their own view of the page
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, statusPagesPath+"/"+w.statusPageOfDomain, nil, w.alice).Code)
	})
}

func TestAdminWorkspaces_AStatusPageOfAWorkspaceWaitingForVerificationAnswersUnavailable(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		id, owner := a.createUnverifiedWorkspace(t, "Waiting", "wait@example.com")
		monitorID := a.createMonitorAs(t, owner, "Site", "waiting-push")
		a.createStatusPageAs(t, owner, statusPage{slug: "waiting", published: true, monitors: []string{monitorID}, domains: []string{"status.waiting.example"}})

		for _, path := range publicStatusPageRoutes("waiting", "status.waiting.example") {
			assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
		}
		for path := range badgePaths(monitorID) {
			assertUnavailable(t, a.request(t, http.MethodGet, path, nil), path)
		}

		a.verifyEmailDirectly(t, "wait@example.com")
		for _, path := range publicStatusPageRoutes("waiting", "status.waiting.example") {
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, "verifying brings it back: "+path)
		}
		_ = id
	})
}

func TestAdminWorkspaces_SuspendingTwiceKeepsTheFirstTime(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.createAcme(t)

	first := a.setSuspended(t, w.id, true)
	time.Sleep(5 * time.Millisecond)
	second := a.setSuspended(t, w.id, true)

	require.NotNil(t, first.SuspendedAt)
	assert.Equal(t, first.SuspendedAt, second.SuspendedAt)
	assert.True(t, second.Suspended)
}

// MARK: Resume

func TestAdminWorkspaces_ResumingBringsEverythingBackAsItWas(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.createAcme(t)
		member := a.loginAs(t, w.memberEmail)
		source := a.schedulerSource(t)
		var heard updates
		source.Watch(func(string) {}, heard.add, func(string) {})
		countsBefore := a.adminWorkspaceView(t, w.id)
		updatedBefore := a.monitorUpdatedAt(t, w.activeA)

		a.setSuspended(t, w.id, true)
		time.Sleep(5 * time.Millisecond)
		got := a.setSuspended(t, w.id, false)

		assert.False(t, got.Suspended)
		assert.Nil(t, got.SuspendedReason)
		assert.Nil(t, got.SuspendedAt)
		assert.Equal(t, countsBefore, got, "the monitors keep the state they had: two active, one paused")

		me := a.meOf(t, member)
		assert.False(t, me.Workspaces[0].Suspended)
		assert.Nil(t, me.Workspaces[0].SuspendedReason)
		assert.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(scheduled(t, source), w.monitorsOfWorkspace), "the paused one stays out")
		loaded, err := source.Load(context.Background(), w.activeA)
		require.NoError(t, err)
		assert.Equal(t, w.activeA, loaded.Monitor.ID)
		assert.True(t, a.monitorUpdatedAt(t, w.activeA).After(updatedBefore), "updated_at moved, so the watchdog does not count the days held back")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/"+w.pushTokenOfActiveA+"?status=up", nil).Code)
		for _, path := range publicStatusPageRoutes(w.slugWithDomain, w.domain) {
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, path)
		}
		for path := range badgePaths(w.activeA) {
			assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil).Code, path)
		}
		// the scheduler is told to pick up the active monitors, by the event it hears for any update
		require.Eventually(t, func() bool { return len(intersect(heard.seen(), w.monitorsOfWorkspace)) >= 2 }, 5*time.Second, 10*time.Millisecond)
		assert.ElementsMatch(t, w.monitorsOfWorkspace[:2], intersect(heard.seen(), w.monitorsOfWorkspace))
	})
}

func TestAdminWorkspaces_AWorkspaceWaitingForVerificationKeepsWaitingAfterTheAdminResumesIt(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		id, owner := a.createUnverifiedWorkspace(t, "Waiting", "wait@example.com")
		monitorID := a.createMonitorAs(t, owner, "Site", "waiting-push")
		a.createStatusPageAs(t, owner, statusPage{slug: "waiting", published: true, monitors: []string{monitorID}})
		source := a.schedulerSource(t)
		var heard updates
		source.Watch(func(string) {}, heard.add, func(string) {})
		reason := func() string {
			me := a.meOf(t, owner)
			require.NotNil(t, me.Workspaces[0].SuspendedReason)
			return *me.Workspaces[0].SuspendedReason
		}
		require.Equal(t, "unverified", reason())

		held := a.setSuspended(t, id, true)
		require.NotNil(t, held.SuspendedReason)
		assert.Equal(t, "admin", *held.SuspendedReason)
		assert.Equal(t, "admin", reason())

		resumed := a.setSuspended(t, id, false)
		assert.True(t, resumed.Suspended, "it still waits for the owner's verification")
		require.NotNil(t, resumed.SuspendedReason)
		assert.Equal(t, "unverified", *resumed.SuspendedReason)
		assert.Equal(t, "unverified", reason())
		assert.NotContains(t, scheduled(t, source), monitorID)
		assertUnavailable(t, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/waiting", nil), "status page")
		time.Sleep(100 * time.Millisecond)
		assert.NotContains(t, heard.seen(), monitorID, "nothing runs, so nothing is woken")

		// and the admin's hold survives the owner's verification
		a.setSuspended(t, id, true)
		a.verifyEmailDirectly(t, "wait@example.com")
		assert.Equal(t, "admin", reason())
		assert.NotContains(t, scheduled(t, source), monitorID)
		a.setSuspended(t, id, false)
		assert.False(t, a.meOf(t, owner).Workspaces[0].Suspended, "with both holds gone it runs")
		assert.Contains(t, scheduled(t, source), monitorID)
	})
}

func TestAdminWorkspaces_AnUnknownWorkspaceIs404AndABodyWithoutSuspendedIs400(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.createAcme(t)

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", "507f1f77bcf86cd799439011"} {
		assert.Equal(t, http.StatusNotFound, a.patchWorkspace(t, id, map[string]any{"suspended": true}).Code, id)
	}
	assert.Equal(t, http.StatusBadRequest, a.patchWorkspace(t, string(w.id), map[string]any{}).Code)
	assert.Equal(t, http.StatusBadRequest, a.patchWorkspace(t, string(w.id), map[string]any{"suspended": "yes"}).Code)
	assert.False(t, a.adminWorkspaceView(t, w.id).Suspended)
}

// MARK: Log

func TestAdminWorkspaces_SuspendAndResumeLogWhoDidItToWhichWorkspace(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	adminID := a.userID(t, adminEmail)
	id := a.createWorkspace(t, "Logged")

	a.setSuspended(t, id, true)
	a.setSuspended(t, id, true) // nothing changed: nothing is logged
	a.setSuspended(t, id, false)
	a.setSuspended(t, id, false)

	type line struct{ actor, action, target string }
	var got []line
	for _, entry := range logs.FilterMessage("Admin action").All() {
		fields := entry.ContextMap()
		assert.Len(t, fields, 3, "who, what and to which workspace, and nothing else")
		got = append(got, line{fields["actorUserId"].(string), fields["action"].(string), fields["targetWorkspaceId"].(string)})
	}
	assert.Equal(t, []line{{adminID, "suspend_workspace", string(id)}, {adminID, "resume_workspace", string(id)}}, got)

	// a refused change logs nothing
	logs.TakeAll()
	require.Equal(t, http.StatusNotFound, a.patchWorkspace(t, "00000000-0000-4000-8000-000000000000", map[string]any{"suspended": true}).Code)
	assert.Empty(t, logs.FilterMessage("Admin action").All())
}

// MARK: /me

func TestAdminWorkspaces_MeCarriesTheSupportContactOnlyWhenItIsSet(t *testing.T) {
	a := newAppWithAdmin(t)
	assert.Nil(t, a.meOf(t, a.asAdmin).SupportContact)

	b := newAppWithAdmin(t, func(cfg *config.Config) { cfg.SupportContact = "help@example.com" })
	got := b.meOf(t, b.asAdmin).SupportContact
	require.NotNil(t, got)
	assert.Equal(t, "help@example.com", *got)

	// blank is unset
	c := newAppWithAdmin(t, func(cfg *config.Config) { cfg.SupportContact = "  " })
	assert.Nil(t, c.meOf(t, c.asAdmin).SupportContact)
}
