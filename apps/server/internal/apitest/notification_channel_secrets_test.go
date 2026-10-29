package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"sync"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// blockExternalHTTP fails every HTTP request the test sends to a host that isn't on this machine,
// and fails the test. The providers send with http.DefaultTransport, so a channel that reached
// Send would otherwise go out to the real service of its type.
func blockExternalHTTP(t *testing.T) {
	t.Helper()
	original := http.DefaultTransport
	var mu sync.Mutex
	var blocked []string
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		host := req.URL.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			return original.RoundTrip(req)
		}
		mu.Lock()
		blocked = append(blocked, req.URL.Host)
		mu.Unlock()
		return nil, fmt.Errorf("the test blocked a request to %s", req.URL.Host)
	})
	t.Cleanup(func() {
		http.DefaultTransport = original
		mu.Lock()
		defer mu.Unlock()
		assert.Empty(t, blocked, "the test sent requests outside this machine")
	})
}

// newSecretsApp builds the API with an admin, as newAppWithAdmin does, and blocks the test from
// sending anything outside this machine. The private address block is off (PP-158): its channels
// send to receivers on this machine, and the transport the test replaces is the default one, which a
// guarded client doesn't use.
func newSecretsApp(t *testing.T) *app {
	t.Helper()
	blockExternalHTTP(t)
	return newAppWithAdmin(t, withBlockPrivateTargets(false))
}

// secretChannel is a valid channel of one type with every one of its secrets set
type secretChannel struct {
	channelType string
	// visible are the config fields that stay visible in responses
	visible map[string]any
	// secrets are the config fields a response must hide, with their stored values
	secrets map[string]string
}

// config returns the config the channel is saved with. Overrides replace the value of a field.
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
// validate tags of the provider's config. Every address in them is on this machine and refuses
// connections, so that a channel that is sent to by mistake reaches nothing.
var secretChannels = []secretChannel{
	{"smtp",
		map[string]any{"smtp_host": "127.0.0.1", "smtp_port": 1, "username": "mailer", "from": "alerts@example.com", "to": "oncall@example.com"},
		map[string]string{"password": "smtp-SECRET-password"}},
	{"telegram",
		map[string]any{"chat_id": "-100123", "server_url": "http://127.0.0.1:1"},
		map[string]string{"bot_token": "123456:SECRET-telegram-token"}},
	{"webhook",
		map[string]any{"webhook_url": "http://127.0.0.1:1/hook", "webhook_content_type": "json"},
		map[string]string{"webhook_signing_secret": "whsec-SECRET-signing", "webhook_additional_headers": `{"X-Api-Key":"SECRET-header"}`}},
	{"slack",
		map[string]any{"slack_channel": "#alerts", "slack_username": "peekaping"},
		map[string]string{"slack_webhook_url": "http://127.0.0.1:1/services/T000/B000/SECRET-slack-webhook", "slack_bot_token": "xoxb-SECRET-slack-bot"}},
	{"ntfy",
		map[string]any{"server_url": "http://127.0.0.1:1", "topic": "alerts", "authentication_type": "basic", "username": "ntfy-user", "priority": 3},
		map[string]string{"password": "ntfy-SECRET-password", "token": "tk_SECRET-ntfy-token"}},
	{"pagerduty",
		map[string]any{"pagerduty_integration_url": "http://127.0.0.1:1/v2/enqueue", "pagerduty_priority": "critical"},
		map[string]string{"pagerduty_integration_key": "SECRET-pagerduty-key"}},
	{"opsgenie",
		map[string]any{"region": "eu", "priority": 2},
		map[string]string{"api_key": "opsgenie-SECRET-key"}},
	{"google_chat",
		map[string]any{},
		map[string]string{"webhook_url": "http://127.0.0.1:1/v1/spaces/AAA/messages?key=K&token=SECRET-google-chat"}},
	{"grafana_oncall",
		map[string]any{},
		map[string]string{"grafana_oncall_url": "http://127.0.0.1:1/integrations/v1/webhook/SECRET-grafana/"}},
	{"signal",
		map[string]any{"signal_url": "http://127.0.0.1:1", "signal_number": "+15550001111", "signal_recipients": "+15550002222"},
		map[string]string{}},
	{"gotify",
		map[string]any{"server_url": "http://127.0.0.1:1", "priority": 5},
		map[string]string{"application_token": "gotify-SECRET-token"}},
	{"pushover",
		map[string]any{"pushover_priority": 0, "pushover_device": "phone"},
		map[string]string{"pushover_user_key": "pushover-SECRET-user", "pushover_app_token": "pushover-SECRET-app"}},
	{"mattermost",
		map[string]any{},
		map[string]string{"webhook_url": "http://127.0.0.1:1/hooks/SECRET-mattermost"}},
	{"matrix",
		map[string]any{"homeserver_url": "http://127.0.0.1:1", "internal_room_id": "!room:matrix.example"},
		map[string]string{"access_token": "matrix-SECRET-token"}},
	{"discord",
		map[string]any{},
		map[string]string{"webhook_url": "http://127.0.0.1:1/api/webhooks/1/SECRET-discord"}},
	{"wecom",
		map[string]any{},
		map[string]string{"webhook_url": "http://127.0.0.1:1/cgi-bin/webhook/send?key=SECRET-wecom"}},
	{"whatsapp",
		map[string]any{"server_url": "http://127.0.0.1:1", "phone_number": "15550001111", "session": "default"},
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
		map[string]string{"integrationUrl": "http://127.0.0.1:1/integration/SECRET-pagertree", "authToken": "pagertree-SECRET-auth"}},
	{"line",
		map[string]any{"user_id": "U123"},
		map[string]string{"channel_access_token": "line-SECRET-token"}},
	{"teams",
		map[string]any{},
		map[string]string{"webhook_url": "http://127.0.0.1:1/webhookb2/SECRET-teams"}},
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
	// The install email channel has no secret to hide, which TestSecretKeysPerProvider pins, and it
	// can't be saved without a sender and a member to send to: install_email_channel_test.go
	// covers it
	registered := slices.DeleteFunc(a.registeredChannelTypes(t), func(channelType string) bool { return channelType == "install_email" })
	assert.Equal(t, registered, fixtureTypes, "every notification channel type needs a case in secretChannels")
}

// A channel can outlive its type: the row stays when the provider is removed. Nothing knows which
// of its fields are secrets, so its config is not shown.
func TestNotificationChannelsAPI_AChannelOfAnUnknownTypeShowsNoConfig(t *testing.T) {
	a := newSecretsApp(t)
	var id string
	first := a.firstWorkspace(t)
	require.NoError(t, a.container.Invoke(func(channels notification_channel.Service) error {
		created, err := channels.Create(context.Background(), first, &notification_channel.CreateUpdateDto{
			Name: "Retired", Type: "retired-type", Active: true, Config: `{"api_token":"retired-SECRET-token"}`,
		})
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	}))

	w := a.request(t, http.MethodGet, "/api/v1/notification-channels/"+id, nil, a.asAdmin)
	got := decode[channelWithConfig](t, w, http.StatusOK)
	assert.Equal(t, "retired-type", got.Type)
	assert.Empty(t, got.Config)
	assert.NotContains(t, w.Body.String(), secretMarker)

	w = a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin)
	assert.Len(t, decode[[]channelWithConfig](t, w, http.StatusOK), 1)
	assert.NotContains(t, w.Body.String(), secretMarker, "the list shows a secret")
}

func TestNotificationChannelsAPI_ResponsesHideSecrets(t *testing.T) {
	a := newSecretsApp(t)

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

// urlCredentialCase is a channel whose URL field holds credentials: the URL it is saved with, and
// the URL the API shows. The password and the tokens are hunter2 and tok123 wherever they are.
type urlCredentialCase struct {
	channelType string
	// key is the config field that holds the URL
	key    string
	stored string
	shown  string
}

var urlCredentialCases = []urlCredentialCase{
	{"webhook", "webhook_url", "http://user:hunter2@127.0.0.1:1/hook?token=hunter2&channel=ops", "http://user:********@127.0.0.1:1/hook?token=********&channel=********"},
	{"ntfy", "server_url", "http://tok123@127.0.0.1:1", "http://********@127.0.0.1:1"},
	{"signal", "signal_url", "http://signal:hunter2@127.0.0.1:1/?apikey=hunter2", "http://signal:********@127.0.0.1:1/?apikey=********"},
	{"gotify", "server_url", "http://user:hunter2@127.0.0.1:1/gotify?token=hunter2", "http://user:********@127.0.0.1:1/gotify?token=********"},
	{"matrix", "homeserver_url", "http://tok123@127.0.0.1:1/?access_token=hunter2", "http://********@127.0.0.1:1/?access_token=********"},
	{"pagerduty", "pagerduty_integration_url", "http://user:hunter2@127.0.0.1:1/v2/enqueue", "http://user:********@127.0.0.1:1/v2/enqueue"},
	{"telegram", "server_url", "http://user:hunter2@127.0.0.1:1", "http://user:********@127.0.0.1:1"},
	{"whatsapp", "server_url", "http://127.0.0.1:1/wa?session=hunter2", "http://127.0.0.1:1/wa?session=********"},
}

// storedChannel returns the channel with the URL it is saved with
func (u urlCredentialCase) storedChannel(t *testing.T) secretChannel {
	t.Helper()
	return secretChannelOfType(t, u.channelType).withVisible(map[string]any{u.key: u.stored})
}

// shownChannel returns the channel with the URL the API shows
func (u urlCredentialCase) shownChannel(t *testing.T) secretChannel {
	t.Helper()
	return secretChannelOfType(t, u.channelType).withVisible(map[string]any{u.key: u.shown})
}

// requireURLHidden checks that a response shows the channel with its secrets and the credentials
// of its URL masked, and nothing else changed
func (u urlCredentialCase) requireURLHidden(t *testing.T, w *httptest.ResponseRecorder, got channelWithConfig) {
	t.Helper()
	assert.NotContains(t, w.Body.String(), "hunter2", "the response shows a password or a query value")
	assert.NotContains(t, w.Body.String(), "tok123", "the response shows a user name that is a token")
	assert.NotContains(t, w.Body.String(), secretMarker, "the response shows a secret")
	assert.JSONEq(t, u.shownChannel(t).redactedConfig(), got.Config)
}

func TestNotificationChannelsAPI_ResponsesMaskURLCredentials(t *testing.T) {
	a := newSecretsApp(t)

	for _, u := range urlCredentialCases {
		t.Run(u.channelType, func(t *testing.T) {
			c := u.storedChannel(t)

			w := a.request(t, http.MethodPost, "/api/v1/notification-channels", c.body("Channel"), a.asAdmin)
			createdChannel := decode[channelWithConfig](t, w, http.StatusCreated)
			u.requireURLHidden(t, w, createdChannel)
			path := "/api/v1/notification-channels/" + createdChannel.ID

			w = a.request(t, http.MethodGet, path, nil, a.asAdmin)
			u.requireURLHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			w = a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin)
			found := false
			for _, listed := range decode[[]channelWithConfig](t, w, http.StatusOK) {
				if listed.ID == createdChannel.ID {
					found = true
					assert.JSONEq(t, u.shownChannel(t).redactedConfig(), listed.Config)
				}
			}
			assert.True(t, found, "the list doesn't have the channel")
			assert.NotContains(t, w.Body.String(), "hunter2", "the list shows a password or a query value")
			assert.NotContains(t, w.Body.String(), "tok123", "the list shows a token")

			w = a.request(t, http.MethodPut, path, c.body("Renamed"), a.asAdmin)
			u.requireURLHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			w = a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed again", "config": c.config(nil)}, a.asAdmin)
			u.requireURLHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			w = a.request(t, http.MethodPatch, path, map[string]any{"name": "Renamed last"}, a.asAdmin)
			u.requireURLHidden(t, w, decode[channelWithConfig](t, w, http.StatusOK))

			assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, createdChannel.ID), "the stored config keeps the URL as it was saved")
		})
	}
}
