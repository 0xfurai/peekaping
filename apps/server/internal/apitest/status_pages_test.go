package apitest

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publicMonitor struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Heartbeats []map[string]any `json:"heartbeats"`
}

func TestPublicStatusPage_ServesPublishedPage(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}, domains: []string{"status.shop.example"}})

	for _, path := range []string{"/api/v1/status-pages/slug/shop", "/api/v1/status-pages/domain/status.shop.example"} {
		t.Run(path, func(t *testing.T) {
			page := decode[struct {
				Slug  string `json:"slug"`
				Title string `json:"title"`
			}](t, a.request(t, http.MethodGet, path, nil), http.StatusOK)
			assert.Equal(t, "shop", page.Slug)
			assert.Equal(t, "Status of shop", page.Title)
		})
	}
	for _, path := range []string{"/api/v1/status-pages/slug/shop/monitors", "/api/v1/status-pages/slug/shop/monitors/homepage"} {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			monitors := decode[[]publicMonitor](t, w, http.StatusOK)
			if assert.Len(t, monitors, 1) {
				assert.Equal(t, monitorID, monitors[0].ID)
				assert.Equal(t, "Checkout", monitors[0].Name)
			}
			assert.NotContains(t, w.Body.String(), "checkout-push-token")
		})
	}
}

func TestPublicStatusPage_HidesUnpublishedPage(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Internal billing", "billing-push-token")
	a.createStatusPage(t, statusPage{slug: "draft", monitors: []string{monitorID}, domains: []string{"status.draft.example"}})

	for _, path := range []string{
		"/api/v1/status-pages/slug/draft",
		"/api/v1/status-pages/domain/status.draft.example",
		"/api/v1/status-pages/slug/draft/monitors",
		"/api/v1/status-pages/slug/draft/monitors/homepage",
	} {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.NotContains(t, w.Body.String(), "Internal billing")
			assert.NotContains(t, w.Body.String(), "draft")
		})
	}
}

type statusPageResponse struct {
	ID         string   `json:"id"`
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Published  bool     `json:"published"`
	MonitorIDs []string `json:"monitor_ids"`
	Domains    []string `json:"domains"`
}

func TestStatusPagesAPI_ManagesStatusPage(t *testing.T) {
	a := newAppWithAdmin(t)
	checkout := a.createMonitor(t, "Checkout", "checkout-push-token")
	search := a.createMonitor(t, "Search", "search-push-token")
	id := a.createStatusPage(t, statusPage{slug: "shop", monitors: []string{checkout}, domains: []string{"status.shop.example"}})
	path := "/api/v1/status-pages/" + id

	got := decode[statusPageResponse](t, a.request(t, http.MethodGet, path, nil, a.withAPIKey), http.StatusOK)
	assert.Equal(t, statusPageResponse{ID: id, Slug: "shop", Title: "Status of shop", MonitorIDs: []string{checkout}, Domains: []string{"status.shop.example"}}, got)
	list := decode[[]statusPageResponse](t, a.request(t, http.MethodGet, "/api/v1/status-pages", nil, a.asAdmin), http.StatusOK)
	if assert.Len(t, list, 1) {
		assert.Equal(t, id, list[0].ID)
	}

	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{
		"title":       "Shop status",
		"published":   true,
		"monitor_ids": []string{search},
		"domains":     []string{"status.shop.example", "shop-status.example"},
	}, a.asAdmin), http.StatusOK)
	got = decode[statusPageResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Shop status", got.Title)
	assert.True(t, got.Published)
	assert.Equal(t, []string{search}, got.MonitorIDs)
	assert.ElementsMatch(t, []string{"status.shop.example", "shop-status.example"}, got.Domains)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/shop", nil).Code)
}

func TestStatusPagesAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	shop := a.createStatusPage(t, statusPage{slug: "shop", domains: []string{"status.shop.example"}})
	blog := a.createStatusPage(t, statusPage{slug: "blog"})

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/status-pages", "{"},
		{"slug too short", http.MethodPost, "/api/v1/status-pages", map[string]any{"slug": "ab", "title": "Status"}},
		{"no title", http.MethodPost, "/api/v1/status-pages", map[string]any{"slug": "docs"}},
		{"domain on another page", http.MethodPost, "/api/v1/status-pages", map[string]any{"slug": "docs", "title": "Docs", "domains": []string{"status.shop.example"}}},
		{"update with malformed JSON", http.MethodPatch, "/api/v1/status-pages/" + blog, "{"},
		{"update to a domain on another page", http.MethodPatch, "/api/v1/status-pages/" + blog, map[string]any{"domains": []string{"status.shop.example"}}},
		{"negative page", http.MethodGet, "/api/v1/status-pages?page=-1", nil},
	})

	list := decode[[]statusPageResponse](t, a.request(t, http.MethodGet, "/api/v1/status-pages", nil, a.asAdmin), http.StatusOK)
	assert.Len(t, list, 2)
	got := decode[statusPageResponse](t, a.request(t, http.MethodGet, "/api/v1/status-pages/"+shop, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{"status.shop.example"}, got.Domains)
	got = decode[statusPageResponse](t, a.request(t, http.MethodGet, "/api/v1/status-pages/"+blog, nil, a.asAdmin), http.StatusOK)
	assert.Empty(t, got.Domains)
}

func TestStatusPagesAPI_CustomDomainsAreLowerCaseAndFoundByAnyCase(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{" Status.Shop.EXAMPLE "}})
	blog := a.createStatusPage(t, statusPage{slug: "blog"})

	got := decode[statusPageResponse](t, a.request(t, http.MethodGet, "/api/v1/status-pages/"+id, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, []string{"status.shop.example"}, got.Domains)
	for _, domain := range []string{"status.shop.example", "STATUS.shop.example"} {
		page := decode[struct {
			Slug string `json:"slug"`
		}](t, a.request(t, http.MethodGet, "/api/v1/status-pages/domain/"+domain, nil), http.StatusOK)
		assert.Equal(t, "shop", page.Slug, domain)
	}

	// Another page can't have it in any case
	a.expectStatus(t, http.StatusBadRequest, []call{
		{"create", http.MethodPost, "/api/v1/status-pages", map[string]any{"slug": "docs", "title": "Docs", "domains": []string{"STATUS.shop.example"}}},
		{"update", http.MethodPatch, "/api/v1/status-pages/" + blog, map[string]any{"domains": []string{"Status.Shop.Example"}}},
	})
}

// Caddy's on_demand_tls asks this before it gets a certificate for a host (PP-332). It has no
// login, so it is a yes or no and nothing else.
func TestStatusPagesAPI_TLSAskAnswersForListedDomains(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})
	a.createStatusPage(t, statusPage{slug: "draft", published: false, domains: []string{"Draft.Shop.Example"}})

	listed := map[string]string{
		"a published page's domain":                 "status.shop.example",
		"an unpublished page's domain":              "draft.shop.example",
		"the domain in upper case":                  "STATUS.Shop.EXAMPLE",
		"the domain with a trailing dot":            "status.shop.example.",
		"an unpublished domain with a trailing dot": "DRAFT.shop.example.",
		"the domain with spaces around it":          " status.shop.example ",
	}
	for name, host := range listed {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodGet, "/api/v1/status-pages/tls-ask?domain="+url.QueryEscape(host), nil)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Empty(t, w.Body.String())
		})
	}
}

func TestStatusPagesAPI_TLSAskRefusesOtherHosts(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})

	for name, host := range map[string]string{
		"an unknown host":                     "unknown.example",
		"a parent of a listed domain":         "shop.example",
		"a subdomain of a listed domain":      "www.status.shop.example",
		"two trailing dots":                   "status.shop.example..",
		"the slug of a page":                  "shop",
		"an empty host":                       "",
		"a host that only has a trailing dot": ".",
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodGet, "/api/v1/status-pages/tls-ask?domain="+url.QueryEscape(host), nil)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), "shop")
		})
	}

	t.Run("no domain parameter", func(t *testing.T) {
		w := a.request(t, http.MethodGet, "/api/v1/status-pages/tls-ask", nil)
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
}

// A domain is saved as it was written, so one with a trailing dot stays that way (ADR 0006 / PP-335
// will normalize saves). The ask finds it whether the host is asked with the dot or without.
func TestStatusPagesAPI_TLSAskFindsADomainSavedWithATrailingDot(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.example.com."}})

	for _, host := range []string{"status.example.com", "status.example.com."} {
		w := a.request(t, http.MethodGet, "/api/v1/status-pages/tls-ask?domain="+url.QueryEscape(host), nil)
		assert.Equal(t, http.StatusOK, w.Code, host)
	}
	w := a.request(t, http.MethodGet, "/api/v1/status-pages/tls-ask?domain="+url.QueryEscape("status.example.com.."), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStatusPagesAPI_TLSAskForgetsARemovedDomain(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})
	ask := "/api/v1/status-pages/tls-ask?domain=status.shop.example"
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, ask, nil).Code)

	a.expectStatus(t, http.StatusOK, []call{{"remove", http.MethodPatch, "/api/v1/status-pages/" + id, map[string]any{"domains": []string{}}}})
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, ask, nil).Code)
}
