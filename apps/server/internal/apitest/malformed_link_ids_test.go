package apitest

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
)

// forEachDatabase runs fn against an app on SQLite, and on PostgreSQL too when
// PEE_TEST_POSTGRES_DSN names a server. PostgreSQL types the id columns as uuid, so a value that
// isn't a UUID is an error of the database there, and the text SQLite compares is never one.
func forEachDatabase(t *testing.T, fn func(t *testing.T, a *app)) {
	t.Helper()
	backends := []struct {
		name   string
		option func(*config.Config)
	}{{"SQLite", func(*config.Config) {}}}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends = append(backends, struct {
			name   string
			option func(*config.Config)
		}{"PostgreSQL", usePostgres(t, dsn)})
	}
	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) { fn(t, newAppWithAdmin(t, backend.option)) })
	}
}

// An ID that isn't a UUID is an ID nobody has: a monitor that names one as its channel, tag or
// proxy, and a maintenance window that names one as a monitor, get the answer a UUID nobody has
// gets, 400 naming the kind, and not a 500 from a database that can't read the value as a UUID.
func TestMalformedLinkIDs_AreRefusedAsUnknownOnEveryDatabase(t *testing.T) {
	forEachDatabase(t, func(t *testing.T, a *app) {
		for _, malformed := range []string{"not-a-uuid", "12345", "00000000-0000-0000-0000-00000000000g"} {
			for what, link := range map[string]func(map[string]any, string) map[string]any{
				"channel": func(body map[string]any, id string) map[string]any {
					return with(body, "notification_ids", []string{id})
				},
				"tag":   func(body map[string]any, id string) map[string]any { return with(body, "tag_ids", []string{id}) },
				"proxy": func(body map[string]any, id string) map[string]any { return with(body, "proxy_id", id) },
			} {
				t.Run("monitor create naming a "+what+" "+malformed, func(t *testing.T) {
					w := a.request(t, http.MethodPost, monitorsPath, link(pushMonitorBody("Doomed", "doomed-"+what), malformed), a.asAdmin)
					unknown := a.request(t, http.MethodPost, monitorsPath, link(pushMonitorBody("Doomed", "doomed-"+what), missingRecordID), a.asAdmin)

					assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
					assert.Equal(t, unknown.Code, w.Code)
					assert.JSONEq(t, unknown.Body.String(), w.Body.String())
					assert.Empty(t, a.listMonitors(t), "no monitor is stored")
				})
			}
			t.Run("maintenance window naming a monitor "+malformed, func(t *testing.T) {
				body := with(maintenanceBody("Doomed"), "monitor_ids", []string{malformed})
				w := a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.asAdmin)
				unknown := a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Doomed"), "monitor_ids", []string{missingRecordID}), a.asAdmin)

				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				assert.Equal(t, unknown.Code, w.Code)
				assert.JSONEq(t, unknown.Body.String(), w.Body.String())
			})
		}
	})
}

// A UUID in upper case is the record the lower case one names, on SQLite as on PostgreSQL
func TestMalformedLinkIDs_AnUpperCaseCopyOfAnIDNamesTheSameRecord(t *testing.T) {
	forEachDatabase(t, func(t *testing.T, a *app) {
		channel, tag := a.createChannel(t, "Channel"), a.createTag(t, "Tag")
		proxy := decode[created](t, a.request(t, http.MethodPost, "/api/v1/proxies", proxyBody("upper.example"), a.asAdmin), http.StatusCreated).ID
		body := with(with(with(pushMonitorBody("Linked", "linked-token"), "notification_ids", []string{strings.ToUpper(channel)}),
			"tag_ids", []string{strings.ToUpper(tag)}), "proxy_id", strings.ToUpper(proxy))

		w := a.request(t, http.MethodPost, monitorsPath, body, a.asAdmin)
		monitor := decode[created](t, w, http.StatusCreated).ID
		got := decode[monitorWithLinks](t, a.request(t, http.MethodGet, monitorsPath+"/"+monitor, nil, a.asAdmin), http.StatusOK)
		assert.Equal(t, []string{channel}, got.NotificationIDs, "linked by the lower case ID")
		assert.Equal(t, []string{tag}, got.TagIDs)

		w = a.request(t, http.MethodPost, "/api/v1/maintenances", with(maintenanceBody("Window"), "monitor_ids", []string{strings.ToUpper(monitor)}), a.asAdmin)
		assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
}
