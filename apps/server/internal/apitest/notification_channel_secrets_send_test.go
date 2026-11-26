package apitest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receivedRequest is a request as the server it was sent to sees it
type receivedRequest struct {
	path   string
	header http.Header
	body   []byte
}

// requestReceiver is a server that records what it is sent
type requestReceiver struct {
	url      string
	mu       sync.Mutex
	requests []receivedRequest
}

// startRequestReceiver serves a local endpoint that answers every request with a 200
func startRequestReceiver(t *testing.T) *requestReceiver {
	t.Helper()
	receiver := &requestReceiver{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receiver.mu.Lock()
		receiver.requests = append(receiver.requests, receivedRequest{path: r.URL.Path, header: r.Header.Clone(), body: body})
		receiver.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	receiver.url = server.URL
	return receiver
}

// routeHostTo sends the requests the app makes to a host, which the test blocks, to the receiver
// instead. Call it after newSecretsApp, which blocks the hosts that are not on this machine and
// keeps the test serial: the transport is the process's.
func routeHostTo(t *testing.T, host string, receiver *requestReceiver) {
	t.Helper()
	serial(t)
	target, err := url.Parse(receiver.url)
	require.NoError(t, err)
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != host {
			return previous.RoundTrip(req)
		}
		redirected := req.Clone(req.Context())
		redirected.URL.Scheme, redirected.URL.Host = target.Scheme, target.Host
		return previous.RoundTrip(redirected)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
}

// received returns the requests the server has been sent
func (r *requestReceiver) received() []receivedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]receivedRequest(nil), r.requests...)
}

// requireOne returns the one request the server was sent, and forgets it
func (r *requestReceiver) requireOne(t *testing.T) receivedRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.requests, 1, "the server should have received one request")
	request := r.requests[0]
	r.requests = nil
	return request
}

// signatureIsValid checks a webhook the way its receiver does: the signature is the HMAC-SHA256
// of "<timestamp>.<body>", keyed with the secret
func signatureIsValid(request receivedRequest, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(request.header.Get("X-Webhook-Timestamp") + "."))
	mac.Write(request.body)
	return request.header.Get("X-Webhook-Signature") == "sha256="+hex.EncodeToString(mac.Sum(nil))
}

// testChannel asks to test a channel of the given type and config. id is the saved channel it
// edits, and is left out when empty.
func (a *app) testChannel(t *testing.T, id, channelType, config string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"name": "Channel", "type": channelType, "active": true, "config": config}
	if id != "" {
		body["id"] = id
	}
	return a.request(t, http.MethodPost, "/api/v1/notification-channels/test", body, a.asAdmin)
}

func TestNotificationChannelsAPI_TestSendsWithTheStoredSecrets(t *testing.T) {
	a := newSecretsApp(t)
	hook := startRequestReceiver(t)
	webhook := secretChannelOfType(t, "webhook").withVisible(map[string]any{"webhook_url": hook.url + "/hook"})
	id := a.createSecretChannel(t, webhook)
	stored := a.storedChannelConfig(t, id)

	t.Run("placeholders send with the stored secrets", func(t *testing.T) {
		decode[any](t, a.testChannel(t, id, "webhook", webhook.withPlaceholders(nil)), http.StatusOK)

		got := hook.requireOne(t)
		assert.Equal(t, "SECRET-header", got.header.Get("X-Api-Key"))
		assert.True(t, signatureIsValid(got, "whsec-SECRET-signing"), "the signature is made with the stored signing secret")
		assert.Contains(t, string(got.body), "This is a test notification from Peekaping")
	})

	t.Run("a new value is sent instead of the stored one", func(t *testing.T) {
		config := webhook.withPlaceholders(map[string]string{"webhook_signing_secret": "whsec-NEWSECRET-signing", "webhook_additional_headers": `{"X-Api-Key":"NEWSECRET-header"}`})
		decode[any](t, a.testChannel(t, id, "webhook", config), http.StatusOK)

		got := hook.requireOne(t)
		assert.Equal(t, "NEWSECRET-header", got.header.Get("X-Api-Key"))
		assert.True(t, signatureIsValid(got, "whsec-NEWSECRET-signing"))
	})

	t.Run("an empty value is sent empty", func(t *testing.T) {
		config := webhook.withPlaceholders(map[string]string{"webhook_signing_secret": "", "webhook_additional_headers": ""})
		decode[any](t, a.testChannel(t, id, "webhook", config), http.StatusOK)

		got := hook.requireOne(t)
		assert.Empty(t, got.header.Get("X-Api-Key"))
		assert.Empty(t, got.header.Get("X-Webhook-Signature"))
	})

	t.Run("without an id placeholders are refused", func(t *testing.T) {
		requireRefusedAsPlaceholder(t, a.testChannel(t, "", "webhook", webhook.withPlaceholders(nil)), "no id")
		assert.Empty(t, hook.received(), "nothing is sent")
	})

	t.Run("a channel that doesn't exist is not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, a.testChannel(t, "no-such-channel", "webhook", webhook.withPlaceholders(nil)).Code)
		assert.Empty(t, hook.received(), "nothing is sent")
	})

	assert.Equal(t, stored, a.storedChannelConfig(t, id), "testing a channel doesn't change it")
}

// A caller who can edit a channel can point it anywhere. Its saved secrets must not follow.
func TestNotificationChannelsAPI_ASavedSecretIsNotSentToAnotherEndpoint(t *testing.T) {
	a := newSecretsApp(t)
	saved := startRequestReceiver(t)
	elsewhere := startRequestReceiver(t)

	t.Run("telegram", func(t *testing.T) {
		telegram := secretChannelOfType(t, "telegram").withVisible(map[string]any{"server_url": saved.url})
		moved := telegram.withVisible(map[string]any{"server_url": elsewhere.url})
		id := a.createSecretChannel(t, telegram)

		requireRefusedAsPlaceholder(t, a.put(t, id, "telegram", moved.withPlaceholders(nil)), "PUT")
		requireRefusedAsPlaceholder(t, a.patch(t, id, "", moved.withPlaceholders(nil)), "PATCH")
		requireRefusedAsPlaceholder(t, a.testChannel(t, id, "telegram", moved.withPlaceholders(nil)), "Test")
		assert.Empty(t, elsewhere.received(), "the bot token went to the other server")
		assert.Empty(t, saved.received())
		assert.JSONEq(t, telegram.config(nil), a.storedChannelConfig(t, id))

		// With the endpoint as it was saved, the placeholder is the saved token
		decode[any](t, a.testChannel(t, id, "telegram", telegram.withPlaceholders(nil)), http.StatusOK)
		assert.Equal(t, "/bot123456:SECRET-telegram-token/sendMessage", saved.requireOne(t).path)
		assert.Empty(t, elsewhere.received())
	})

	t.Run("webhook", func(t *testing.T) {
		webhook := secretChannelOfType(t, "webhook").withVisible(map[string]any{"webhook_url": saved.url + "/hook"})
		moved := webhook.withVisible(map[string]any{"webhook_url": elsewhere.url + "/hook"})
		id := a.createSecretChannel(t, webhook)

		requireRefusedAsPlaceholder(t, a.put(t, id, "webhook", moved.withPlaceholders(nil)), "PUT")
		requireRefusedAsPlaceholder(t, a.patch(t, id, "", moved.withPlaceholders(nil)), "PATCH")
		requireRefusedAsPlaceholder(t, a.testChannel(t, id, "webhook", moved.withPlaceholders(nil)), "Test")
		// Even with only the signing secret left as the placeholder
		onlySigning := moved.withPlaceholders(map[string]string{"webhook_additional_headers": ""})
		requireRefusedAsPlaceholder(t, a.testChannel(t, id, "webhook", onlySigning), "Test with one placeholder")
		assert.Empty(t, elsewhere.received(), "the signing secret went to the other endpoint")
		assert.Empty(t, saved.received())
		assert.JSONEq(t, webhook.config(nil), a.storedChannelConfig(t, id))

		decode[any](t, a.testChannel(t, id, "webhook", webhook.withPlaceholders(nil)), http.StatusOK)
		got := saved.requireOne(t)
		assert.True(t, signatureIsValid(got, "whsec-SECRET-signing"), "the placeholder is the saved signing secret")
		assert.Equal(t, "SECRET-header", got.header.Get("X-Api-Key"))
		assert.Empty(t, elsewhere.received())
	})
}

// A send that fails prints the URL it went to, and for these channels the secret is in the URL.
// The test endpoint answers with that error, so it must not repeat what it took from the saved channel.
func TestNotificationChannelsAPI_TestFailureDoesNotEchoASavedSecret(t *testing.T) {
	a := newSecretsApp(t)

	// Every channel here sends to an address of the fixture that refuses connections. Slack's
	// fixture has a bot token, which sends to slack.com instead.
	sendsToItsOwnAddress := map[string]bool{
		"smtp": true, "telegram": true, "webhook": true, "ntfy": true, "pagerduty": true,
		"google_chat": true, "grafana_oncall": true, "gotify": true, "mattermost": true, "matrix": true,
		"discord": true, "wecom": true, "whatsapp": true, "pagertree": true, "teams": true,
	}
	for _, c := range secretChannels {
		if !sendsToItsOwnAddress[c.channelType] {
			continue
		}
		t.Run(c.channelType, func(t *testing.T) {
			id := a.createSecretChannel(t, c)

			w := a.testChannel(t, id, c.channelType, c.withPlaceholders(nil))

			assert.NotContains(t, w.Body.String(), secretMarker, "the response repeats a saved secret")
			if c.channelType == "telegram" || c.channelType == "discord" {
				// The bot token is in the path of the URL, and the webhook URL is the secret. The
				// request URL leaves the error whole, so neither is there to be masked.
				require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), "Failed to send test notification")
				assert.Contains(t, w.Body.String(), "[redacted URL]")
				assert.Contains(t, w.Body.String(), "connection refused", "the cause stays")
			}
		})
	}
}

// net/http prints the URL of a failed request with the password of its user info replaced, and
// the path as net/url writes it, so the secret is not in the error as it was entered
func TestNotificationChannelsAPI_TestFailureMasksASecretAsAURLPrintsIt(t *testing.T) {
	a := newSecretsApp(t)

	t.Run("a webhook URL with user info", func(t *testing.T) {
		discord := secretChannelOfType(t, "discord")
		discord.secrets = map[string]string{"webhook_url": "http://user:pw@127.0.0.1:1/api/webhooks/1/TOKENPATH"}
		id := a.createSecretChannel(t, discord)

		w := a.testChannel(t, id, "discord", discord.withPlaceholders(nil))

		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "connection refused", "the cause stays")
		assert.NotContains(t, w.Body.String(), "TOKENPATH")
		assert.NotContains(t, w.Body.String(), "user:")
	})

	t.Run("a bot token with a slash and a space", func(t *testing.T) {
		telegram := secretChannelOfType(t, "telegram")
		telegram.secrets = map[string]string{"bot_token": "123:ABC/def ghi"}
		id := a.createSecretChannel(t, telegram)

		w := a.testChannel(t, id, "telegram", telegram.withPlaceholders(nil))

		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "connection refused", "the cause stays")
		assert.NotContains(t, w.Body.String(), "123:ABC")
		assert.NotContains(t, w.Body.String(), "def")
		assert.NotContains(t, w.Body.String(), "ghi")
	})
}

// encoding/json fills a field from a key of any letter case, and the last one in the config wins.
// A config that names the endpoint twice must not get past the check that the endpoint is the
// saved one, and then send the saved token to the other one.
func TestNotificationChannelsAPI_ADuplicateKeyDoesNotMoveTheEndpoint(t *testing.T) {
	a := newSecretsApp(t)
	saved := startRequestReceiver(t)
	evil := startRequestReceiver(t)
	telegram := secretChannelOfType(t, "telegram").withVisible(map[string]any{"server_url": saved.url})
	id := a.createSecretChannel(t, telegram)

	// The saved endpoint under another letter case, after the evil one
	config := `{"chat_id":"-100123","server_url":"` + evil.url + `","SERVER_URL":"` + saved.url + `","bot_token":"` + secretPlaceholder + `"}`
	// The same, in the order the server writes keys back in
	reversed := `{"chat_id":"-100123","SERVER_URL":"` + saved.url + `","server_url":"` + evil.url + `","bot_token":"` + secretPlaceholder + `"}`

	for _, config := range []string{config, reversed} {
		for request, w := range map[string]*httptest.ResponseRecorder{
			"PUT":    a.put(t, id, "telegram", config),
			"PATCH":  a.patch(t, id, "", config),
			"Test":   a.testChannel(t, id, "telegram", config),
			"create": a.request(t, http.MethodPost, "/api/v1/notification-channels", with(telegram.body("Copy"), "config", config), a.asAdmin),
		} {
			require.Equal(t, http.StatusBadRequest, w.Code, request+": "+w.Body.String())
			assert.Contains(t, w.Body.String(), "Invalid config: duplicate key", request)
			assert.NotContains(t, w.Body.String(), secretMarker, request)
		}
	}

	assert.Empty(t, evil.received(), "the bot token went to the other server")
	assert.Empty(t, saved.received(), "nothing is sent")
	assert.JSONEq(t, telegram.config(nil), a.storedChannelConfig(t, id))
	list := decode[[]channelWithConfig](t, a.request(t, http.MethodGet, "/api/v1/notification-channels?limit=1000", nil, a.asAdmin), http.StatusOK)
	assert.Len(t, list, 1, "no channel was created")
}

func TestNotificationChannelsAPI_TestRefusesAPlaceholderWithNothingToResolveIt(t *testing.T) {
	a := newSecretsApp(t)
	// A channel of another type than the ones tested, so that it has nothing to resolve with
	otherID := a.createSecretChannel(t, secretChannelOfType(t, "signal"))

	for _, c := range secretChannels {
		if len(c.secrets) == 0 {
			continue
		}
		t.Run(c.channelType, func(t *testing.T) {
			requireRefusedAsPlaceholder(t, a.testChannel(t, "", c.channelType, c.withPlaceholders(nil)), "no id")
			requireRefusedAsPlaceholder(t, a.testChannel(t, otherID, c.channelType, c.withPlaceholders(nil)), "the id of a channel of another type")
			for _, key := range c.secretKeys() {
				requireRefusedAsPlaceholder(t, a.testChannel(t, "", c.channelType, c.config(map[string]string{key: secretPlaceholder})), "no id, only "+key)
			}
		})
	}
}

// A URL sent back as the API showed it is the stored URL: the test sends with its user info, which
// the receiver sees as the Authorization header, and to its path
func TestNotificationChannelsAPI_TestSendsToTheStoredURL(t *testing.T) {
	a := newSecretsApp(t)
	hook := startRequestReceiver(t)
	host := strings.TrimPrefix(hook.url, "http://")
	u := urlCredentialCase{"webhook", "webhook_url", "http://user:hunter2@" + host + "/hook?token=hunter2", "http://user:********@" + host + "/hook?token=********"}
	id := a.createSecretChannel(t, u.storedChannel(t))
	stored := a.storedChannelConfig(t, id)

	decode[any](t, a.testChannel(t, id, "webhook", u.shownChannel(t).withPlaceholders(nil)), http.StatusOK)

	got := hook.requireOne(t)
	assert.Equal(t, "/hook", got.path)
	user, password, ok := (&http.Request{Header: got.header}).BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "user", user)
	assert.Equal(t, "hunter2", password, "the stored password is sent")
	assert.True(t, signatureIsValid(got, "whsec-SECRET-signing"), "the other saved secrets are resolved too")

	t.Run("a masked URL that was moved is refused, and nothing is sent", func(t *testing.T) {
		moved := u.shownChannel(t).withVisible(map[string]any{"webhook_url": strings.Replace(u.shown, "/hook", "/other", 1)})
		w := a.testChannel(t, id, "webhook", moved.withPlaceholders(nil))
		requireRefusedAsPlaceholder(t, w, "moved")
		assert.Contains(t, w.Body.String(), "webhook_url")
		assert.Empty(t, hook.received(), "nothing is sent")
	})

	t.Run("without an id a masked URL is refused", func(t *testing.T) {
		w := a.testChannel(t, "", "webhook", u.shownChannel(t).withPlaceholders(nil))
		requireRefusedAsPlaceholder(t, w, "no id")
		assert.Empty(t, hook.received(), "nothing is sent")
	})

	assert.Equal(t, stored, a.storedChannelConfig(t, id), "testing a channel doesn't change it")
}

// A failed send prints the URL it went to. The test endpoint answers with that error, so it must
// not repeat the password or a query value of the stored URL.
func TestNotificationChannelsAPI_TestFailureDoesNotEchoTheCredentialsOfAStoredURL(t *testing.T) {
	a := newSecretsApp(t)

	for _, u := range urlCredentialCases {
		t.Run(u.channelType, func(t *testing.T) {
			id := a.createSecretChannel(t, u.storedChannel(t))

			w := a.testChannel(t, id, u.channelType, u.shownChannel(t).withPlaceholders(nil))

			require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "Failed to send test notification")
			assert.NotContains(t, w.Body.String(), "hunter2")
			assert.NotContains(t, w.Body.String(), "tok123")
			assert.NotContains(t, w.Body.String(), secretMarker)
		})
	}
}
