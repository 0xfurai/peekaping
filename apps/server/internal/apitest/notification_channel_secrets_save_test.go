package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"peekaping/internal/modules/notification_channel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionalSecrets are the secrets a channel is valid without, by channel type
var optionalSecrets = map[string][]string{
	"webhook":   {"webhook_signing_secret", "webhook_additional_headers"},
	"slack":     {"slack_webhook_url", "slack_bot_token"},
	"ntfy":      {"password", "token"},
	"pagertree": {"authToken"},
	"twilio":    {"twilio_api_key"},
	"whatsapp":  {"api_key"},
}

// visibleEndpoints are the config fields that decide where a channel sends its secrets and are
// not secrets themselves, by channel type, each with a value to change it to
var visibleEndpoints = map[string]map[string]any{
	"smtp":      {"smtp_host": "127.0.0.2", "smtp_port": 2, "smtp_secure": true},
	"telegram":  {"server_url": "http://127.0.0.1:2"},
	"gotify":    {"server_url": "http://127.0.0.1:2"},
	"matrix":    {"homeserver_url": "http://127.0.0.1:2"},
	"ntfy":      {"server_url": "http://127.0.0.1:2"},
	"pagerduty": {"pagerduty_integration_url": "http://127.0.0.1:2/v2/enqueue"},
	"whatsapp":  {"server_url": "http://127.0.0.1:2"},
	"webhook":   {"webhook_url": "http://127.0.0.1:2/hook"},
}

// secretEndpoints are the secrets that are themselves where a channel sends, by channel type
var secretEndpoints = map[string]string{
	"discord":        "webhook_url",
	"google_chat":    "webhook_url",
	"grafana_oncall": "grafana_oncall_url",
	"mattermost":     "webhook_url",
	"pagertree":      "integrationUrl",
	"slack":          "slack_webhook_url",
	"teams":          "webhook_url",
	"wecom":          "webhook_url",
}

// storedChannelConfig returns the config a channel is saved with, which the API doesn't show
func (a *app) storedChannelConfig(t *testing.T, id string) string {
	t.Helper()
	var config string
	require.NoError(t, a.container.Invoke(func(channels notification_channel.Service) error {
		channel, err := channels.FindByID(context.Background(), id)
		if err != nil {
			return err
		}
		require.NotNil(t, channel, id)
		require.NotNil(t, channel.Config, id)
		config = *channel.Config
		return nil
	}))
	return config
}

// createSecretChannel saves the channel with its real secrets and returns its ID
func (a *app) createSecretChannel(t *testing.T, c secretChannel) string {
	t.Helper()
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/notification-channels", c.body("Channel"), a.asAdmin), http.StatusCreated).ID
}

func (c secretChannel) secretKeys() []string {
	keys := []string{}
	for key := range c.secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// withPlaceholders returns the config with every secret as the placeholder, except the overrides
func (c secretChannel) withPlaceholders(overrides map[string]string) string {
	values := map[string]string{}
	for key := range c.secrets {
		values[key] = secretPlaceholder
	}
	for key, value := range overrides {
		values[key] = value
	}
	return c.config(values)
}

// withVisible returns the channel with some of its visible config fields changed
func (c secretChannel) withVisible(overrides map[string]any) secretChannel {
	visible := map[string]any{}
	for key, value := range c.visible {
		visible[key] = value
	}
	for key, value := range overrides {
		visible[key] = value
	}
	c.visible = visible
	return c
}

// without returns the channel with some of its visible config fields left out
func (c secretChannel) without(keys ...string) secretChannel {
	visible := map[string]any{}
	for key, value := range c.visible {
		visible[key] = value
	}
	for _, key := range keys {
		delete(visible, key)
	}
	c.visible = visible
	return c
}

func secretChannelOfType(t *testing.T, channelType string) secretChannel {
	t.Helper()
	for _, c := range secretChannels {
		if c.channelType == channelType {
			return c
		}
	}
	t.Fatalf("no secret channel of type %s", channelType)
	return secretChannel{}
}

// put replaces the channel with a channel of the given type and config
func (a *app) put(t *testing.T, id, channelType, config string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"name": "Channel", "type": channelType, "active": true, "config": config}
	return a.request(t, http.MethodPut, "/api/v1/notification-channels/"+id, body, a.asAdmin)
}

// patch changes the channel's config, and its type when channelType isn't empty
func (a *app) patch(t *testing.T, id, channelType, config string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"config": config}
	if channelType != "" {
		body["type"] = channelType
	}
	return a.request(t, http.MethodPatch, "/api/v1/notification-channels/"+id, body, a.asAdmin)
}

// requireRefusedAsPlaceholder checks that a request was refused because a placeholder couldn't
// be resolved, and that it named the secrets
func requireRefusedAsPlaceholder(t *testing.T, w *httptest.ResponseRecorder, message string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, message+": "+w.Body.String())
	assert.Contains(t, w.Body.String(), "re-enter the saved secrets (", message)
	assert.NotContains(t, w.Body.String(), secretMarker, message)
}

func TestNotificationChannelsAPI_SavingWithPlaceholdersKeepsSecrets(t *testing.T) {
	a := newSecretsApp(t)

	for _, c := range secretChannels {
		t.Run(c.channelType, func(t *testing.T) {
			id := a.createSecretChannel(t, c)
			path := "/api/v1/notification-channels/" + id

			decode[any](t, a.put(t, id, c.channelType, c.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "PUT")

			decode[any](t, a.patch(t, id, "", c.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "PATCH")

			// The form of the web app: the config it got, saved back as it is
			shown := decode[channelWithConfig](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
			decode[any](t, a.put(t, id, c.channelType, shown.Config), http.StatusOK)
			assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "PUT of the config the API showed")
		})
	}
}

func TestNotificationChannelsAPI_SavingANewValueReplacesTheSecret(t *testing.T) {
	a := newSecretsApp(t)

	for _, c := range secretChannels {
		for _, key := range c.secretKeys() {
			t.Run(c.channelType+"/"+key, func(t *testing.T) {
				id := a.createSecretChannel(t, c)
				replacement := strings.Replace(c.secrets[key], secretMarker, "NEW"+secretMarker, 1)
				require.NotEqual(t, c.secrets[key], replacement)
				replaced := c.config(map[string]string{key: replacement})

				if secretEndpoints[c.channelType] == key && len(c.secrets) > 1 {
					// A new secret URL sends the other secrets somewhere else, so they have to be entered again
					requireRefusedAsPlaceholder(t, a.put(t, id, c.channelType, c.withPlaceholders(map[string]string{key: replacement})), "PUT")
					requireRefusedAsPlaceholder(t, a.patch(t, id, "", c.withPlaceholders(map[string]string{key: replacement})), "PATCH")
					assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "refused")

					decode[any](t, a.put(t, id, c.channelType, replaced), http.StatusOK)
					assert.JSONEq(t, replaced, a.storedChannelConfig(t, id), "PUT with every secret entered again")
					return
				}

				decode[any](t, a.put(t, id, c.channelType, c.withPlaceholders(map[string]string{key: replacement})), http.StatusOK)
				assert.JSONEq(t, replaced, a.storedChannelConfig(t, id), "PUT")

				decode[any](t, a.patch(t, id, "", c.withPlaceholders(map[string]string{key: c.secrets[key]})), http.StatusOK)
				assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "PATCH")
			})
		}
	}
}

func TestNotificationChannelsAPI_SavingAnEmptyValueClearsAnOptionalSecret(t *testing.T) {
	a := newSecretsApp(t)

	for _, c := range secretChannels {
		for _, key := range optionalSecrets[c.channelType] {
			t.Run(c.channelType+"/"+key, func(t *testing.T) {
				id := a.createSecretChannel(t, c)
				path := "/api/v1/notification-channels/" + id
				cleared := c.config(map[string]string{key: ""})
				withCleared := c.withPlaceholders(map[string]string{key: ""})

				if secretEndpoints[c.channelType] == key && len(c.secrets) > 1 {
					// Clearing a secret URL changes where the other secrets are sent
					requireRefusedAsPlaceholder(t, a.put(t, id, c.channelType, withCleared), "PUT")
					assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "refused")
					return
				}

				decode[any](t, a.put(t, id, c.channelType, withCleared), http.StatusOK)
				assert.JSONEq(t, cleared, a.storedChannelConfig(t, id), "PUT")

				// The other secrets are still there, and the response shows the cleared one as empty
				shown := decode[channelWithConfig](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
				assert.JSONEq(t, withCleared, shown.Config)

				decode[any](t, a.patch(t, id, "", c.config(nil)), http.StatusOK)
				decode[any](t, a.patch(t, id, "", withCleared), http.StatusOK)
				assert.JSONEq(t, cleared, a.storedChannelConfig(t, id), "PATCH")
			})
		}
	}
}

func TestNotificationChannelsAPI_APlaceholderOnACreatedChannelIsRefused(t *testing.T) {
	a := newSecretsApp(t)

	for _, c := range secretChannels {
		if len(c.secrets) == 0 {
			continue
		}
		t.Run(c.channelType, func(t *testing.T) {
			for _, key := range c.secretKeys() {
				body := with(c.body("Channel"), "config", c.config(map[string]string{key: secretPlaceholder}))
				requireRefusedAsPlaceholder(t, a.request(t, http.MethodPost, "/api/v1/notification-channels", body, a.asAdmin), key)
			}
		})
	}

	list := decode[[]channelWithConfig](t, a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin), http.StatusOK)
	assert.Empty(t, list, "no channel was created")
}

func TestNotificationChannelsAPI_AChangedEndpointRefusesThePlaceholders(t *testing.T) {
	a := newSecretsApp(t)

	for channelType, changes := range visibleEndpoints {
		c := secretChannelOfType(t, channelType)
		for field, value := range changes {
			t.Run(channelType+"/"+field, func(t *testing.T) {
				id := a.createSecretChannel(t, c)
				moved := c.withVisible(map[string]any{field: value})

				requireRefusedAsPlaceholder(t, a.put(t, id, channelType, moved.withPlaceholders(nil)), "PUT")
				requireRefusedAsPlaceholder(t, a.patch(t, id, "", moved.withPlaceholders(nil)), "PATCH")
				assert.JSONEq(t, c.config(nil), a.storedChannelConfig(t, id), "refused")

				// The same change with every secret entered again is saved
				decode[any](t, a.put(t, id, channelType, moved.config(nil)), http.StatusOK)
				assert.JSONEq(t, moved.config(nil), a.storedChannelConfig(t, id), "PUT with every secret entered again")
			})
		}
	}
}

// The web form fills in a field a stored config leaves out with the value the sender uses. A form
// the user didn't touch must still count as the saved endpoint.
func TestNotificationChannelsAPI_AnEndpointLeftAtItsDefaultIsUnchanged(t *testing.T) {
	a := newSecretsApp(t)

	t.Run("telegram", func(t *testing.T) {
		telegramAPI := startRequestReceiver(t)
		routeHostTo(t, "api.telegram.org", telegramAPI)
		withoutServer := secretChannelOfType(t, "telegram").without("server_url")
		withDefault := withoutServer.withVisible(map[string]any{"server_url": "https://api.telegram.org"})

		t.Run("a saved channel without a server_url, sent with the default one", func(t *testing.T) {
			id := a.createSecretChannel(t, withoutServer)

			decode[any](t, a.testChannel(t, id, "telegram", withDefault.withPlaceholders(nil)), http.StatusOK)
			assert.Equal(t, "/bot123456:SECRET-telegram-token/sendMessage", telegramAPI.requireOne(t).path, "the saved token, to the default server")

			decode[any](t, a.put(t, id, "telegram", withDefault.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, withDefault.config(nil), a.storedChannelConfig(t, id), "PUT")

			otherID := a.createSecretChannel(t, withoutServer)
			decode[any](t, a.patch(t, otherID, "", withDefault.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, withDefault.config(nil), a.storedChannelConfig(t, otherID), "PATCH")
		})

		t.Run("a saved channel with the default server_url, sent without one", func(t *testing.T) {
			id := a.createSecretChannel(t, withDefault)

			decode[any](t, a.testChannel(t, id, "telegram", withoutServer.withPlaceholders(nil)), http.StatusOK)
			assert.Equal(t, "/bot123456:SECRET-telegram-token/sendMessage", telegramAPI.requireOne(t).path)

			decode[any](t, a.put(t, id, "telegram", withoutServer.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, withoutServer.config(nil), a.storedChannelConfig(t, id), "PUT")
		})

		t.Run("another server is still refused", func(t *testing.T) {
			id := a.createSecretChannel(t, withoutServer)
			moved := withoutServer.withVisible(map[string]any{"server_url": "http://127.0.0.1:2"})

			requireRefusedAsPlaceholder(t, a.put(t, id, "telegram", moved.withPlaceholders(nil)), "PUT")
			requireRefusedAsPlaceholder(t, a.patch(t, id, "", moved.withPlaceholders(nil)), "PATCH")
			requireRefusedAsPlaceholder(t, a.testChannel(t, id, "telegram", moved.withPlaceholders(nil)), "Test")
			assert.Empty(t, telegramAPI.received())
			assert.JSONEq(t, withoutServer.config(nil), a.storedChannelConfig(t, id))
		})
	})

	t.Run("smtp", func(t *testing.T) {
		smtp := secretChannelOfType(t, "smtp") // saved without smtp_secure
		insecure := smtp.withVisible(map[string]any{"smtp_secure": false})
		secure := smtp.withVisible(map[string]any{"smtp_secure": true})

		t.Run("smtp_secure false is the saved endpoint of a config without it", func(t *testing.T) {
			id := a.createSecretChannel(t, smtp)

			decode[any](t, a.put(t, id, "smtp", insecure.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, insecure.config(nil), a.storedChannelConfig(t, id), "PUT")
			decode[any](t, a.patch(t, id, "", smtp.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, smtp.config(nil), a.storedChannelConfig(t, id), "PATCH back to a config without it")

			// The test goes on to send, to an address that refuses connections
			w := a.testChannel(t, id, "smtp", insecure.withPlaceholders(nil))
			assert.NotContains(t, w.Body.String(), "re-enter the saved secrets")
		})

		t.Run("smtp_secure true is another endpoint", func(t *testing.T) {
			id := a.createSecretChannel(t, smtp)

			requireRefusedAsPlaceholder(t, a.put(t, id, "smtp", secure.withPlaceholders(nil)), "PUT")
			requireRefusedAsPlaceholder(t, a.patch(t, id, "", secure.withPlaceholders(nil)), "PATCH")
			requireRefusedAsPlaceholder(t, a.testChannel(t, id, "smtp", secure.withPlaceholders(nil)), "Test")
			assert.JSONEq(t, smtp.config(nil), a.storedChannelConfig(t, id))
		})
	})
}

// A PATCH that names no config keeps the stored one. Under another type that config is read by
// another provider, which doesn't know the old type's secret keys: the old secret would come
// back in the clear. A new type needs the config that goes with it.
func TestNotificationChannelsAPI_PatchingTheTypeWithoutAConfigIsRefused(t *testing.T) {
	a := newSecretsApp(t)

	const wantMessage = "Invalid config: send the config when changing the type"
	cases := []struct {
		name string
		from secretChannel
		// extras are keys of the type it changes to, which the saved type keeps without knowing them
		extras map[string]any
		toType string
	}{
		{
			name:   "telegram to signal",
			from:   secretChannelOfType(t, "telegram"),
			extras: map[string]any{"signal_url": "https://signal.example", "signal_number": "1", "signal_recipients": "2"},
			toType: "signal",
		},
		{
			name:   "discord to webhook",
			from:   secretChannelOfType(t, "discord"),
			extras: map[string]any{"webhook_content_type": "json"},
			toType: "webhook",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channel := tc.from.withVisible(tc.extras)
			id := a.createSecretChannel(t, channel)
			path := "/api/v1/notification-channels/" + id
			// The way to get the extra keys in is a save of the form the API showed, with an addition
			decode[any](t, a.put(t, id, channel.channelType, channel.withPlaceholders(nil)), http.StatusOK)

			w := a.request(t, http.MethodPatch, path, map[string]any{"type": tc.toType}, a.asAdmin)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), wantMessage)
			assert.NotContains(t, w.Body.String(), secretMarker)

			w = a.request(t, http.MethodGet, path, nil, a.asAdmin)
			got := decode[channelWithConfig](t, w, http.StatusOK)
			assert.Equal(t, tc.from.channelType, got.Type)
			assert.NotContains(t, w.Body.String(), secretMarker, "a later GET shows a secret")
			assert.JSONEq(t, channel.redactedConfig(), got.Config)
			assert.JSONEq(t, channel.config(nil), a.storedChannelConfig(t, id))
		})
	}

	t.Run("a type with the config that goes with it is saved", func(t *testing.T) {
		channel := secretChannelOfType(t, "telegram")
		signal := secretChannelOfType(t, "signal")
		id := a.createSecretChannel(t, channel)
		path := "/api/v1/notification-channels/" + id

		w := a.request(t, http.MethodPatch, path, map[string]any{"type": "signal", "config": signal.config(nil)}, a.asAdmin)

		decode[any](t, w, http.StatusOK)
		w = a.request(t, http.MethodGet, path, nil, a.asAdmin)
		got := decode[channelWithConfig](t, w, http.StatusOK)
		assert.Equal(t, "signal", got.Type)
		assert.JSONEq(t, signal.config(nil), got.Config)
		assert.NotContains(t, w.Body.String(), secretMarker)
	})

	t.Run("naming the stored type again needs no config", func(t *testing.T) {
		channel := secretChannelOfType(t, "telegram")
		id := a.createSecretChannel(t, channel)

		w := a.request(t, http.MethodPatch, "/api/v1/notification-channels/"+id, map[string]any{"type": "telegram"}, a.asAdmin)

		decode[any](t, w, http.StatusOK)
		assert.JSONEq(t, channel.config(nil), a.storedChannelConfig(t, id))
	})

	t.Run("an unknown type is still unsupported", func(t *testing.T) {
		id := a.createSecretChannel(t, secretChannelOfType(t, "telegram"))

		w := a.request(t, http.MethodPatch, "/api/v1/notification-channels/"+id, map[string]any{"type": "carrier-pigeon"}, a.asAdmin)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "Unsupported notification type")
	})
}

func TestNotificationChannelsAPI_AChangedTypeDoesNotKeepTheOldSecrets(t *testing.T) {
	a := newSecretsApp(t)
	smtp := secretChannelOfType(t, "smtp")
	ntfy := secretChannelOfType(t, "ntfy")
	sendgrid := secretChannelOfType(t, "sendgrid")
	opsgenie := secretChannelOfType(t, "opsgenie")

	t.Run("an optional secret with the same key is refused", func(t *testing.T) {
		// Both types keep their password under "password"
		id := a.createSecretChannel(t, smtp)
		placeholder := ntfy.config(map[string]string{"password": secretPlaceholder, "token": "", "authentication_type": "none"})

		requireRefusedAsPlaceholder(t, a.put(t, id, "ntfy", placeholder), "PUT")
		requireRefusedAsPlaceholder(t, a.patch(t, id, "ntfy", placeholder), "PATCH")
		assert.JSONEq(t, smtp.config(nil), a.storedChannelConfig(t, id))
	})

	t.Run("new values are saved, without the old secrets", func(t *testing.T) {
		id := a.createSecretChannel(t, smtp)
		entered := ntfy.config(map[string]string{"password": "ntfy-NEWSECRET-password", "token": "", "authentication_type": "none"})

		decode[any](t, a.put(t, id, "ntfy", entered), http.StatusOK)

		stored := a.storedChannelConfig(t, id)
		assert.NotContains(t, stored, "smtp-SECRET-password")
		assert.JSONEq(t, entered, stored)
	})

	t.Run("a required secret is refused", func(t *testing.T) {
		// Both types keep their key under "api_key"
		id := a.createSecretChannel(t, sendgrid)
		path := "/api/v1/notification-channels/" + id

		requireRefusedAsPlaceholder(t, a.put(t, id, "opsgenie", opsgenie.withPlaceholders(nil)), "PUT")
		requireRefusedAsPlaceholder(t, a.patch(t, id, "opsgenie", opsgenie.withPlaceholders(nil)), "PATCH")

		assert.JSONEq(t, sendgrid.config(nil), a.storedChannelConfig(t, id))
		assert.Equal(t, "sendgrid", decode[channelWithConfig](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK).Type)
	})
}

// The web form saves the config it got back as it is. A URL the API showed with its credentials
// masked must keep the stored URL, and the channel's other saved secrets with it.
func TestNotificationChannelsAPI_SavingTheMaskedURLKeepsTheStoredURL(t *testing.T) {
	a := newSecretsApp(t)

	for _, u := range urlCredentialCases {
		t.Run(u.channelType, func(t *testing.T) {
			stored := u.storedChannel(t)
			shown := u.shownChannel(t)
			id := a.createSecretChannel(t, stored)
			path := "/api/v1/notification-channels/" + id

			decode[any](t, a.put(t, id, u.channelType, shown.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, stored.config(nil), a.storedChannelConfig(t, id), "PUT")

			decode[any](t, a.patch(t, id, "", shown.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, stored.config(nil), a.storedChannelConfig(t, id), "PATCH")

			// The form of the web app: the config it got, saved back as it is
			got := decode[channelWithConfig](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
			decode[any](t, a.put(t, id, u.channelType, got.Config), http.StatusOK)
			assert.JSONEq(t, stored.config(nil), a.storedChannelConfig(t, id), "PUT of the config the API showed")

			// Another field changes while the URL is kept
			renamed := shown.withVisible(map[string]any{"future_field": "x"})
			decode[any](t, a.put(t, id, u.channelType, renamed.withPlaceholders(nil)), http.StatusOK)
			assert.JSONEq(t, stored.withVisible(map[string]any{"future_field": "x"}).config(nil), a.storedChannelConfig(t, id), "PUT with another field changed")
		})
	}
}

// A URL that is typed anew is saved as typed. It moves the channel, so its secrets are typed too.
func TestNotificationChannelsAPI_SavingANewURLReplacesTheStoredURL(t *testing.T) {
	a := newSecretsApp(t)
	u := urlCredentialCases[0]
	id := a.createSecretChannel(t, u.storedChannel(t))

	typed := "http://other:newpw@127.0.0.1:2/hook?token=newtoken"
	decode[any](t, a.put(t, id, "webhook", u.storedChannel(t).withVisible(map[string]any{"webhook_url": typed}).config(nil)), http.StatusOK)
	assert.JSONEq(t, u.storedChannel(t).withVisible(map[string]any{"webhook_url": typed}).config(nil), a.storedChannelConfig(t, id))
}

// A URL that still holds the placeholder but is not exactly the one the API showed is refused, so
// that a masked password is never sent to a new place or kept under a new URL
func TestNotificationChannelsAPI_AChangedMaskedURLIsRefused(t *testing.T) {
	a := newSecretsApp(t)

	for _, u := range urlCredentialCases {
		t.Run(u.channelType, func(t *testing.T) {
			stored := u.storedChannel(t)
			id := a.createSecretChannel(t, stored)
			before := a.storedChannelConfig(t, id)

			moved := strings.Replace(u.shown, "127.0.0.1:1", "127.0.0.2:1", 1)
			config := u.shownChannel(t).withVisible(map[string]any{u.key: moved}).withPlaceholders(nil)

			w := a.put(t, id, u.channelType, config)
			requireRefusedAsPlaceholder(t, w, "PUT")
			assert.Contains(t, w.Body.String(), u.key, "PUT names the URL")
			w = a.patch(t, id, "", config)
			requireRefusedAsPlaceholder(t, w, "PATCH")
			assert.Contains(t, w.Body.String(), u.key, "PATCH names the URL")

			// Typing over one of the masked parts changes the URL, so the secrets are not kept for it
			edited := strings.Replace(u.shown, "********", "mine", 1)
			w = a.put(t, id, u.channelType, u.shownChannel(t).withVisible(map[string]any{u.key: edited}).withPlaceholders(nil))
			requireRefusedAsPlaceholder(t, w, "PUT with the first placeholder typed over")

			assert.Equal(t, before, a.storedChannelConfig(t, id), "nothing is saved")
		})
	}
}

func TestNotificationChannelsAPI_AMaskedURLOnACreatedChannelIsRefused(t *testing.T) {
	a := newSecretsApp(t)

	for _, u := range urlCredentialCases {
		t.Run(u.channelType, func(t *testing.T) {
			w := a.request(t, http.MethodPost, "/api/v1/notification-channels", u.shownChannel(t).body("Channel"), a.asAdmin)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "re-enter the saved secrets (")
			assert.Contains(t, w.Body.String(), u.key)
		})
	}
}

// A masked URL of one type means nothing under another
func TestNotificationChannelsAPI_AMaskedURLAfterATypeChangeIsRefused(t *testing.T) {
	a := newSecretsApp(t)
	webhook := urlCredentialCases[0]
	signal := urlCredentialCases[2]
	id := a.createSecretChannel(t, webhook.storedChannel(t))
	before := a.storedChannelConfig(t, id)

	// The signal config sends the masked webhook URL back as its own
	config := signal.shownChannel(t).withVisible(map[string]any{"signal_url": webhook.shown}).withPlaceholders(nil)

	w := a.put(t, id, "signal", config)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "re-enter the saved secrets (signal_url)")
	w = a.patch(t, id, "signal", config)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "re-enter the saved secrets (signal_url)")
	assert.Equal(t, before, a.storedChannelConfig(t, id), "nothing is saved")
}
