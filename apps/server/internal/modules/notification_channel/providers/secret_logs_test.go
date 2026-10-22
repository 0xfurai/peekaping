package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// loggedText returns everything the observed logger wrote, one entry per line: each message
// and its fields
func loggedText(logs *observer.ObservedLogs) string {
	var lines []string
	for _, entry := range logs.All() {
		line := entry.Message
		for key, value := range entry.ContextMap() {
			line += fmt.Sprintf(" %s=%v", key, value)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// A send to a channel whose URL is the credential must leave that URL out of its logs, at
// every level, and still log which channel it sent to and how it went
func TestSenders_DoNotLogSecretURLs(t *testing.T) {
	cases := []struct {
		name string
		// send runs a send of the channel against serverURL
		send func(logger *zap.SugaredLogger, serverURL string) error
		// secrets are the parts of the channel's config that must not show up in its logs
		secrets func(serverURL string) []string
		// channel is how the logs name the channel
		channel string
	}{
		{
			name: "telegram",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"bot_token": "123456:secret-bot-token", "chat_id": "42", "server_url": "` + serverURL + `"}`
				return NewTelegramSender(logger, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(serverURL string) []string { return []string{"123456:secret-bot-token", serverURL, "sendMessage"} },
			channel: "telegram",
		},
		{
			name: "webhook",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"webhook_url": "` + serverURL + `/hook/secret-webhook-path", "webhook_content_type": "json",` +
					` "webhook_additional_headers": "{\"Authorization\": \"Bearer secret-header-token\"}"}`
				return NewWebhookSender(logger, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(serverURL string) []string {
				return []string{"secret-webhook-path", "secret-header-token", "Authorization", serverURL}
			},
			channel: "webhook",
		},
		{
			name: "google chat",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"webhook_url": "` + serverURL + `/v1/spaces/AAA/messages?key=secret-chat-key&token=secret-chat-token"}`
				return NewGoogleChatSender(logger, &config.Config{}, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(serverURL string) []string { return []string{"secret-chat-key", "secret-chat-token", serverURL} },
			channel: "google chat",
		},
		{
			name: "wecom",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"webhook_url": "` + serverURL + `/cgi-bin/webhook/send?key=secret-wecom-key"}`
				return NewWeComSender(logger, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(serverURL string) []string { return []string{"secret-wecom-key", serverURL} },
			channel: "wecom",
		},
		{
			name: "grafana oncall",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"grafana_oncall_url": "` + serverURL + `/integrations/v1/webhook/secret-oncall-token/"}`
				return NewGrafanaOncallSender(logger, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(serverURL string) []string { return []string{"secret-oncall-token", serverURL} },
			channel: "grafana oncall",
		},
		{
			// PagerDuty's URL is a public endpoint; its integration key is the credential
			name: "pagerduty",
			send: func(logger *zap.SugaredLogger, serverURL string) error {
				cfg := `{"pagerduty_integration_key": "secret-pd-key", "pagerduty_integration_url": "` + serverURL + `/v2/enqueue"}`
				return NewPagerDutySender(logger, &config.Config{}, NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
			},
			secrets: func(string) []string { return []string{"secret-pd-key"} },
			channel: "pagerduty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			core, logs := observer.New(zap.DebugLevel)

			require.NoError(t, tc.send(zap.New(core).Sugar(), server.URL))

			logged := loggedText(logs)
			for _, secret := range tc.secrets(server.URL) {
				assert.NotContains(t, logged, secret)
			}
			assert.Contains(t, strings.ToLower(logged), tc.channel+" notification sent", "the log says which channel the send went to and that it succeeded")
		})
	}
}

// stubTransport answers every request with 200, so a sender with a fixed endpoint (LINE,
// Slack's API) can run without the network
type stubTransport struct{}

func (stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(`{"ok": true}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// Template bindings hold the whole monitor: its config (basic auth password, tokens, DSNs) and
// push token. A channel that renders a template must not dump them into its debug logs.
func TestSenders_DoNotLogTheMonitorsConfig(t *testing.T) {
	client := &http.Client{Transport: stubTransport{}}
	m := &monitor.Model{
		ID:        "mon-1",
		Name:      "Public API",
		Type:      "http",
		Config:    `{"url": "https://example.com", "authMethod": "basic", "basic_auth_pass": "secret-monitor-password"}`,
		PushToken: "secret-push-token",
	}

	cases := []struct {
		name string
		send func(logger *zap.SugaredLogger) error
	}{
		{"slack", func(logger *zap.SugaredLogger) error {
			cfg := `{"slack_webhook_url": "https://hooks.slack.com/services/T000/B000/xxx"}`
			return NewSlackSender(logger, &config.Config{}, client).Send(context.Background(), cfg, "Monitor is down", m, testHeartbeat())
		}},
		{"telegram", func(logger *zap.SugaredLogger) error {
			cfg := `{"bot_token": "123456:token", "chat_id": "42", "use_template": true, "template": "{{ name }} is {{ status }}"}`
			return NewTelegramSender(logger, client).Send(context.Background(), cfg, "Monitor is down", m, testHeartbeat())
		}},
		{"line", func(logger *zap.SugaredLogger) error {
			cfg := `{"channel_access_token": "token", "user_id": "user", "template": "{{ name }} is {{ status }}"}`
			return NewLineSender(logger, client).Send(context.Background(), cfg, "Monitor is down", m, testHeartbeat())
		}},
		{"discord", func(logger *zap.SugaredLogger) error {
			cfg := `{"webhook_url": "https://discord.com/api/webhooks/1/xxx"}`
			return NewDiscordSender(logger, client).Send(context.Background(), cfg, "Monitor is down", m, testHeartbeat())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)

			require.NoError(t, tc.send(zap.New(core).Sugar()))

			logged := loggedText(logs)
			assert.NotContains(t, logged, "secret-monitor-password")
			assert.NotContains(t, logged, "secret-push-token")
			assert.NotContains(t, logged, "basic_auth_pass")
		})
	}
}

func TestWebhookSender_InvalidAdditionalHeadersErrorDoesNotEchoTheHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	// Not JSON, so the send fails while it parses them
	cfg := `{"webhook_url": "` + server.URL + `", "webhook_content_type": "json", "webhook_additional_headers": "Authorization: Bearer secret-header-token"}`

	err := NewWebhookSender(zap.NewNop().Sugar(), NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "additional Headers is not a valid JSON")
	assert.NotContains(t, err.Error(), "secret-header-token")
}

func testMonitor() *monitor.Model {
	return &monitor.Model{ID: "mon-1", Name: "Public API", Type: "http", Config: `{"url": "https://example.com"}`}
}

func testHeartbeat() *heartbeat.Model {
	return &heartbeat.Model{MonitorID: "mon-1", Status: shared.MonitorStatusDown, Msg: "connection refused"}
}
