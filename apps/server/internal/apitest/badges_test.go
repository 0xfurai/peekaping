package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"peekaping/internal/modules/monitor_tls_info"
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

// storeCertExpiry gives a monitor a certificate with the days left, as a check of it would.
func (a *app) storeCertExpiry(t *testing.T, monitorID string, days int) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(tlsInfo monitor_tls_info.Service) error {
		return tlsInfo.StoreTLSInfoObject(context.Background(), monitorID, shared.TLSInfo{
			CertInfo: &shared.CertificateInfo{DaysRemaining: days},
		})
	}))
}

// A color that isn't a color must neither add markup nor change the badge:
// it's as if the option was left out.
func TestBadges_IgnoreUnsafeColors(t *testing.T) {
	a := newAppWithMonitorListener(t)
	// upColor and downColor only show on a monitor that's Up or Down, and on a
	// certificate expiry badge for a monitor with a certificate
	pending := a.createMonitor(t, "Pending monitor", "pending-push-token")
	up := a.createMonitor(t, "Up monitor", "up-push-token")
	down := a.createMonitor(t, "Down monitor", "down-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{pending, up, down}})
	a.reportStatus(t, up, shared.MonitorStatusUp)
	a.reportStatus(t, down, shared.MonitorStatusDown)
	a.storeCertExpiry(t, up, 30)
	a.storeCertExpiry(t, down, 3)

	const hostile = `"><script>alert(1)</script>`
	for name, monitorID := range map[string]string{"pending": pending, "up": up, "down": down} {
		for path := range badgePaths(monitorID) {
			for _, style := range []string{"flat", "flat-square", "plastic", "for-the-badge", "social"} {
				plain := path + "?style=" + style
				want := a.request(t, http.MethodGet, plain, nil)
				require.Equal(t, http.StatusOK, want.Code, want.Body.String())
				for _, option := range []string{"color", "labelColor", "upColor", "downColor"} {
					t.Run(name+path+"/"+style+"/"+option, func(t *testing.T) {
						w := a.request(t, http.MethodGet, plain+"&"+option+"="+url.QueryEscape(hostile), nil)
						assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
						assert.NotContains(t, w.Body.String(), "<script")
						assert.Equal(t, want.Body.String(), w.Body.String())
					})
				}
			}
		}
	}
}

var badgeFills = regexp.MustCompile(`<rect (?:x="\d+" )?width="\d+" height="\d+" fill="([^"]*)"/>`)

// The colors a badge accepts are drawn as hex, whichever way they're written.
func TestBadges_DrawValidColors(t *testing.T) {
	a := newAppWithMonitorListener(t)
	pending := a.createMonitor(t, "Pending monitor", "pending-push-token")
	up := a.createMonitor(t, "Up monitor", "up-push-token")
	down := a.createMonitor(t, "Down monitor", "down-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{pending, up, down}})
	a.reportStatus(t, up, shared.MonitorStatusUp)
	a.reportStatus(t, down, shared.MonitorStatusDown)

	for _, tt := range []struct {
		name, monitorID, query string
		labelFill, valueFill   string
	}{
		{"no color", pending, "", "#555", "#fe7d37"},
		{"bare hex", pending, "color=4c1", "#555", "#4c1"},
		{"hex with hash", pending, "color=%234c1", "#555", "#4c1"},
		{"long bare hex", pending, "color=ff0000", "#555", "#ff0000"},
		{"hex keeps its case", pending, "color=%23ABCDEF", "#555", "#ABCDEF"},
		{"color name", pending, "color=brightgreen", "#555", "#4c1"},
		{"color name in any case", pending, "color=LightGray", "#555", "#9f9f9f"},
		{"label hex", pending, "labelColor=333", "#333", "#fe7d37"},
		{"label color name", pending, "labelColor=red", "#e05d44", "#fe7d37"},
		{"up color", up, "upColor=orange", "#555", "#fe7d37"},
		{"down color", down, "downColor=yellow", "#555", "#dfb317"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/v1/badge/" + tt.monitorID + "/status?" + tt.query
			w := a.request(t, http.MethodGet, path, nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			fills := badgeFills.FindAllStringSubmatch(w.Body.String(), -1)
			require.GreaterOrEqual(t, len(fills), 2, w.Body.String())
			assert.Equal(t, tt.labelFill, fills[0][1])
			assert.Equal(t, tt.valueFill, fills[1][1])
		})
	}
}
