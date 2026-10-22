package apitest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
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
