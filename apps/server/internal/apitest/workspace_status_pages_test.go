package apitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A status page belongs to a workspace. The general rules of a workspace's records (create lands in
// the request's workspace, another workspace's page answers 404 on every route, lists show a
// workspace its own) are checked with the other records in workspace_records_test.go. These are the
// rules that are the page's own (PP-150, docs/adr/0005 sections 2, 6 and 8).

const statusPagesPath = "/api/v1/status-pages"

// bobsWorkspace makes a second workspace with a member, Bob, who owns it
func bobsWorkspace(t *testing.T, a *app) (workspace.ID, credentials) {
	t.Helper()
	second := a.createWorkspace(t, "Second")
	return second, a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
}

// slugOrDomainCase is a request that asks for a slug or a domain that Alice's page holds
type slugOrDomainCase struct {
	name   string
	create map[string]any
	update map[string]any
	// refusal is what the refusal says, and status the code it has
	refusal string
	status  int
}

func (a *app) createMonitorAs(t *testing.T, creds credentials, name, pushToken string) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody(name, pushToken), creds), http.StatusCreated).ID
}

func (a *app) createStatusPageAs(t *testing.T, creds credentials, page statusPage) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, statusPagesPath, map[string]any{
		"slug":        page.slug,
		"title":       "Status of " + page.slug,
		"published":   page.published,
		"monitor_ids": page.monitors,
		"domains":     page.domains,
	}, creds), http.StatusCreated).ID
}

// A page shows only the monitors of its own workspace. Naming one of another workspace gets the
// answer an ID that doesn't exist gets, and nothing is written (AC1)
func TestStatusPageWorkspaces_APageCannotShowAnotherWorkspacesMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	_, bob := bobsWorkspace(t, a)
	alices := a.createMonitor(t, "Alices", "alices-token")
	bobs := a.createMonitorAs(t, bob, "Bobs", "bobs-token")
	page := a.createStatusPageAs(t, bob, statusPage{slug: "bobs-page", published: true, monitors: []string{bobs}})
	path := statusPagesPath + "/" + page
	before := a.request(t, http.MethodGet, path, nil, bob).Body.String()
	require.Contains(t, before, bobs)

	t.Run("create", func(t *testing.T) {
		body := func(id string) map[string]any {
			return map[string]any{"slug": "doomed", "title": "Doomed", "monitor_ids": []string{bobs, id}}
		}
		w := a.request(t, http.MethodPost, statusPagesPath, body(alices), bob)
		unknown := a.request(t, http.MethodPost, statusPagesPath, body(missingRecordID), bob)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "Monitor not found")
		assert.Equal(t, unknown.Code, w.Code)
		assert.JSONEq(t, unknown.Body.String(), w.Body.String(), "the same answer as for a monitor that doesn't exist")
		assert.NotContains(t, w.Body.String(), "Alices")
		assert.Equal(t, []string{page}, idsOfList(t, a.request(t, http.MethodGet, statusPagesPath, nil, bob)), "no page is stored")
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, statusPagesPath+"/slug/doomed", nil).Code)
	})
	t.Run("update", func(t *testing.T) {
		body := func(id string) map[string]any {
			return map[string]any{"title": "Renamed", "monitor_ids": []string{bobs, id}}
		}
		w := a.request(t, http.MethodPatch, path, body(alices), bob)
		unknown := a.request(t, http.MethodPatch, path, body(missingRecordID), bob)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "Monitor not found")
		assert.Equal(t, unknown.Code, w.Code)
		assert.JSONEq(t, unknown.Body.String(), w.Body.String())
		assert.JSONEq(t, before, a.request(t, http.MethodGet, path, nil, bob).Body.String(), "the page is as it was, title and monitors")
	})
	t.Run("the other way round", func(t *testing.T) {
		w := a.request(t, http.MethodPost, statusPagesPath, map[string]any{"slug": "alices-page", "title": "Alices", "monitor_ids": []string{bobs}}, a.asAdmin)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Empty(t, idsOfList(t, a.request(t, http.MethodGet, statusPagesPath, nil, a.asAdmin)))
	})

	// the monitors of each workspace are still theirs to show
	mine := a.createStatusPage(t, statusPage{slug: "alices-own", monitors: []string{alices}})
	assert.Equal(t, []string{alices}, decode[statusPageResponse](t, a.request(t, http.MethodGet, statusPagesPath+"/"+mine, nil, a.asAdmin), http.StatusOK).MonitorIDs)
	bobsOwn := a.createMonitorAs(t, bob, "Bobs second", "bobs-second-token")
	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"monitor_ids": []string{bobsOwn}}, bob), http.StatusOK)
	assert.Equal(t, []string{bobsOwn}, decode[statusPageResponse](t, a.request(t, http.MethodGet, path, nil, bob), http.StatusOK).MonitorIDs)
}

// An ID that isn't a UUID names no monitor, and gets the answer a UUID nobody has gets, on
// PostgreSQL too, where the id column is a uuid. An upper case copy of an ID names the monitor.
func TestStatusPageWorkspaces_AMalformedMonitorIDIsAnUnknownOne(t *testing.T) {
	parallel(t)
	forEachDatabase(t, func(t *testing.T, a *app) {
		monitor := a.createMonitor(t, "Shown", "shown-token")
		for _, malformed := range []string{"not-a-uuid", "12345"} {
			body := func(id string) map[string]any {
				return map[string]any{"slug": "doomed", "title": "Doomed", "monitor_ids": []string{id}}
			}
			w := a.request(t, http.MethodPost, statusPagesPath, body(malformed), a.asAdmin)
			unknown := a.request(t, http.MethodPost, statusPagesPath, body(missingRecordID), a.asAdmin)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.JSONEq(t, unknown.Body.String(), w.Body.String())
		}

		page := a.createStatusPage(t, statusPage{slug: "upper", monitors: []string{strings.ToUpper(monitor)}})
		assert.Equal(t, []string{monitor}, decode[statusPageResponse](t, a.request(t, http.MethodGet, statusPagesPath+"/"+page, nil, a.asAdmin), http.StatusOK).MonitorIDs, "shown by the lower case ID")
		decode[any](t, a.request(t, http.MethodPatch, statusPagesPath+"/"+page, map[string]any{"monitor_ids": []string{strings.ToUpper(monitor)}}, a.asAdmin), http.StatusOK)
		assert.Equal(t, []string{monitor}, decode[statusPageResponse](t, a.request(t, http.MethodGet, statusPagesPath+"/"+page, nil, a.asAdmin), http.StatusOK).MonitorIDs)
	})
}

// A visitor needs no workspace: a published page of any workspace is found by its slug or its
// custom domain, with its monitors, and a monitor on it has its badge (AC1)
func TestStatusPageWorkspaces_PublicPagesAndBadgesWorkForEveryWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)
	_, bob := bobsWorkspace(t, a)
	alices := a.createMonitor(t, "Alices checkout", "alices-token")
	bobs := a.createMonitorAs(t, bob, "Bobs checkout", "bobs-token")
	a.createStatusPage(t, statusPage{slug: "alices-shop", published: true, monitors: []string{alices}, domains: []string{"status.alice.example"}})
	a.createStatusPageAs(t, bob, statusPage{slug: "bobs-shop", published: true, monitors: []string{bobs}, domains: []string{"Status.Bob.example"}})
	a.createStatusPageAs(t, bob, statusPage{slug: "bobs-draft", monitors: []string{bobs}, domains: []string{"draft.bob.example"}})

	for _, page := range []struct{ slug, domain, title, monitor, name string }{
		{"alices-shop", "status.alice.example", "Status of alices-shop", alices, "Alices checkout"},
		{"bobs-shop", "status.bob.example", "Status of bobs-shop", bobs, "Bobs checkout"},
	} {
		t.Run(page.slug, func(t *testing.T) {
			for _, path := range []string{statusPagesPath + "/slug/" + page.slug, statusPagesPath + "/domain/" + page.domain} {
				got := decode[struct {
					Slug  string `json:"slug"`
					Title string `json:"title"`
				}](t, a.request(t, http.MethodGet, path, nil), http.StatusOK)
				assert.Equal(t, page.slug, got.Slug, path)
				assert.Equal(t, page.title, got.Title, path)
			}
			for _, path := range []string{statusPagesPath + "/slug/" + page.slug + "/monitors", statusPagesPath + "/slug/" + page.slug + "/monitors/homepage"} {
				monitors := decode[[]publicMonitor](t, a.request(t, http.MethodGet, path, nil), http.StatusOK)
				if assert.Len(t, monitors, 1, path) {
					assert.Equal(t, page.monitor, monitors[0].ID, path)
					assert.Equal(t, page.name, monitors[0].Name, path)
				}
			}
			for path, title := range badgePaths(page.monitor) {
				assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
			}
		})
	}
	// and a page that isn't published is still hidden
	for _, path := range []string{statusPagesPath + "/slug/bobs-draft", statusPagesPath + "/domain/draft.bob.example", statusPagesPath + "/slug/bobs-draft/monitors"} {
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil).Code, path)
	}
}

func (c slugOrDomainCase) check(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if c.status != 0 {
		assert.Equal(t, c.status, w.Code, w.Body.String())
	}
	assert.Contains(t, w.Body.String(), c.refusal)
}

// A slug and a custom domain are unique across the install. One that another workspace's page holds
// is refused as taken, with the answer the same workspace's page gets, and nothing is said about the
// page that holds it (AC2)
func TestStatusPageWorkspaces_ASlugOrDomainAnotherWorkspaceHoldsIsTaken(t *testing.T) {
	a := newAppWithAdmin(t)
	_, bob := bobsWorkspace(t, a)
	a.createStatusPage(t, statusPage{slug: "alices-shop", published: true, domains: []string{"status.alice.example"}})
	bobsPage := a.createStatusPageAs(t, bob, statusPage{slug: "bobs-shop", domains: []string{"status.bob.example"}})
	alicesSecond := a.createStatusPage(t, statusPage{slug: "alices-second"})
	bobsPath := statusPagesPath + "/" + bobsPage
	bobsBefore := a.request(t, http.MethodGet, bobsPath, nil, bob).Body.String()
	bobsCount := len(idsOfList(t, a.request(t, http.MethodGet, statusPagesPath, nil, bob)))

	// the answer to the same request from the workspace that holds the value, and from the other
	sameWorkspace := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		return a.request(t, method, path, body, a.asAdmin)
	}
	crossWorkspace := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		return a.request(t, method, path, body, bob)
	}
	for _, c := range []slugOrDomainCase{
		{
			name:    "slug",
			create:  map[string]any{"slug": "alices-shop", "title": "Copy"},
			update:  map[string]any{"slug": "alices-shop"},
			refusal: `"code":"SLUG_EXISTS"`, status: http.StatusBadRequest,
		},
		{
			name:    "domain",
			create:  map[string]any{"slug": "copy-domain", "title": "Copy", "domains": []string{"status.alice.example"}},
			update:  map[string]any{"domains": []string{"STATUS.Alice.example"}},
			refusal: `"code":"DOMAIN_EXISTS"`, status: http.StatusBadRequest,
		},
	} {
		t.Run(c.name+" on create", func(t *testing.T) {
			same := sameWorkspace(http.MethodPost, statusPagesPath, c.create)
			cross := crossWorkspace(http.MethodPost, statusPagesPath, c.create)

			assert.GreaterOrEqual(t, same.Code, http.StatusBadRequest, "it is refused: "+same.Body.String())
			c.check(t, same)
			assert.Equal(t, same.Code, cross.Code, cross.Body.String())
			assert.JSONEq(t, same.Body.String(), cross.Body.String(), "the same answer")
			assert.NotContains(t, cross.Body.String(), "Alice", "and nothing of the page that holds it")
			assert.Len(t, idsOfList(t, a.request(t, http.MethodGet, statusPagesPath, nil, bob)), bobsCount, "no page is stored")
		})
		t.Run(c.name+" on update", func(t *testing.T) {
			same := sameWorkspace(http.MethodPatch, statusPagesPath+"/"+alicesSecond, c.update)
			cross := crossWorkspace(http.MethodPatch, bobsPath, c.update)

			assert.GreaterOrEqual(t, same.Code, http.StatusBadRequest, "it is refused: "+same.Body.String())
			c.check(t, same)
			assert.Equal(t, same.Code, cross.Code, cross.Body.String())
			assert.JSONEq(t, same.Body.String(), cross.Body.String(), "the same answer")
			assert.NotContains(t, cross.Body.String(), "Alice")
			if c.name == "domain" {
				assert.JSONEq(t, bobsBefore, a.request(t, http.MethodGet, bobsPath, nil, bob).Body.String(), "Bob's page keeps its domains")
			}
		})
	}
	// Alice's pages are still hers, and a value is free again once the page that held it is gone
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, statusPagesPath+"/slug/alices-shop", nil).Code)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, statusPagesPath+"/domain/status.alice.example", nil).Code)
	decode[any](t, a.request(t, http.MethodDelete, bobsPath, nil, bob), http.StatusOK)
	reused := a.createStatusPageAs(t, bob, statusPage{slug: "bobs-shop", published: true, domains: []string{"status.bob.example"}})
	assert.NotEqual(t, bobsPage, reused)
}
