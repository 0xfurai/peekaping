package apitest

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestAPI_MissingRecordsAreNotFound(t *testing.T) {
	a := newAppWithAdmin(t)
	const id = "00000000-0000-4000-8000-000000000000"
	var (
		monitor     = "/api/v1/monitors/" + id
		maintenance = "/api/v1/maintenances/" + id
		channel     = "/api/v1/notification-channels/" + id
		proxy       = "/api/v1/proxies/" + id
		tag         = "/api/v1/tags/" + id
		statusPage  = "/api/v1/status-pages/" + id
		apiKey      = "/api/v1/api-keys/" + id
		since       = url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	)

	a.expectStatus(t, http.StatusNotFound, []call{
		{"get monitor", http.MethodGet, monitor, nil},
		{"replace monitor", http.MethodPut, monitor, pushMonitorBody("Renamed", "renamed-push-token")},
		{"update monitor", http.MethodPatch, monitor, map[string]any{"name": "Renamed"}},
		{"delete monitor", http.MethodDelete, monitor, nil},
		{"reset monitor", http.MethodPost, monitor + "/reset", nil},
		{"monitor TLS info", http.MethodGet, monitor + "/tls", nil},
		{"monitor heartbeats", http.MethodGet, monitor + "/heartbeats", nil},
		{"monitor uptime", http.MethodGet, monitor + "/stats/uptime", nil},
		{"monitor stat points", http.MethodGet, monitor + "/stats/points?since=" + since, nil},

		{"get maintenance window", http.MethodGet, maintenance, nil},
		{"replace maintenance window", http.MethodPut, maintenance, maintenanceBody("Renamed")},
		{"update maintenance window", http.MethodPatch, maintenance, map[string]any{"title": "Renamed"}},
		{"delete maintenance window", http.MethodDelete, maintenance, nil},
		{"pause maintenance window", http.MethodPatch, maintenance + "/pause", nil},
		{"resume maintenance window", http.MethodPatch, maintenance + "/resume", nil},

		{"get notification channel", http.MethodGet, channel, nil},
		{"replace notification channel", http.MethodPut, channel, webhookChannelBody("Renamed")},
		{"update notification channel", http.MethodPatch, channel, map[string]any{"name": "Renamed"}},
		{"delete notification channel", http.MethodDelete, channel, nil},

		{"get proxy", http.MethodGet, proxy, nil},
		{"replace proxy", http.MethodPut, proxy, proxyBody("renamed.example")},
		{"update proxy", http.MethodPatch, proxy, map[string]any{"host": "renamed.example"}},
		{"delete proxy", http.MethodDelete, proxy, nil},

		{"get tag", http.MethodGet, tag, nil},
		{"replace tag", http.MethodPut, tag, tagBody("Renamed")},
		{"update tag", http.MethodPatch, tag, map[string]any{"name": "Renamed"}},
		{"delete tag", http.MethodDelete, tag, nil},

		{"get status page", http.MethodGet, statusPage, nil},
		{"update status page", http.MethodPatch, statusPage, map[string]any{"title": "Renamed"}},
		{"delete status page", http.MethodDelete, statusPage, nil},

		{"get API key", http.MethodGet, apiKey, nil},
		{"update API key", http.MethodPut, apiKey, map[string]any{"name": "Renamed"}},
		{"delete API key", http.MethodDelete, apiKey, nil},
	})
}
