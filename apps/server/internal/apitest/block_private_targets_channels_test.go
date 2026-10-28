package apitest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-158: with BLOCK_PRIVATE_TARGETS on, saving or testing a notification channel whose URL or SMTP
// host is a literal blocked address is refused with 400 and TARGET_NOT_ALLOWED. A name is saved, and a
// channel that sends to one that resolves to a blocked address fails when it dials.

func channelOfType(name, channelType, channelConfig string) map[string]any {
	return map[string]any{"name": name, "type": channelType, "active": true, "config": channelConfig}
}

func webhookConfigFor(url string) string {
	return fmt.Sprintf(`{"webhook_url":%q,"webhook_content_type":"json"}`, url)
}

func smtpChannelConfig(host string, port int) string {
	return fmt.Sprintf(`{"smtp_host":%q,"smtp_port":%d,"username":"alerts@example.com","password":"smtp-pass","from":"alerts@example.com","to":"oncall@example.com"}`, host, port)
}

// hookReceiver is a webhook endpoint on this machine that counts what it is sent
func hookReceiver(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestBlockPrivateTargets_ChannelsAtABlockedAddressAreRefusedOnCreateUpdateAndPatch(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	tests := []struct {
		name string
		body map[string]any
		host string
	}{
		{"webhook on loopback", channelOfType("Local", "webhook", webhookConfigFor("http://127.0.0.1/x")), "127.0.0.1"},
		{"webhook on the metadata address", channelOfType("Metadata", "webhook", webhookConfigFor("http://169.254.169.254/latest/meta-data")), "169.254.169.254"},
		{"ntfy on a private address", channelOfType("Ntfy", "ntfy", `{"server_url":"http://10.0.0.5:8080","topic":"t","authentication_type":"none","priority":3}`), "10.0.0.5"},
		{"smtp on a private address", channelOfType("Mail", "smtp", smtpChannelConfig("10.0.0.1", 25)), "10.0.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name+" on create", func(t *testing.T) {
			assertTargetNotAllowed(t, a.request(t, http.MethodPost, channelsPath, tc.body, a.asAdmin), tc.host)
		})
	}

	id := decode[created](t, a.request(t, http.MethodPost, channelsPath, webhookChannelBody("Hooks"), a.asAdmin), http.StatusCreated).ID
	path := channelsPath + "/" + id
	for _, tc := range tests {
		t.Run(tc.name+" on update", func(t *testing.T) {
			assertTargetNotAllowed(t, a.request(t, http.MethodPut, path, tc.body, a.asAdmin), tc.host)
		})
		t.Run(tc.name+" on patch", func(t *testing.T) {
			assertTargetNotAllowed(t, a.request(t, http.MethodPatch, path, map[string]any{"type": tc.body["type"], "config": tc.body["config"]}, a.asAdmin), tc.host)
		})
	}
	// the channel is as it was, and an edit that leaves where it sends alone saves
	decode[created](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Hooks v2"}, a.asAdmin), http.StatusOK)
}

func TestBlockPrivateTargets_AChannelAtANameIsSaved(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))

	for _, body := range []map[string]any{
		channelOfType("Corp", "webhook", webhookConfigFor("https://hooks.corp.example/x")),
		channelOfType("Localhost", "webhook", webhookConfigFor("http://localhost:9/x")),
		channelOfType("Public", "webhook", webhookConfigFor("https://8.8.8.8/x")),
		channelOfType("Mail", "smtp", smtpChannelConfig("smtp.corp.example", 587)),
	} {
		decode[created](t, a.request(t, http.MethodPost, channelsPath, body, a.asAdmin), http.StatusCreated)
	}
}

func TestBlockPrivateTargets_TheTestEndpointRefusesABlockedTargetAndSendsNothing(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	hook, requests := hookReceiver(t)

	// a literal address is refused as a save is
	assertTargetNotAllowed(t, a.request(t, http.MethodPost, channelTestPath, channelOfType("Local", "webhook", webhookConfigFor(hook.URL)), a.asAdmin), "127.0.0.1")

	// a name saves and is refused when the test dials it
	u, err := url.Parse(hook.URL)
	require.NoError(t, err)
	byName := "http://localhost:" + u.Port() + "/x"
	w := a.request(t, http.MethodPost, channelTestPath, channelOfType("Local", "webhook", webhookConfigFor(byName)), a.asAdmin)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "target address is not allowed")
	assert.NotContains(t, w.Body.String(), "[redacted URL]/x?", "the URL stays out of the answer")

	mail := smtptest.Start(t)
	assertTargetNotAllowed(t, a.request(t, http.MethodPost, channelTestPath, channelOfType("Mail", "smtp", smtpChannelConfig(mail.Host(), mail.Port())), a.asAdmin), "127.0.0.1")

	assert.Zero(t, requests.Load(), "the webhook saw no request")
	assert.Empty(t, mail.Messages(), "the mail server took no message")
}

func TestBlockPrivateTargets_TheTestEndpointRefusesAnSMTPServerThatIsANameForALoopbackAddress(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(true))
	mail := smtptest.Start(t)

	w := a.request(t, http.MethodPost, channelTestPath, channelOfType("Mail", "smtp", smtpChannelConfig("localhost", mail.Port())), a.asAdmin)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "target address is not allowed")
	assert.Empty(t, mail.Messages())
}

func TestBlockPrivateTargets_OffChannelsAtPrivateAddressesSaveAndSend(t *testing.T) {
	a := newAppWithAdmin(t, withBlockPrivateTargets(false))
	hook, requests := hookReceiver(t)
	mail := smtptest.Start(t)

	webhook := channelOfType("Local", "webhook", webhookConfigFor(hook.URL))
	smtp := channelOfType("Mail", "smtp", smtpChannelConfig(mail.Host(), mail.Port()))
	for _, body := range []map[string]any{webhook, smtp, channelOfType("Metadata", "webhook", webhookConfigFor("http://169.254.169.254/x"))} {
		id := decode[created](t, a.request(t, http.MethodPost, channelsPath, body, a.asAdmin), http.StatusCreated).ID
		decode[created](t, a.request(t, http.MethodPut, channelsPath+"/"+id, body, a.asAdmin), http.StatusOK)
		decode[created](t, a.request(t, http.MethodPatch, channelsPath+"/"+id, map[string]any{"config": body["config"]}, a.asAdmin), http.StatusOK)
	}

	decode[any](t, a.request(t, http.MethodPost, channelTestPath, webhook, a.asAdmin), http.StatusOK)
	decode[any](t, a.request(t, http.MethodPost, channelTestPath, smtp, a.asAdmin), http.StatusOK)

	assert.Equal(t, int32(1), requests.Load())
	assert.Len(t, mail.WaitFor(t, 1), 1)
}

// The install email channel sends through the install's own account-email sender, which the guard
// does not touch even when that sender is on this machine
func TestBlockPrivateTargets_TheInstallEmailChannelStillSendsThroughTheAccountSender(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, withAccountSMTP(mail), withSignupMode(config.SignupModeInvite), withBlockPrivateTargets(true))
	mail.WaitFor(t, 1)
	installEmailTeam(t, a)

	w := a.request(t, http.MethodPost, channelTestPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Len(t, mailTo(mail, installEmailMember), 1)
}
