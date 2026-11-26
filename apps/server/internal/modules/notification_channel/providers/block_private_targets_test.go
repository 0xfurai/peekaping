package providers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/netguard"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-158: with BLOCK_PRIVATE_TARGETS on, the providers dial through the guard, so a channel at a blocked
// address, at a name that resolves to one or behind a redirect to one sends nothing.

// countingServer is an HTTP server on loopback that counts the requests it gets
func countingServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if handler != nil {
			handler(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return string(out)
}

// urlProvider sends a notification to the URL it is given, with the client the test chooses
type urlProvider struct {
	name string
	send func(client *http.Client, url string) error
}

func urlProviders(t *testing.T) []urlProvider {
	send := func(sender interface {
		Send(context.Context, string, string, *monitor.Model, *heartbeat.Model) error
	}, config string) error {
		return sender.Send(context.Background(), config, "Monitor is down", testMonitor(), testHeartbeat())
	}
	return []urlProvider{
		{"webhook", func(c *http.Client, url string) error {
			return send(NewWebhookSender(nopLogger(), c), marshal(t, WebhookConfig{WebhookURL: url, WebhookContentType: "json"}))
		}},
		{"webhook with a signing secret", func(c *http.Client, url string) error {
			return send(NewWebhookSender(nopLogger(), c), marshal(t, WebhookConfig{WebhookURL: url, WebhookContentType: "json", WebhookSigningSecret: "s3cret"}))
		}},
		{"ntfy", func(c *http.Client, url string) error {
			return send(NewNTFYSender(nopLogger(), c), marshal(t, NTFYConfig{ServerUrl: url, Topic: "alerts", AuthenticationType: "none", Priority: 3}))
		}},
		{"discord", func(c *http.Client, url string) error {
			return send(NewDiscordSender(nopLogger(), c), marshal(t, DiscordConfig{WebhookURL: url}))
		}},
	}
}

func TestProviders_AGuardedClientSendsNothingToABlockedAddress(t *testing.T) {
	for _, provider := range urlProviders(t) {
		for _, host := range []string{"127.0.0.1", "localhost"} {
			t.Run(provider.name+" at "+host, func(t *testing.T) {
				server, requests := countingServer(t, nil)
				_, port, err := net.SplitHostPort(server.Listener.Addr().String())
				require.NoError(t, err)
				client := NewGuardedHTTPClient(netguard.New(true))

				err = provider.send(client, "http://"+net.JoinHostPort(host, port)+"/hook")

				require.Error(t, err)
				assert.ErrorIs(t, err, netguard.ErrBlocked)
				assert.Zero(t, requests.Load(), "the server saw no request")
			})
		}
	}
}

func TestProviders_AGuardedClientSendsNothingToTheMetadataAddress(t *testing.T) {
	for _, provider := range urlProviders(t) {
		t.Run(provider.name, func(t *testing.T) {
			err := provider.send(NewGuardedHTTPClient(netguard.New(true)), "http://169.254.169.254/latest/meta-data/")

			require.Error(t, err)
			assert.ErrorIs(t, err, netguard.ErrBlocked)
		})
	}
}

func TestProviders_AGuardedClientRefusesARedirectToABlockedAddress(t *testing.T) {
	// Only 127.0.0.2 is blocked, so that the test can serve on 127.0.0.1 and redirect to it
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
	for _, provider := range urlProviders(t) {
		t.Run(provider.name, func(t *testing.T) {
			// Where the redirect leads is listened on when the machine has the address; on one that
			// doesn't (macOS only has 127.0.0.1 up), the dial would be refused either way
			var blockedRequests atomic.Int32
			target := "127.0.0.2:1"
			if listener, err := net.Listen("tcp", "127.0.0.2:0"); err == nil {
				blocked := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					blockedRequests.Add(1)
				})}}
				blocked.Start()
				t.Cleanup(blocked.Close)
				target = listener.Addr().String()
			}
			allowed, allowedRequests := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://"+target+"/", http.StatusTemporaryRedirect)
			})

			err := provider.send(NewGuardedHTTPClient(guard), allowed.URL+"/hook")

			require.Error(t, err)
			assert.Equal(t, int32(1), allowedRequests.Load(), "the first address was reached")
			assert.Zero(t, blockedRequests.Load(), "the address the redirect leads to saw no request")
			// Webhooks with a signing secret don't follow a redirect, so they fail on the status
			if provider.name != "webhook with a signing secret" {
				assert.ErrorIs(t, err, netguard.ErrBlocked)
			}
		})
	}
}

func TestProviders_AGuardThatIsOffChangesNothing(t *testing.T) {
	for _, provider := range urlProviders(t) {
		for name, client := range map[string]*http.Client{
			"a guard that is off": NewGuardedHTTPClient(netguard.New(false)),
			"no guard":            NewHTTPClient(),
			"a nil guard":         NewGuardedHTTPClient(nil),
		} {
			t.Run(provider.name+" with "+name, func(t *testing.T) {
				server, requests := countingServer(t, nil)

				require.NoError(t, provider.send(client, server.URL+"/hook"))

				assert.Equal(t, int32(1), requests.Load())
			})
		}
	}
}

func smtpConfigAt(t *testing.T, server *smtptest.Server, host string) string {
	t.Helper()
	return marshal(t, EmailConfig{SMTPHost: host, SMTPPort: server.Port(), SMTPUsername: "alerts@example.com", SMTPPassword: "smtp-pass",
		SMTPFrom: "alerts@example.com", SMTPTo: "oncall@example.com"})
}

func TestEmailSender_AGuardedSenderSendsNothingToABlockedServer(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			server := smtptest.Start(t)
			sender := NewGuardedEmailSender(nopLogger(), netguard.New(true))

			err := sender.Send(context.Background(), smtpConfigAt(t, server, host), "Monitor is down", testMonitor(), testHeartbeat())

			require.Error(t, err)
			assert.ErrorIs(t, err, netguard.ErrBlocked)
			assert.Empty(t, server.Messages(), "the server took no message")
		})
	}
}

func TestEmailSender_WithTheGuardOffItSends(t *testing.T) {
	for name, sender := range map[string]*EmailSender{
		"a guard that is off": NewGuardedEmailSender(nopLogger(), netguard.New(false)),
		"no guard":            NewEmailSender(nopLogger()),
	} {
		t.Run(name, func(t *testing.T) {
			server := smtptest.Start(t)

			require.NoError(t, sender.Send(context.Background(), smtpConfigAt(t, server, "127.0.0.1"), "Monitor is down", testMonitor(), testHeartbeat()))

			assert.Len(t, server.WaitFor(t, 1), 1)
		})
	}
}
