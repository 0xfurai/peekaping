package apitest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// with returns a copy of body with one field changed.
func with(body map[string]any, field string, value any) map[string]any {
	changed := map[string]any{field: value}
	for k, v := range body {
		if k != field {
			changed[k] = v
		}
	}
	return changed
}

type created struct {
	ID string `json:"id"`
}

func pushMonitorBody(name, pushToken string) map[string]any {
	return map[string]any{
		"type":             "push",
		"name":             name,
		"interval":         60,
		"retry_interval":   60,
		"timeout":          16,
		"active":           true,
		"notification_ids": []string{},
		"push_token":       pushToken,
		"config":           fmt.Sprintf(`{"pushToken":%q}`, pushToken),
	}
}

// createMonitor creates an active push monitor through the API and returns
// its ID.
func (a *app) createMonitor(t *testing.T, name, pushToken string) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", pushMonitorBody(name, pushToken), a.asAdmin), http.StatusCreated).ID
}

// pauseMonitor pauses a monitor the way the web app does.
func (a *app) pauseMonitor(t *testing.T, monitorID string) {
	t.Helper()
	w := a.request(t, http.MethodPatch, "/api/v1/monitors/"+monitorID, map[string]any{"active": false}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

type statusPage struct {
	slug      string
	published bool
	monitors  []string
	domains   []string
}

// createStatusPage creates a status page through the API and returns its ID.
func (a *app) createStatusPage(t *testing.T, page statusPage) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/status-pages", map[string]any{
		"slug":        page.slug,
		"title":       "Status of " + page.slug,
		"published":   page.published,
		"monitor_ids": page.monitors,
		"domains":     page.domains,
	}, a.asAdmin), http.StatusCreated).ID
}

func maintenanceBody(title string) map[string]any {
	return map[string]any{"title": title, "strategy": "manual", "active": true}
}

// weeklyMaintenanceBody is a maintenance window every Monday and Wednesday
// from start to end, in HH:MM.
func weeklyMaintenanceBody(title, start, end string) map[string]any {
	body := maintenanceBody(title)
	body["strategy"] = "recurring-weekday"
	body["weekdays"] = []int{1, 3}
	body["start_time"] = start
	body["end_time"] = end
	body["timezone"] = "UTC"
	return body
}

func webhookChannelBody(name string) map[string]any {
	return map[string]any{
		"name":   name,
		"type":   "webhook",
		"active": true,
		"config": `{"webhook_url":"https://hooks.example/peekaping","webhook_content_type":"json"}`,
	}
}

func proxyBody(host string) map[string]any {
	return map[string]any{"protocol": "http", "host": host, "port": 8080}
}

func tagBody(name string) map[string]any {
	return map[string]any{"name": name, "color": "#3B82F6"}
}
