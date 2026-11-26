package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestSlackSender() *SlackSender {
	return NewSlackSender(zap.NewNop().Sugar(), installsettings.Static(&config.Config{}), NewHTTPClient())
}

func sendTestSlackMessage(sender *SlackSender, configJSON string) error {
	return sender.Send(
		context.Background(),
		configJSON,
		"Monitor is down",
		&monitor.Model{Name: "Test Monitor"},
		&heartbeat.Model{Status: shared.MonitorStatusDown},
	)
}

func TestSlackSender_Validate(t *testing.T) {
	sender := newTestSlackSender()

	cases := []struct {
		name    string
		config  string
		wantErr string
	}{
		{"webhook only", `{"slack_webhook_url": "https://hooks.slack.com/services/T/B/X"}`, ""},
		{"bot token with channel", `{"slack_bot_token": "xoxb-1", "slack_channel": "#alerts"}`, ""},
		{"neither webhook nor token", `{"slack_channel": "#alerts"}`, "either slack_webhook_url or slack_bot_token"},
		{"whitespace token", `{"slack_bot_token": "  \n", "slack_channel": "#alerts"}`, "either slack_webhook_url or slack_bot_token"},
		{"token without channel", `{"slack_bot_token": "xoxb-1"}`, "slack_channel is required"},
		{"token with whitespace channel", `{"slack_bot_token": "xoxb-1", "slack_channel": " "}`, "slack_channel is required"},
		{"invalid webhook url", `{"slack_webhook_url": "not-a-url"}`, "WebhookURL"},
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

func TestSlackSender_SendWithBotToken(t *testing.T) {
	var gotAuth string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		_, _ = io.WriteString(w, `{"ok": true, "channel": "C1", "ts": "1.2"}`)
	}))
	defer server.Close()

	sender := newTestSlackSender()
	sender.apiURL = server.URL

	err := sendTestSlackMessage(sender, `{"slack_bot_token": "xoxb-test\n", "slack_channel": "#alerts"}`)

	require.NoError(t, err)
	assert.Equal(t, "Bearer xoxb-test", gotAuth)
	assert.Equal(t, "#alerts", gotPayload["channel"])
	assert.Equal(t, "Monitor is down", gotPayload["text"])
}

func TestSlackSender_SendWithBotTokenAPIError(t *testing.T) {
	// chat.postMessage answers HTTP 200 even when the message was not posted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok": false, "error": "not_in_channel"}`)
	}))
	defer server.Close()

	sender := newTestSlackSender()
	sender.apiURL = server.URL

	err := sendTestSlackMessage(sender, `{"slack_bot_token": "xoxb-test", "slack_channel": "#alerts"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not_in_channel")
}

func TestSlackSender_SendWithWebhookErrorStatus(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "no_service")
	}))
	defer server.Close()

	err := sendTestSlackMessage(newTestSlackSender(), `{"slack_webhook_url": "`+server.URL+`/services/T/B/X"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Slack webhook returned status 404")
	assert.Contains(t, err.Error(), "no_service")
	assert.Empty(t, gotAuth)
}

func TestSlackSender_SendErrorHidesWebhookURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	webhookURL := server.URL + "/services/T/B/secret"
	server.Close()

	err := sendTestSlackMessage(newTestSlackSender(), `{"slack_webhook_url": "`+webhookURL+`"}`)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
}

func TestSlackSender_SendWebhookErrorDropsUnknownBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "404 page not found: "+r.URL.Path)
	}))
	defer server.Close()

	err := sendTestSlackMessage(newTestSlackSender(), `{"slack_webhook_url": "`+server.URL+`/services/T/B/secret"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Slack webhook returned status 404")
	assert.NotContains(t, err.Error(), "secret")
}

func TestSlackSender_SendWebhookErrorDropsReasonPhrase(t *testing.T) {
	for _, body := range []string{"", "no_service"} {
		t.Run("body "+body, func(t *testing.T) {
			// net/http always writes the standard reason phrase, so the response is written raw
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, buf, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				defer conn.Close()
				_, _ = fmt.Fprintf(buf, "HTTP/1.1 404 %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", r.URL.Path, len(body), body)
				_ = buf.Flush()
			}))
			defer server.Close()

			err := sendTestSlackMessage(newTestSlackSender(), `{"slack_webhook_url": "`+server.URL+`/services/T/B/secret"}`)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "Slack webhook returned status 404 Not Found")
			assert.Contains(t, err.Error(), body)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestSlackSender_SendRejectsHTTPSDowngradeRedirect(t *testing.T) {
	plainHit := false
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHit = true
		_, _ = io.WriteString(w, `{"ok": true}`)
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/api/chat.postMessage", http.StatusTemporaryRedirect)
	}))
	defer secure.Close()

	sender := newTestSlackSender()
	sender.apiURL = secure.URL
	sender.client.Transport = secure.Client().Transport

	err := sendTestSlackMessage(sender, `{"slack_bot_token": "xoxb-test", "slack_channel": "#alerts"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "307")
	assert.False(t, plainHit, "the bot token must not follow a redirect to plain HTTP")
}

func TestSlackSender_RichMessageAddress(t *testing.T) {
	cases := []struct {
		name        string
		monitorType string
		config      string
		wantField   string
		wantSiteURL string
	}{
		{
			name:        "http url drops credentials, query and fragment",
			monitorType: "http-keyword",
			config:      `{"url": "https://user:pass@api.example.com/wiki/Foo_(bar)?token=abc#top"}`,
			wantField:   "*URL*\nhttps://api.example.com/wiki/Foo_(bar)",
			wantSiteURL: "https://api.example.com/wiki/Foo_(bar)",
		},
		{
			name:        "mrkdwn characters in the url are escaped in the field only",
			monitorType: "http",
			config:      `{"url": "https://example.com/a&b"}`,
			wantField:   "*URL*\nhttps://example.com/a&amp;b",
			wantSiteURL: "https://example.com/a&b",
		},
		{
			name:        "tcp host and port are not a link",
			monitorType: "tcp",
			config:      `{"host": "10.0.0.5", "port": 5432}`,
			wantField:   "*Host*\n10.0.0.5:5432",
		},
		{
			name:        "tcp ipv6 host is bracketed",
			monitorType: "tcp",
			config:      `{"host": "2001:db8::1", "port": 443}`,
			wantField:   "*Host*\n[2001:db8::1]:443",
		},
		{
			name:        "dns host without the resolver port",
			monitorType: "dns",
			config:      `{"host": "example.com", "resolver_server": "1.1.1.1", "port": 53}`,
			wantField:   "*Host*\nexample.com",
		},
		{
			name:        "push monitor has no address",
			monitorType: "push",
			config:      `{"pushToken": "secret"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks := newTestSlackSender().buildBlocks(
				"",
				&monitor.Model{Type: tc.monitorType, Config: tc.config},
				&heartbeat.Model{Status: shared.MonitorStatusDown},
				"Peekaping Alert",
				"down",
			)

			// The message and the time come first, then the address when there is one
			fields := blocks[1]["fields"].([]map[string]any)
			if tc.wantField == "" {
				assert.Len(t, fields, 2)
			} else {
				require.Len(t, fields, 3)
				assert.Equal(t, tc.wantField, fields[2]["text"])
			}

			// With no public URL, the only button is the one to the site
			if tc.wantSiteURL == "" {
				assert.Len(t, blocks, 2, "no actions block without a link")
			} else {
				require.Len(t, blocks, 3)
				elements := blocks[2]["elements"].([]map[string]any)
				require.Len(t, elements, 1)
				assert.Equal(t, "Visit site", elements[0]["text"].(map[string]any)["text"])
				assert.Equal(t, tc.wantSiteURL, elements[0]["url"])
			}
		})
	}
}
