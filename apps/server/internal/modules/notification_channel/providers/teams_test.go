package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestTeamsSender() *TeamsSender {
	return NewTeamsSender(zap.NewNop().Sugar(), installsettings.Static(&config.Config{ClientURL: "https://peekaping.example.com/"}), NewHTTPClient())
}

func sendTestTeamsMessage(sender *TeamsSender, configJSON string) error {
	return sender.Send(
		context.Background(),
		configJSON,
		"Monitor is down",
		&monitor.Model{Name: "Test Monitor"},
		&heartbeat.Model{Status: shared.MonitorStatusDown},
	)
}

// testTeamsCard holds the parts of the adaptive card the tests check
type testTeamsCard struct {
	FallbackText string `json:"fallbackText"`
	Body         []struct {
		Type  string `json:"type"`
		Style string `json:"style"`
		Text  string `json:"text"`
		Facts []struct {
			Title string `json:"title"`
			Value string `json:"value"`
		} `json:"facts"`
	} `json:"body"`
	Actions []struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"actions"`
}

func (c testTeamsCard) facts() map[string]string {
	facts := map[string]string{}
	for _, block := range c.Body {
		for _, fact := range block.Facts {
			facts[fact.Title] = fact.Value
		}
	}
	return facts
}

func (c testTeamsCard) actions() map[string]string {
	actions := map[string]string{}
	for _, action := range c.Actions {
		actions[action.Title] = action.URL
	}
	return actions
}

func (c testTeamsCard) text() string {
	for _, block := range c.Body {
		if block.Type == "TextBlock" {
			return block.Text
		}
	}
	return ""
}

func buildTestTeamsCard(t *testing.T, m *monitor.Model, hb *heartbeat.Model, message string) testTeamsCard {
	t.Helper()
	raw, err := json.Marshal(newTestTeamsSender().buildAdaptiveCard(context.Background(), &TeamsConfig{}, m, hb, message))
	require.NoError(t, err)
	var card testTeamsCard
	require.NoError(t, json.Unmarshal(raw, &card))
	return card
}

func TestTeamsSender_Validate(t *testing.T) {
	sender := newTestTeamsSender()

	cases := []struct {
		name    string
		config  string
		wantErr string
	}{
		{"webhook only", `{"webhook_url": "https://example.powerplatform.com/workflows/1"}`, ""},
		{"webhook with trailing newline", `{"webhook_url": "https://example.powerplatform.com/workflows/1\n"}`, ""},
		{"webhook and server url", `{"webhook_url": "https://example.powerplatform.com/workflows/1", "server_url": "https://peekaping.example.com"}`, ""},
		{"missing webhook", `{}`, "WebhookURL"},
		{"invalid webhook", `{"webhook_url": "not-a-url"}`, "WebhookURL"},
		{"invalid server url", `{"webhook_url": "https://example.powerplatform.com/workflows/1", "server_url": "peekaping"}`, "ServerURL"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sender.Validate(tc.config)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestTeamsSender_Send(t *testing.T) {
	var gotPayload map[string]any
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	err := sendTestTeamsMessage(newTestTeamsSender(), `{"webhook_url": "`+server.URL+`/workflows/1"}`)

	require.NoError(t, err)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "message", gotPayload["type"])
	attachments, ok := gotPayload["attachments"].([]any)
	require.True(t, ok)
	require.Len(t, attachments, 1)
	attachment := attachments[0].(map[string]any)
	assert.Equal(t, "application/vnd.microsoft.card.adaptive", attachment["contentType"])
	content := attachment["content"].(map[string]any)
	assert.Equal(t, "AdaptiveCard", content["type"])
	assert.Equal(t, "🔴 Test Monitor is down", content["fallbackText"])
}

func TestTeamsSender_CardShowsStatus(t *testing.T) {
	cases := []struct {
		status    shared.MonitorStatus
		wantTitle string
		wantStyle string
	}{
		{shared.MonitorStatusUp, "✅ API is up", "good"},
		{shared.MonitorStatusDown, "🔴 API is down", "attention"},
		{shared.MonitorStatusPending, "⏳ API is pending", "warning"},
		{shared.MonitorStatusMaintenance, "🚧 API is under maintenance", "accent"},
	}

	for _, tc := range cases {
		t.Run(tc.wantTitle, func(t *testing.T) {
			card := buildTestTeamsCard(t, &monitor.Model{Name: "API"}, &heartbeat.Model{Status: tc.status}, "200 - OK")

			assert.Equal(t, tc.wantTitle, card.FallbackText)
			assert.Equal(t, tc.wantStyle, card.Body[0].Style)
			assert.Equal(t, "200 - OK", card.text())
		})
	}
}

func TestTeamsSender_CardLinksToMonitor(t *testing.T) {
	hbTime := time.Date(2026, 10, 17, 21, 24, 55, 0, time.FixedZone("EDT", -4*60*60))
	card := buildTestTeamsCard(t,
		&monitor.Model{ID: "m1", Name: "API", Type: "http", Config: `{"url": "https://api.example.com/health"}`},
		&heartbeat.Model{Status: shared.MonitorStatusDown, Time: hbTime},
		"timeout",
	)

	assert.Equal(t, map[string]string{
		"Monitor": "API",
		"URL":     "https://api.example.com/health",
		"Time":    "2026-10-18 01:24:55 UTC",
	}, card.facts())
	assert.Equal(t, map[string]string{
		"View in Peekaping": "https://peekaping.example.com/monitors/m1",
		"Visit site":        "https://api.example.com/health",
	}, card.actions())
}

func TestTeamsSender_CardServerURLOverridesClientURL(t *testing.T) {
	raw, err := json.Marshal(newTestTeamsSender().buildAdaptiveCard(
		context.Background(),
		&TeamsConfig{ServerURL: "https://status.example.com/"},
		&monitor.Model{ID: "m1", Name: "API"},
		&heartbeat.Model{Status: shared.MonitorStatusDown},
		"timeout",
	))
	require.NoError(t, err)
	var card testTeamsCard
	require.NoError(t, json.Unmarshal(raw, &card))

	assert.Equal(t, "https://status.example.com/monitors/m1", card.actions()["View in Peekaping"])
}

func TestTeamsSender_CardAddress(t *testing.T) {
	cases := []struct {
		name        string
		monitorType string
		config      string
		wantFacts   map[string]string
		wantSiteURL string
	}{
		{
			name:        "http url drops credentials, query and fragment",
			monitorType: "http-keyword",
			config:      `{"url": "https://user:pass@api.example.com/wiki/Foo_(bar)?token=abc#top"}`,
			wantFacts:   map[string]string{"URL": "https://api.example.com/wiki/Foo_(bar)"},
			wantSiteURL: "https://api.example.com/wiki/Foo_(bar)",
		},
		{
			name:        "tcp host and port are not a link",
			monitorType: "tcp",
			config:      `{"host": "10.0.0.5", "port": 5432}`,
			wantFacts:   map[string]string{"Host": "10.0.0.5:5432"},
		},
		{
			name:        "tcp ipv6 host is bracketed",
			monitorType: "tcp",
			config:      `{"host": "2001:db8::1", "port": 443}`,
			wantFacts:   map[string]string{"Host": "[2001:db8::1]:443"},
		},
		{
			name:        "dns host without the resolver port",
			monitorType: "dns",
			config:      `{"host": "example.com", "resolver_server": "1.1.1.1", "port": 53}`,
			wantFacts:   map[string]string{"Host": "example.com"},
		},
		{
			name:        "push monitor has no address",
			monitorType: "push",
			config:      `{"pushToken": "secret"}`,
			wantFacts:   map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := buildTestTeamsCard(t,
				&monitor.Model{Type: tc.monitorType, Config: tc.config},
				&heartbeat.Model{Status: shared.MonitorStatusDown},
				"down",
			)

			assert.Equal(t, tc.wantFacts, card.facts())
			if tc.wantSiteURL == "" {
				assert.Empty(t, card.Actions)
			} else {
				assert.Equal(t, map[string]string{"Visit site": tc.wantSiteURL}, card.actions())
			}
		})
	}
}

func TestTeamsSender_CardForTestNotification(t *testing.T) {
	// The test endpoint sends a monitor that isn't saved and a heartbeat without a time
	card := buildTestTeamsCard(t,
		&monitor.Model{Name: "Test Monitor", Type: "http"},
		&heartbeat.Model{Status: shared.MonitorStatusDown, Msg: "Test"},
		"Test",
	)

	assert.Equal(t, map[string]string{"Monitor": "Test Monitor"}, card.facts())
	assert.Empty(t, card.Actions)
}

func TestTeamsSender_CardKeepsLineBreaks(t *testing.T) {
	// Certificate expiry notifications send a multi-line message and no heartbeat
	card := buildTestTeamsCard(t,
		&monitor.Model{Name: "API"},
		nil,
		"🚨 Certificate Expiry Warning\n\nMonitor: API\nExpires in: 7 days",
	)

	assert.Equal(t, "API", card.FallbackText)
	assert.Equal(t, "warning", card.Body[0].Style)
	assert.Equal(t, "🚨 Certificate Expiry Warning\n\nMonitor: API\n\nExpires in: 7 days", card.text())
}

func TestTeamsSender_SendErrorHidesWebhookURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	webhookURL := server.URL + "/workflows/1?sig=secret"
	server.Close()

	err := sendTestTeamsMessage(newTestTeamsSender(), `{"webhook_url": "`+webhookURL+`"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send Teams message")
	assert.NotContains(t, err.Error(), "secret")
}

func TestTeamsSender_SendErrorKeepsPowerAutomateCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error": {"code": "InvalidRequestContent", "message": "bad request to `+r.URL.String()+`"}}`)
	}))
	defer server.Close()

	err := sendTestTeamsMessage(newTestTeamsSender(), `{"webhook_url": "`+server.URL+`/workflows/1?sig=secret"}`)

	require.Error(t, err)
	assert.Equal(t, "Teams webhook returned status 400 Bad Request: InvalidRequestContent", err.Error())
}

func TestTeamsSender_SendErrorDropsReasonPhraseAndBody(t *testing.T) {
	for _, body := range []string{"", "not found: /workflows/1?sig=secret", `{"error": {"code": "sig=secret"}}`} {
		t.Run("body "+body, func(t *testing.T) {
			// net/http always writes the standard reason phrase, so the response is written raw
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, buf, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				defer conn.Close()
				_, _ = fmt.Fprintf(buf, "HTTP/1.1 404 %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", r.URL.RequestURI(), len(body), body)
				_ = buf.Flush()
			}))
			defer server.Close()

			err := sendTestTeamsMessage(newTestTeamsSender(), `{"webhook_url": "`+server.URL+`/workflows/1?sig=secret"}`)

			require.Error(t, err)
			assert.Equal(t, "Teams webhook returned status 404 Not Found", err.Error())
		})
	}
}
