package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestSlackSender() *SlackSender {
	return NewSlackSender(zap.NewNop().Sugar(), &config.Config{})
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
