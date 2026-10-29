package apitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/limits"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-160: the limits of a workspace beyond its monitors: status pages, custom domains, members,
// the workspaces a user owns, what GET /workspace/limits reports, and the admin's own limits.

const workspaceLimitsPath = "/api/v1/workspace/limits"

// withLimits sets the install's defaults, the WORKSPACE_LIMIT_* variables
func withLimits(set func(cfg *config.Config)) func(*config.Config) { return set }

// postPage creates a status page with the domains as the admin
func (a *app) postPage(t *testing.T, slug string, domains ...string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, statusPagesPath, map[string]any{"slug": slug, "title": "Status of " + slug, "domains": domains}, a.asAdmin)
}

// MARK: Status pages

// AC4: a status page create while the workspace has as many as its limit is refused, from the
// install's defaults and from the workspace's own limits, at the line and above it
func TestLimits_StatusPages_ACreateAtOrAboveTheLimitIsRefused(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitStatusPages = 2 }))

		decode[created](t, a.postPage(t, "one"), http.StatusCreated)
		decode[created](t, a.postPage(t, "two"), http.StatusCreated)
		w := a.postPage(t, "three")

		assertLimitReached(t, w, "status_pages", 2)
		assert.Equal(t, 2, a.count(t, "status_pages"), "the refused create stored nothing")

		// The workspace's own limits replace the install's: above the line is refused too, and a
		// higher one lets the next in
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{StatusPages: limitOf(1)})
		assertLimitReached(t, a.postPage(t, "three"), "status_pages", 1)
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{StatusPages: limitOf(3)})
		decode[created](t, a.postPage(t, "three"), http.StatusCreated)
	})
}

// MARK: Custom domains

// AC4: custom domains count across every status page of the workspace, on create and on update
func TestLimits_CustomDomains_CountAcrossTheWorkspacesPagesOnCreateAndUpdate(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitCustomDomains = 2 }))
		decode[created](t, a.postPage(t, "first", "one.example.com"), http.StatusCreated)

		assertLimitReached(t, a.postPage(t, "second", "two.example.com", "three.example.com"), "custom_domains", 2)
		assert.Equal(t, 1, a.count(t, "status_pages"), "the refused page is not created")
		second := decode[created](t, a.postPage(t, "second", "two.example.com"), http.StatusCreated).ID
		decode[created](t, a.postPage(t, "third"), http.StatusCreated) // a page without domains adds none

		path := statusPagesPath + "/" + second
		assertLimitReached(t, a.request(t, http.MethodPatch, path, map[string]any{"domains": []string{"two.example.com", "three.example.com"}}, a.asAdmin), "custom_domains", 2)
		assert.ElementsMatch(t, []string{"two.example.com"}, a.pageDomains(t, second), "the refused update changed nothing")

		// Swapping a domain for another keeps the count, and is not refused
		decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"domains": []string{"four.example.com"}}, a.asAdmin), http.StatusOK)
		assert.ElementsMatch(t, []string{"four.example.com"}, a.pageDomains(t, second))
		// Other edits of a page at the limit are not refused
		decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"title": "Renamed", "domains": []string{"four.example.com"}}, a.asAdmin), http.StatusOK)

		// Above the line, set by the workspace's own limits: removing is allowed, adding is not
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{CustomDomains: limitOf(1)})
		assertLimitReached(t, a.request(t, http.MethodPatch, path, map[string]any{"domains": []string{"four.example.com", "five.example.com"}}, a.asAdmin), "custom_domains", 1)
		decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"domains": []string{}}, a.asAdmin), http.StatusOK)
		assert.Empty(t, a.pageDomains(t, second))
	})
}

func (a *app) pageDomains(t *testing.T, id string) []string {
	t.Helper()
	return decode[struct {
		Domains []string `json:"domains"`
	}](t, a.request(t, http.MethodGet, statusPagesPath+"/"+id, nil, a.asAdmin), http.StatusOK).Domains
}

// MARK: Members

func withMemberLimit(n int) func(*config.Config) {
	return func(cfg *config.Config) { cfg.WorkspaceLimitMembers = n }
}

// AC4: the members limit counts the members and the pending invites when an invite is made
func TestLimits_Members_AnInviteCountsThePendingOnesBesideTheMembers(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withMemberLimit(3))
		a.addMember(t, a.firstWorkspace(t), a.createUser(t, "bob@example.com"), workspace.RoleMember)
		a.invite(t, "carol@example.com", "member", a.asAdmin)

		w := a.request(t, http.MethodPost, invitesPath, map[string]any{"email": "dave@example.com", "role": "member"}, a.asAdmin)

		assertLimitReached(t, w, "members", 3)
		assert.Len(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK), 1, "the refused invite stored nothing")
		// Inviting the pending address again replaces its invite, and counts it once
		a.invite(t, "carol@example.com", "admin", a.asAdmin)
		// An expired invite can't be accepted, so it doesn't count
		again := decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK)
		require.Len(t, again, 1)
		a.expire(t, again[0].ID)
		a.invite(t, "dave@example.com", "member", a.asAdmin)
	})
}

// AC4: an invite that was counted when it was made can be accepted at the limit, by a signed-in
// user and by a signup through it; once the workspace has as many members as its limit, it can't
func TestLimits_Members_ACountedInviteIsAcceptedAndOneOverTheLimitIsNot(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withMemberLimit(3))
		a.createUser(t, "bob@example.com")
		bob := a.invite(t, "bob@example.com", "member", a.asAdmin)
		carol := a.invite(t, "carol@example.com", "member", a.asAdmin)

		decode[workspaceResponse](t, a.accept(t, bob.Token, a.loginAs(t, "bob@example.com")), http.StatusOK)
		require.Equal(t, http.StatusCreated, a.signUpThrough(t, carol.Token, "carol@example.com").Code, "the limit itself is reached by the counted invites")

		// Above the line: invites made before the workspace's own limit was lowered can't be used
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{Members: limitOf(5)})
		dave := a.invite(t, "dave@example.com", "member", a.asAdmin)
		a.createUser(t, "erin@example.com")
		erin := a.invite(t, "erin@example.com", "member", a.asAdmin)
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{Members: limitOf(3)})

		assertLimitReached(t, a.accept(t, erin.Token, a.loginAs(t, "erin@example.com")), "members", 3)
		assertLimitReached(t, a.signUpThrough(t, dave.Token, "dave@example.com"), "members", 3)
		assert.Zero(t, a.countWhere(t, "users", "email = ?", "dave@example.com"), "the refused signup made no account")
		assert.Equal(t, "pending", a.preview(t, erin.Token).Status, "a refused accept leaves the invite usable")
	})
}

func (a *app) countWhere(t *testing.T, table, where string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		var err error
		n, err = db.NewSelect().Table(table).Where(where, args...).Count(context.Background())
		return err
	}))
	return n
}

// MARK: Owned workspaces

// AC4: the workspaces a user owns are counted when they create one, against the install's
// defaults: the new workspace has no limits of its own yet
func TestLimits_OwnedWorkspaces_ACreateAtTheUsersLimitIsRefused(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitOwnedWorkspaces = 2 }))
		create := func(creds credentials) *httpRecorder {
			return a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "Side project"}, creds)
		}

		decode[workspaceResponse](t, create(a.asAdmin), http.StatusCreated)
		assertLimitReached(t, create(a.asAdmin), "owned_workspaces", 2)
		assert.Equal(t, 2, a.count(t, "workspaces"), "the refused create stored nothing")

		// The limit is the user's: one who is only a member of the admin's workspaces owns none
		member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)
		decode[workspaceResponse](t, create(member), http.StatusCreated)
		decode[workspaceResponse](t, create(member), http.StatusCreated)
		assertLimitReached(t, create(member), "owned_workspaces", 2)

		// A workspace's own limits don't decide it
		a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{OwnedWorkspaces: limitOf(10)})
		assertLimitReached(t, create(a.asAdmin), "owned_workspaces", 2)
	})
}

// MARK: Two at once

// countDelay is how long a count of the race tests takes
const countDelay = 250 * time.Millisecond

type slowPages struct{ status_page.Repository }

func (r slowPages) CountPages(ctx context.Context, id workspace.ID) (int, error) {
	time.Sleep(countDelay)
	return r.Repository.CountPages(ctx, id)
}

func (r slowPages) CountDomains(ctx context.Context, id workspace.ID) (int, error) {
	time.Sleep(countDelay)
	return r.Repository.CountDomains(ctx, id)
}

type slowMembers struct{ workspace.MemberRepository }

func (r slowMembers) CountMembers(ctx context.Context, id workspace.ID) (int, error) {
	time.Sleep(countDelay)
	return r.MemberRepository.CountMembers(ctx, id)
}

// AC5: two writes at once at one below the limit end with one of them refused
func TestLimits_TwoCreatesAtOnceBelowTheLimitStoreOne(t *testing.T) {
	backends := map[string]func(*config.Config){"SQLite": func(*config.Config) {}}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends["PostgreSQL"] = usePostgres(t, dsn)
	}
	// race sends the requests at once: of the creates one below the limit, one is stored
	const racers = 6
	race := func(t *testing.T, send func(i int) int) {
		codes := make([]int, racers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i] = send(i)
			}()
		}
		close(start)
		wg.Wait()
		created, refused := 0, 0
		for _, code := range codes {
			switch code {
			case http.StatusCreated:
				created++
			case http.StatusForbidden:
				refused++
			}
		}
		assert.Equal(t, 1, created, "%v", codes)
		assert.Equal(t, racers-1, refused, "%v", codes)
	}
	// newApp holds every count a moment, so that writes at once all count before any of them writes,
	// as they would if nothing held the workspace
	newApp := func(t *testing.T, options ...func(*config.Config)) *app {
		a := newAppDecorated(t, func(container *dig.Container) error {
			return errors.Join(
				container.Decorate(func(r status_page.Repository) status_page.Repository { return slowPages{r} }),
				container.Decorate(func(r workspace.MemberRepository) workspace.MemberRepository { return slowMembers{r} }),
			)
		}, options...)
		a.registerAdmin(t)
		return a
	}
	for name, backend := range backends {
		t.Run(name+"/status pages", func(t *testing.T) {
			a := newApp(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitStatusPages = 3 }))
			decode[created](t, a.postPage(t, "one"), http.StatusCreated)
			decode[created](t, a.postPage(t, "two"), http.StatusCreated)

			race(t, func(i int) int { return a.postPage(t, fmt.Sprintf("racer-%d", i)).Code })
			assert.Equal(t, 3, a.count(t, "status_pages"))
		})
		t.Run(name+"/custom domains", func(t *testing.T) {
			a := newApp(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitCustomDomains = 1 }))

			race(t, func(i int) int {
				return a.postPage(t, fmt.Sprintf("racer-%d", i), fmt.Sprintf("racer-%d.example.com", i)).Code
			})
			assert.Equal(t, 1, a.count(t, "domain_status_page"))
		})
		t.Run(name+"/members", func(t *testing.T) {
			a := newApp(t, backend, withMemberLimit(2))

			race(t, func(i int) int {
				return a.request(t, http.MethodPost, invitesPath, map[string]any{"email": fmt.Sprintf("racer-%d@example.com", i), "role": "member"}, a.asAdmin).Code
			})
			assert.Len(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK), 1)
		})
	}
}

// AC1: an install that sets nothing refuses none of these
func TestLimits_WithNothingSetNoneOfTheseIsRefused(t *testing.T) {
	a := newAppWithAdmin(t)
	for i := range 12 {
		decode[created](t, a.postPage(t, fmt.Sprintf("page-%d", i), fmt.Sprintf("a-%d.example.com", i), fmt.Sprintf("b-%d.example.com", i)), http.StatusCreated)
		a.invite(t, fmt.Sprintf("invitee-%d@example.com", i), "member", a.asAdmin)
		decode[workspaceResponse](t, a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "Side project"}, a.asAdmin), http.StatusCreated)
	}
	a.createUser(t, "late@example.com")
	late := a.invite(t, "late@example.com", "member", a.asAdmin)
	decode[workspaceResponse](t, a.accept(t, late.Token, a.loginAs(t, "late@example.com")), http.StatusOK)
}

// MARK: GET /workspace/limits

type limitUsage struct {
	Max    *int   `json:"max"`
	Used   int    `json:"used"`
	Source string `json:"source"`
}

type limitsReport struct {
	Source string                `json:"source"`
	Limits map[string]limitUsage `json:"limits"`
}

var everyLimit = []string{"monitors", "min_check_interval", "status_pages", "custom_domains", "members", "owned_workspaces", "probes_per_monitor", "retention_days"}

// AC6: with nothing set every limit is null, and the usage is what the workspace has
func TestLimits_Report_WithNothingSetEveryLimitIsNullAndTheUsageIsShown(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createHTTPMonitor(t, "One", "")
	a.createHTTPMonitor(t, "Two", "")
	a.pauseMonitor(t, a.createHTTPMonitor(t, "Paused", ""))
	decode[created](t, a.postPage(t, "one", "one.example.com", "two.example.com"), http.StatusCreated)
	decode[created](t, a.postPage(t, "two", "three.example.com"), http.StatusCreated)
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)
	a.invite(t, "pending@example.com", "member", a.asAdmin)
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Two probes", ""), "local", frankfurt.ID), a.asAdmin), http.StatusCreated)

	report := decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, member), http.StatusOK)

	assert.Equal(t, "none", report.Source)
	assert.ElementsMatch(t, everyLimit, keys(report.Limits))
	for name, l := range report.Limits {
		assert.Nil(t, l.Max, name)
		assert.Equal(t, "none", l.Source, name)
	}
	assert.Equal(t, 3, report.Limits["monitors"].Used, "the active monitors")
	assert.Equal(t, 2, report.Limits["status_pages"].Used)
	assert.Equal(t, 3, report.Limits["custom_domains"].Used, "across the pages")
	assert.Equal(t, 3, report.Limits["members"].Used, "the members and the pending invite")
	assert.Equal(t, 0, report.Limits["owned_workspaces"].Used, "the member owns none")
	assert.Equal(t, 2, report.Limits["probes_per_monitor"].Used, "the most any monitor runs on")
	assert.Equal(t, config.AbsoluteMinCheckInterval, report.Limits["min_check_interval"].Used, "the minimum that applies")
	assert.Equal(t, 180, report.Limits["retention_days"].Used, "the workspace's data retention period")

	asAdmin := decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, 1, asAdmin.Limits["owned_workspaces"].Used, "the caller's own workspaces")
	byKey := decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.withAPIKey), http.StatusOK)
	assert.Equal(t, 0, byKey.Limits["owned_workspaces"].Used, "an API key owns nothing")
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, workspaceLimitsPath, nil).Code)
}

// AC6: each limit's value comes with where it comes from: the install's defaults, else the
// workspace's own; the owned workspaces always come from the install
func TestLimits_Report_ShowsEachLimitAndItsSource(t *testing.T) {
	a := newAppWithAdmin(t, withMinCheckInterval(30), withLimits(func(cfg *config.Config) {
		cfg.WorkspaceLimitMonitors = 10
		cfg.WorkspaceLimitMinCheckInterval = 60
		cfg.WorkspaceLimitOwnedWorkspaces = 3
		cfg.WorkspaceLimitRetentionDays = 30
	}))

	report := decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "install", report.Source)
	assert.Equal(t, limitOf(10), report.Limits["monitors"].Max)
	assert.Equal(t, "install", report.Limits["monitors"].Source)
	assert.Nil(t, report.Limits["status_pages"].Max, "a default the install doesn't set is no limit")
	assert.Equal(t, limitOf(60), report.Limits["min_check_interval"].Max)
	assert.Equal(t, 60, report.Limits["min_check_interval"].Used, "the stricter of the install's and the workspace's")
	assert.Equal(t, limitOf(3), report.Limits["owned_workspaces"].Max)
	assert.Equal(t, limitOf(30), report.Limits["retention_days"].Max)

	a.setOwnLimits(t, a.firstWorkspace(t), &limits.Limits{StatusPages: limitOf(1)})
	report = decode[limitsReport](t, a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "workspace", report.Source)
	assert.Equal(t, limitOf(1), report.Limits["status_pages"].Max)
	assert.Equal(t, "workspace", report.Limits["status_pages"].Source)
	assert.Nil(t, report.Limits["monitors"].Max, "the workspace's own limits replace the defaults whole")
	assert.Equal(t, 30, report.Limits["min_check_interval"].Used, "the install's minimum alone")
	assert.Equal(t, limitOf(3), report.Limits["owned_workspaces"].Max, "a user's workspaces are counted against the install's")
	assert.Equal(t, "install", report.Limits["owned_workspaces"].Source)
}

// MARK: Admin

// adminLimits is the limits of a workspace as the admin list shows them
type adminLimits struct {
	Source string          `json:"source"`
	Values map[string]*int `json:"values"`
}

func (a *app) adminLimitsOf(t *testing.T, id workspace.ID) adminLimits {
	t.Helper()
	for _, w := range decode[struct {
		Workspaces []struct {
			ID     string      `json:"id"`
			Limits adminLimits `json:"limits"`
		} `json:"workspaces"`
	}](t, a.request(t, http.MethodGet, adminWorkspacesPath+"?limit=100", nil, a.asAdmin), http.StatusOK).Workspaces {
		if w.ID == string(id) {
			return w.Limits
		}
	}
	t.Fatalf("the admin list does not show workspace %s", id)
	return adminLimits{}
}

// AC6: an instance admin sets a workspace's own limits, makes it unlimited with {}, and clears them
// back to the install's defaults; the list shows each, and the services enforce it
func TestLimits_Admin_SetsUnlimitsAndClearsAWorkspacesOwnLimits(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, withLimits(func(cfg *config.Config) { cfg.WorkspaceLimitStatusPages = 1 }))
		id := a.firstWorkspace(t)
		listed := a.adminLimitsOf(t, id)
		assert.Equal(t, "install", listed.Source)
		assert.ElementsMatch(t, everyLimit, keys(listed.Values), "every limit, null for none")
		assert.Equal(t, limitOf(1), listed.Values["status_pages"])
		assert.Nil(t, listed.Values["monitors"])

		set := decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"limits": map[string]any{"status_pages": 2, "members": 5}}), http.StatusOK)
		assert.Equal(t, string(id), set.ID, "the answer is the workspace")
		listed = a.adminLimitsOf(t, id)
		assert.Equal(t, "workspace", listed.Source)
		assert.Equal(t, limitOf(2), listed.Values["status_pages"])
		assert.Equal(t, limitOf(5), listed.Values["members"])
		decode[created](t, a.postPage(t, "one"), http.StatusCreated)
		decode[created](t, a.postPage(t, "two"), http.StatusCreated)
		assertLimitReached(t, a.postPage(t, "three"), "status_pages", 2)

		decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"limits": map[string]any{}}), http.StatusOK)
		listed = a.adminLimitsOf(t, id)
		assert.Equal(t, "workspace", listed.Source, "no limits is a value of its own")
		for name, value := range listed.Values {
			assert.Nil(t, value, name)
		}
		decode[created](t, a.postPage(t, "three"), http.StatusCreated)

		decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"clearLimits": true}), http.StatusOK)
		listed = a.adminLimitsOf(t, id)
		assert.Equal(t, "install", listed.Source, "back to the install's defaults")
		assert.Equal(t, limitOf(1), listed.Values["status_pages"])
		assertLimitReached(t, a.postPage(t, "four"), "status_pages", 1)

		// The suspension still works beside it, and both in one request
		both := decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"suspended": true, "limits": map[string]any{"monitors": 3}}), http.StatusOK)
		assert.True(t, both.Suspended)
		assert.Equal(t, limitOf(3), a.adminLimitsOf(t, id).Values["monitors"])
	})
}

func TestLimits_Admin_ABadChangeIsRefusedAndOnlyAnInstanceAdminMakesOne(t *testing.T) {
	a := newAppWithAdmin(t)
	id := string(a.firstWorkspace(t))
	for name, body := range map[string]map[string]any{
		"nothing named":         {},
		"a negative limit":      {"limits": map[string]any{"monitors": -1}},
		"setting and clearing":  {"limits": map[string]any{"monitors": 1}, "clearLimits": true},
		"clearLimits not true":  {"clearLimits": false},
		"limits that are null":  {"limits": nil},
		"a limit that's a word": {"limits": map[string]any{"monitors": "ten"}},
	} {
		assert.Equal(t, http.StatusBadRequest, a.patchWorkspace(t, id, body).Code, name)
	}
	assert.Equal(t, "none", a.adminLimitsOf(t, workspace.ID(id)).Source, "nothing was stored")
	assert.Equal(t, http.StatusNotFound, a.patchWorkspace(t, "00000000-0000-4000-8000-000000000000", map[string]any{"limits": map[string]any{}}).Code)

	owner := a.userIn(t, "owner@example.com", workspace.ID(id), workspace.RoleOwner)
	for name, creds := range map[string]credentials{"an owner": owner, "an API key": a.withAPIKey} {
		assert.Equal(t, http.StatusForbidden, a.patchWorkspace(t, id, map[string]any{"limits": map[string]any{}}, creds).Code, name)
		assert.Equal(t, http.StatusForbidden, a.patchWorkspace(t, id, map[string]any{"clearLimits": true}, creds).Code, name)
	}
	assert.Equal(t, "none", a.adminLimitsOf(t, workspace.ID(id)).Source)
}

func TestLimits_Admin_ASetOrClearIsLoggedWithWhoDidItToWhichWorkspace(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	adminID := a.userID(t, adminEmail)
	id := a.createWorkspace(t, "Logged")
	logs.TakeAll()

	decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"limits": map[string]any{"monitors": 3}}), http.StatusOK)
	decode[adminWorkspace](t, a.patchWorkspace(t, string(id), map[string]any{"clearLimits": true}), http.StatusOK)

	type line struct{ actor, action, target string }
	var got []line
	for _, entry := range logs.FilterMessage("Admin action").All() {
		fields := entry.ContextMap()
		assert.Len(t, fields, 3, "who, what and to which workspace, and nothing else")
		got = append(got, line{fields["actorUserId"].(string), fields["action"].(string), fields["targetWorkspaceId"].(string)})
	}
	assert.Equal(t, []line{{adminID, "set_workspace_limits", string(id)}, {adminID, "clear_workspace_limits", string(id)}}, got)

	logs.TakeAll()
	require.Equal(t, http.StatusNotFound, a.patchWorkspace(t, "00000000-0000-4000-8000-000000000000", map[string]any{"limits": map[string]any{}}).Code)
	assert.Empty(t, logs.FilterMessage("Admin action").All(), "a refused change logs nothing")
}

// The report is JSON with every limit present, null for none
func TestLimits_Report_NullIsWrittenForNoLimit(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.request(t, http.MethodGet, workspaceLimitsPath, nil, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw struct {
		Data struct {
			Limits map[string]map[string]json.RawMessage `json:"limits"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	for name, l := range raw.Data.Limits {
		assert.Equal(t, "null", string(l["max"]), name)
		assert.ElementsMatch(t, []string{"max", "used", "source"}, keys(l), name)
	}
}
