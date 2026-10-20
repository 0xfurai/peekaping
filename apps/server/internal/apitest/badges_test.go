package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

var badgeTitle = regexp.MustCompile(`<title>(.*?)</title>`)

// assertBadge checks that the response is an SVG badge reading title.
func assertBadge(t *testing.T, w *httptest.ResponseRecorder, title string) {
	t.Helper()
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	if match := badgeTitle.FindStringSubmatch(w.Body.String()); assert.NotNil(t, match, w.Body.String()) {
		assert.Equal(t, title, match[1])
	}
}

// badgePaths are every badge route for a monitor, with the badge each one
// renders for a monitor that's Pending and has no heartbeats yet.
func badgePaths(monitorID string) map[string]string {
	base := "/api/v1/badge/" + monitorID
	return map[string]string{
		base + "/status":     "status: Pending",
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
	// A draft page listing the monitor too doesn't hide it
	a.createStatusPage(t, statusPage{slug: "draft", monitors: []string{monitorID}})

	for path, title := range badgePaths(monitorID) {
		t.Run(path, func(t *testing.T) {
			assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
		})
	}
}

// A badge URL made on a MongoDB install names its monitor by ObjectID. After the
// install is imported, the monitor's row has the UUID that ObjectID maps to.
func TestBadges_ServeImportedMonitorByObjectID(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}})
	// The UUID computed outside Go for this ObjectID
	const objectID, importedID = "507f1f77bcf86cd799439011", "2ec0f307-6408-57cd-b5a8-08c55d4ab445"
	a.rekeyMonitor(t, monitorID, importedID)

	for _, id := range []string{objectID, importedID} {
		for path, title := range badgePaths(id) {
			t.Run(path, func(t *testing.T) {
				assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
			})
		}
	}

	// An ObjectID that maps to no monitor is as missing as any other ID
	for path := range badgePaths("ffffffffffffffffffffffff") {
		w := a.request(t, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, w.Code, path)
	}
}

// rekeyMonitor gives a monitor and its status page links another ID, the way
// the import writes an ObjectID's UUID.
func (a *app) rekeyMonitor(t *testing.T, from, to string) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		// The tables reference the monitor, so its ID changes in both at once
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		for table, column := range map[string]string{"monitors": "id", "monitor_status_pages": "monitor_id"} {
			if _, err := conn.ExecContext(ctx, "UPDATE "+table+" SET "+column+" = ? WHERE "+column+" = ?", to, from); err != nil {
				return err
			}
		}
		_, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
		return err
	}))
}

func TestBadges_StatusStaysDownAfterTheMonitorIsEdited(t *testing.T) {
	a := newAppWithMonitorListener(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}})
	a.reportStatus(t, monitorID, shared.MonitorStatusDown)

	w := a.request(t, http.MethodPut, "/api/v1/monitors/"+monitorID, pushMonitorBody("Checkout API", "checkout-push-token"), a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assertBadge(t, a.request(t, http.MethodGet, "/api/v1/badge/"+monitorID+"/status", nil), "status: Down")
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
		base + "/status?style=unknown":               "status: Pending",
		base + "/cert-exp?warnDays=soon&downDays=-1": "cert exp: N/A",
	} {
		t.Run(path, func(t *testing.T) {
			assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
		})
	}
}
