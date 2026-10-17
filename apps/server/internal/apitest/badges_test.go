package apitest

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

var badgeTitle = regexp.MustCompile(`<title>(.*?)</title>`)

// badgePaths are every badge route for a monitor, with the badge each one
// renders for a monitor that's Up and has no heartbeats yet.
func badgePaths(monitorID string) map[string]string {
	base := "/api/v1/badge/" + monitorID
	return map[string]string{
		base + "/status":     "status: Up",
		base + "/uptime":     "uptime (24h): 0.0%",
		base + "/uptime/720": "uptime (30d): 0.0%",
		base + "/ping":       "ping (24h): 0ms",
		base + "/ping/2160":  "ping (90d): 0ms",
		base + "/cert-exp":   "cert exp: N/A",
		base + "/response":   "response: N/A",
	}
}

func TestBadges_ServeMonitorOnPublishedPage(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}})

	for path, title := range badgePaths(monitorID) {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
			if match := badgeTitle.FindStringSubmatch(w.Body.String()); assert.NotNil(t, match, w.Body.String()) {
				assert.Equal(t, title, match[1])
			}
		})
	}
}

func TestBadges_HideMonitorsOffPublishedPages(t *testing.T) {
	a := newAppWithAdmin(t)
	draftOnly := a.createMonitor(t, "On a draft page", "draft-push-token")
	a.createStatusPage(t, statusPage{slug: "draft", monitors: []string{draftOnly}})
	paused := a.createMonitor(t, "Paused", "paused-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{paused}})
	a.pauseMonitor(t, paused)
	unlisted := a.createMonitor(t, "On no page", "unlisted-push-token")

	monitors := map[string]string{
		"only on an unpublished page": draftOnly,
		"paused":                      paused,
		"on no status page":           unlisted,
		"missing":                     "missing",
	}
	for name, monitorID := range monitors {
		for path := range badgePaths(monitorID) {
			t.Run(name+path, func(t *testing.T) {
				w := a.request(t, http.MethodGet, path, nil)
				assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
				assert.NotContains(t, w.Body.String(), "<svg")
			})
		}
	}
}

func TestBadges_FallBackOnInvalidOptions(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}})
	base := "/api/v1/badge/" + monitorID

	for path, title := range map[string]string{
		base + "/uptime/not-a-number":                "uptime (24h): 0.0%",
		base + "/ping/-720":                          "ping (24h): 0ms",
		base + "/status?style=unknown":               "status: Up",
		base + "/cert-exp?warnDays=soon&downDays=-1": "cert exp: N/A",
	} {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if match := badgeTitle.FindStringSubmatch(w.Body.String()); assert.NotNil(t, match, w.Body.String()) {
				assert.Equal(t, title, match[1])
			}
		})
	}
}
