package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The text options of a badge URL change what the badge says, and an empty one leaves the default.
func TestBadges_ReadTextOptions(t *testing.T) {
	a := newAppWithMonitorListener(t)
	pending := a.createMonitor(t, "Pending monitor", "pending-push-token")
	up := a.createMonitor(t, "Up monitor", "up-push-token")
	down := a.createMonitor(t, "Down monitor", "down-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{pending, up, down}})
	a.reportStatus(t, up, shared.MonitorStatusUp)
	a.reportStatus(t, down, shared.MonitorStatusDown)
	a.storeCertExpiry(t, up, 30)

	for _, tt := range []struct {
		name, path, title string
	}{
		{"up label", up + "/status?upLabel=Online", "status: Online"},
		{"empty up label", up + "/status?upLabel=", "status: Up"},
		{"down label", down + "/status?downLabel=Offline", "status: Offline"},
		{"empty down label", down + "/status?downLabel=", "status: Down"},
		{"up label on a down monitor", down + "/status?upLabel=Online", "status: Down"},
		{"label", up + "/status?label=health", "health: Up"},
		{"label prefix and suffix", up + "/status?label=health&labelPrefix=a-&labelSuffix=-z", "a-health-z: Up"},
		{"label prefix and suffix on the default label", up + "/status?labelPrefix=a-&labelSuffix=-z", "a-status-z: Up"},
		{"cert-exp prefix and suffix", up + "/cert-exp?prefix=~&suffix=!", "cert exp: ~30d!"},
		{"cert-exp label parts", up + "/cert-exp?labelPrefix=x-&labelSuffix=-y", "x-cert exp-y: 30d"},
		{"uptime prefix", pending + "/uptime?prefix=~&suffix=!", "uptime (24h): ~N/A"},
		{"uptime label parts", pending + "/uptime?label=sla&labelPrefix=a-&labelSuffix=-z", "a-sla-z (24h): N/A"},
		{"ping label", pending + "/ping?label=latency", "latency (24h): N/A"},
		{"response label", pending + "/response?label=last&labelSuffix=!", "last!: N/A"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertBadge(t, a.request(t, http.MethodGet, "/api/v1/badge/"+tt.path, nil), tt.title)
		})
	}
}

// A valid style draws a badge of its own, and an unknown one the default flat badge.
func TestBadges_ReadStyle(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{monitorID}})
	path := "/api/v1/badge/" + monitorID + "/status"

	for _, tt := range []struct {
		style    string
		contains string
	}{
		{"", `font-weight="400"`},
		{"flat", `font-weight="400"`},
		{"unknown", `font-weight="400"`},
		{"plastic", `height="18"`},
		{"for-the-badge", `height="28"`},
		{"social", `textLength=`},
		{"flat-square", `<svg `},
	} {
		t.Run(tt.style, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path+"?style="+tt.style, nil)
			assertBadge(t, w, "status: Pending")
			assert.Contains(t, w.Body.String(), tt.contains)
		})
	}
	// flat-square is flat without the rounded corners and the gradient
	square := a.request(t, http.MethodGet, path+"?style=flat-square", nil)
	assert.NotContains(t, square.Body.String(), `rx="`)
	assert.Contains(t, a.request(t, http.MethodGet, path, nil).Body.String(), `rx="3"`)
	assert.Equal(t, a.request(t, http.MethodGet, path, nil).Body.String(), a.request(t, http.MethodGet, path+"?style=unknown", nil).Body.String())
}

// warnDays must be above zero and downDays zero or more to count, and move the colors of the
// certificate expiry badge.
func TestBadges_ReadCertExpiryDays(t *testing.T) {
	a := newAppWithMonitorListener(t)
	soon := a.createMonitor(t, "Soon", "soon-push-token")
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{soon}})
	a.reportStatus(t, soon, shared.MonitorStatusUp)
	// 3 days left: down by default (7 days), orange from 8 to 14, and green beyond
	a.storeCertExpiry(t, soon, 3)

	for _, tt := range []struct {
		name, query, fill string
	}{
		{"defaults", "", "#e05d44"},
		{"down days of zero", "downDays=0", "#fe7d37"},
		{"down days of zero and warn days below the days left", "downDays=0&warnDays=2", "#4c1"},
		{"warn days of zero is ignored", "downDays=0&warnDays=0", "#fe7d37"},
		{"negative warn days is ignored", "downDays=0&warnDays=-5", "#fe7d37"},
		{"negative down days is ignored", "downDays=-1", "#e05d44"},
		{"down days that aren't a number are ignored", "downDays=soon&warnDays=soon", "#e05d44"},
		{"down days below the days left", "downDays=2&warnDays=10", "#fe7d37"},
		{"down days of the days left", "downDays=3", "#e05d44"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := a.request(t, http.MethodGet, "/api/v1/badge/"+soon+"/cert-exp?"+tt.query, nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			fills := badgeFills.FindAllStringSubmatch(w.Body.String(), -1)
			require.GreaterOrEqual(t, len(fills), 2, w.Body.String())
			assert.Equal(t, tt.fill, fills[1][1])
		})
	}
}
