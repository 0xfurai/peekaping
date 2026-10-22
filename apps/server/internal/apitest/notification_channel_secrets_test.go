package apitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"peekaping/internal/modules/notification_channel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secretPlaceholder is what the API shows in place of a stored secret
const secretPlaceholder = "********"

// secretMarker is in every secret value below, and in no other value. It has no characters JSON
// escapes, so it shows up in a response body as it is, wherever a secret leaks to.
const secretMarker = "SECRET"

// secretChannel is a valid channel of one type with every one of its secrets set
type secretChannel struct {
	channelType string
	// visible are the config fields that stay visible in responses
	visible map[string]any
	// secrets are the config fields a response must hide, with their stored values
	secrets map[string]string
}

// config returns the config the channel is saved with. Overrides replace fields, and an empty
// override removes the field.
func (c secretChannel) config(overrides map[string]string) string {
	fields := map[string]any{}
	for key, value := range c.visible {
		fields[key] = value
	}
	for key, value := range c.secrets {
		fields[key] = value
	}
	for key, value := range overrides {
		fields[key] = value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// redactedConfig returns the config a response shows: the secrets are replaced by the placeholder
func (c secretChannel) redactedConfig() string {
	overrides := map[string]string{}
	for key := range c.secrets {
		overrides[key] = secretPlaceholder
	}
	return c.config(overrides)
}

func (c secretChannel) body(name string) map[string]any {
	return map[string]any{"name": name, "type": c.channelType, "active": true, "config": c.config(nil)}
}

// secretChannels has a valid channel for every notification channel type, built from the
// validate tags of the provider's config
var secretChannels = []secretChannel{
	{"smtp",
		map[string]any{"smtp_host": "smtp.example.com", "smtp_port": 587, "username": "mailer", "from": "alerts@example.com", "to": "oncall@example.com"},
		map[string]string{"password": "smtp-SECRET-password"}},
	{"telegram",
		map[string]any{"chat_id": "-100123"},
		map[string]string{"bot_token": "123456:SECRET-telegram-token"}},
	{"webhook",
		map[string]any{"webhook_url": "https://hooks.example/peekaping", "webhook_content_type": "json"},
		map[string]string{"webhook_signing_secret": "whsec-SECRET-signing", "webhook_additional_headers": `{"X-Api-Key":"SECRET-header"}`}},
	{"slack",
		map[string]any{"slack_channel": "#alerts", "slack_username": "peekaping"},
		map[string]string{"slack_webhook_url": "https://hooks.slack.com/services/T000/B000/SECRET-slack-webhook", "slack_bot_token": "xoxb-SECRET-slack-bot"}},
	{"ntfy",
		map[string]any{"server_url": "https://ntfy.example", "topic": "alerts", "authentication_type": "basic", "username": "ntfy-user", "priority": 3},
		map[string]string{"password": "ntfy-SECRET-password", "token": "tk_SECRET-ntfy-token"}},
	{"pagerduty",
		map[string]any{"pagerduty_integration_url": "https://events.pagerduty.com/v2/enqueue", "pagerduty_priority": "critical"},
		map[string]string{"pagerduty_integration_key": "SECRET-pagerduty-key"}},
	{"opsgenie",
		map[string]any{"region": "eu", "priority": 2},
		map[string]string{"api_key": "opsgenie-SECRET-key"}},
	{"google_chat",
		map[string]any{},
		map[string]string{"webhook_url": "https://chat.googleapis.com/v1/spaces/AAA/messages?key=K&token=SECRET-google-chat"}},
	{"grafana_oncall",
		map[string]any{},
		map[string]string{"grafana_oncall_url": "https://oncall.example/integrations/v1/webhook/SECRET-grafana/"}},
	{"signal",
		map[string]any{"signal_url": "https://signal.example", "signal_number": "+15550001111", "signal_recipients": "+15550002222"},
		map[string]string{}},
	{"gotify",
		map[string]any{"server_url": "https://gotify.example", "priority": 5},
		map[string]string{"application_token": "gotify-SECRET-token"}},
	{"pushover",
		map[string]any{"pushover_priority": 0, "pushover_device": "phone"},
		map[string]string{"pushover_user_key": "pushover-SECRET-user", "pushover_app_token": "pushover-SECRET-app"}},
	{"mattermost",
		map[string]any{},
		map[string]string{"webhook_url": "https://mattermost.example/hooks/SECRET-mattermost"}},
	{"matrix",
		map[string]any{"homeserver_url": "https://matrix.example", "internal_room_id": "!room:matrix.example"},
		map[string]string{"access_token": "matrix-SECRET-token"}},
	{"discord",
		map[string]any{},
		map[string]string{"webhook_url": "https://discord.com/api/webhooks/1/SECRET-discord"}},
	{"wecom",
		map[string]any{},
		map[string]string{"webhook_url": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRET-wecom"}},
	{"whatsapp",
		map[string]any{"server_url": "https://wa.example", "phone_number": "15550001111", "session": "default"},
		map[string]string{"api_key": "whatsapp-SECRET-key"}},
	{"twilio",
		map[string]any{"twilio_account_sid": "AC123", "twilio_from_number": "+15550001111", "twilio_to_number": "+15550002222"},
		map[string]string{"twilio_auth_token": "twilio-SECRET-auth", "twilio_api_key": "twilio-SECRET-key"}},
	{"sendgrid",
		map[string]any{"from_email": "alerts@example.com", "to_email": "oncall@example.com"},
		map[string]string{"api_key": "SG.SECRET-sendgrid"}},
	{"pushbullet",
		map[string]any{},
		map[string]string{"pushbullet_access_token": "o.SECRET-pushbullet"}},
	{"pagertree",
		map[string]any{"urgency": "high", "autoResolve": true},
		map[string]string{"integrationUrl": "https://api.pagertree.com/integration/SECRET-pagertree", "authToken": "pagertree-SECRET-auth"}},
	{"line",
		map[string]any{"user_id": "U123"},
		map[string]string{"channel_access_token": "line-SECRET-token"}},
	{"teams",
		map[string]any{},
		map[string]string{"webhook_url": "https://example.webhook.office.com/webhookb2/SECRET-teams"}},
}

type channelWithConfig struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Config string `json:"config"`
}

// requireHidden checks that a response shows the channel with its secrets replaced and nothing
// else changed. w is the whole response and got the channel in it.
func (c secretChannel) requireHidden(t *testing.T, w *httptest.ResponseRecorder, got channelWithConfig) {
	t.Helper()
	assert.NotContains(t, w.Body.String(), secretMarker, "the response shows a secret")
	assert.JSONEq(t, c.redactedConfig(), got.Config)
}

// registeredChannelTypes returns the notification channel types the app has a provider for
func (a *app) registeredChannelTypes(t *testing.T) []string {
	t.Helper()
	var types []string
	require.NoError(t, a.container.Invoke(func(providers *notification_channel.ProviderRegistry) {
		types = providers.Types()
	}))
	return types
}

func TestNotificationChannelsAPI_SecretChannelsCoverEveryProvider(t *testing.T) {
	a := newApp(t)

	fixtureTypes := []string{}
	for _, c := range secretChannels {
		fixtureTypes = append(fixtureTypes, c.channelType)
		for key, value := range c.secrets {
			assert.Contains(t, value, secretMarker, "%s %s", c.channelType, key)
		}
		for key, value := range c.visible {
			if s, ok := value.(string); ok {
				assert.NotContains(t, s, secretMarker, "%s %s", c.channelType, key)
			}
		}
	}
	sort.Strings(fixtureTypes)
	assert.Equal(t, a.registeredChannelTypes(t), fixtureTypes, "every notification channel type needs a case in secretChannels")
}

func TestNotificationChannelsAPI_ResponsesHideSecrets(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, c := range secretChannels {
		t.Run(c.channelType, func(t *testing.T) {
			// The channel is saved with its real secrets
			w := a.request(t, http.MethodPost, "/api/v1/notification-channels", c.body("Channel"), a.asAdmin)
			createdChannel := decode[channelWithConfig](t, w, http.StatusCreated)
			c.requireHidden(t, w, createdChannel)
			path := "/api/v1/notification-channels/" + createdChannel.ID

			w = a.request(t, http.MethodGet, path, nil, a.asAdmin)
			c.requireHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			w = a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin)
			found := false
			for _, listed := range decode[[]channelWithConfig](t, w, http.StatusOK) {
				if listed.ID == createdChannel.ID {
					found = true
					assert.JSONEq(t, c.redactedConfig(), listed.Config)
				}
			}
			assert.True(t, found, "the list doesn't have the channel")
			assert.NotContains(t, w.Body.String(), secretMarker, "the list shows a secret")

			w = a.request(t, http.MethodPut, path, c.body("Renamed"), a.asAdmin)
			c.requireHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			w = a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed again", "config": c.config(nil)}, a.asAdmin)
			c.requireHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			// A rename with no config in the request shows the stored config the same way
			w = a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed last"}, a.asAdmin)
			c.requireHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))
		})
	}

	// The list of every channel shows no secret either
	w := a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin)
	assert.Len(t, decode[[]channelWithConfig](t, w, http.StatusOK), len(secretChannels))
	assert.NotContains(t, w.Body.String(), secretMarker, "the list shows a secret")
}
