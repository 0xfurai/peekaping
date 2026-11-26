package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// captureWebhook starts a test server that records the first request's body and headers,
// then runs Send against it with cfg. cfg.WebhookURL is set to the test server's URL.
func captureWebhook(t *testing.T, cfg WebhookConfig) (body []byte, headers http.Header) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg.WebhookURL = srv.URL
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	sender := NewWebhookSender(zap.NewNop().Sugar(), NewHTTPClient())
	err = sender.Send(
		context.Background(),
		string(cfgJSON),
		"test message",
		&monitor.Model{ID: "m1", Name: "Test Monitor"},
		&heartbeat.Model{Msg: "down"},
	)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	return body, headers
}

// rawBody returns the request body as sent.
func rawBody(_ *testing.T, _ http.Header, body []byte) []byte {
	return body
}

// formDataField returns the "data" field of a multipart/form-data request,
// the way a receiver that parses the form (PHP's $_POST['data']) sees it.
func formDataField(t *testing.T, headers http.Header, body []byte) []byte {
	t.Helper()

	_, params, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		t.Fatalf("invalid Content-Type %q: %v", headers.Get("Content-Type"), err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("failed to parse multipart body: %v", err)
	}
	if len(form.Value["data"]) != 1 {
		t.Fatalf("expected one data field, got %d", len(form.Value["data"]))
	}
	return []byte(form.Value["data"][0])
}

// Pins the signature format against a value computed outside Go:
// printf '1700000000.hello' | openssl dgst -sha256 -hmac key
func TestSignWebhookPayload_KnownValue(t *testing.T) {
	got := signWebhookPayload("1700000000", []byte("hello"), "key")
	want := "sha256=4d583a269f4f276a3fa80ff31b5a01879a848096983222a17893d198418939aa"
	if got != want {
		t.Errorf("signWebhookPayload = %q, want %q", got, want)
	}
}

// The signature covers the body for JSON and custom bodies, and the data field
// for form-data, since multipart parsers don't expose the raw body.
func TestWebhookSend_SignsPayloadForEachContentType(t *testing.T) {
	cases := []struct {
		name          string
		cfg           WebhookConfig
		signedPayload func(t *testing.T, headers http.Header, body []byte) []byte
	}{
		{"json", WebhookConfig{WebhookContentType: "json"}, rawBody},
		{"form-data", WebhookConfig{WebhookContentType: "form-data"}, formDataField},
		{"custom", WebhookConfig{
			WebhookContentType: "custom",
			WebhookCustomBody:  "monitor {{ monitor.name }} is {{ heartbeat.msg }}",
		}, rawBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.WebhookSigningSecret = "shh"
			body, headers := captureWebhook(t, tc.cfg)

			got := headers.Get(webhookSignatureHeader)
			want := signWebhookPayload(headers.Get(webhookTimestampHeader), tc.signedPayload(t, headers, body), "shh")
			if got != want {
				t.Errorf("signature = %q, want %q", got, want)
			}
		})
	}
}

func TestWebhookSend_SendsRecentTimestamp(t *testing.T) {
	before := time.Now().Unix()
	_, headers := captureWebhook(t, WebhookConfig{WebhookContentType: "json", WebhookSigningSecret: "shh"})
	after := time.Now().Unix()

	raw := headers.Get(webhookTimestampHeader)
	if raw == "" {
		t.Fatalf("expected %q header when signing, got none", webhookTimestampHeader)
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("timestamp %q is not a unix integer: %v", raw, err)
	}
	if ts < before || ts > after {
		t.Errorf("timestamp %d outside send window [%d, %d]", ts, before, after)
	}
}

func TestWebhookSend_NoSecretNoSignatureOrTimestamp(t *testing.T) {
	_, headers := captureWebhook(t, WebhookConfig{WebhookContentType: "json"})

	if got := headers.Get(webhookSignatureHeader); got != "" {
		t.Errorf("expected no signature header without a secret, got %q", got)
	}
	if got := headers.Get(webhookTimestampHeader); got != "" {
		t.Errorf("expected no timestamp header without a secret, got %q", got)
	}
}

// A signed request isn't sent on to a redirect target, which could replay it.
// Unsigned requests still follow redirects.
func TestWebhookSend_SignedRequestsDontFollowRedirects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		secret   string
		redirect int
		wantSent bool
	}{
		{"signed 307", "shh", http.StatusTemporaryRedirect, false},
		{"signed 308", "shh", http.StatusPermanentRedirect, false},
		{"unsigned 307", "", http.StatusTemporaryRedirect, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var targetHits int
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetHits++
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, tc.redirect)
			}))
			defer origin.Close()

			cfgJSON, err := json.Marshal(WebhookConfig{
				WebhookURL:           origin.URL,
				WebhookContentType:   "json",
				WebhookSigningSecret: tc.secret,
			})
			if err != nil {
				t.Fatalf("failed to marshal config: %v", err)
			}
			err = NewWebhookSender(zap.NewNop().Sugar(), NewHTTPClient()).Send(
				context.Background(),
				string(cfgJSON),
				"test message",
				&monitor.Model{ID: "m1", Name: "Test Monitor"},
				&heartbeat.Model{Msg: "down"},
			)

			if tc.wantSent {
				if err != nil || targetHits != 1 {
					t.Errorf("Send = %v with %d target hits, want success and 1 hit", err, targetHits)
				}
				return
			}
			if err == nil {
				t.Errorf("Send succeeded, want an error for the %d redirect", tc.redirect)
			}
			if targetHits != 0 {
				t.Errorf("redirect target got %d signed requests, want 0", targetHits)
			}
		})
	}
}

// Additional headers are applied before signing, so they can't replace the
// signature or the timestamp it covers.
func TestWebhookSend_AdditionalHeadersCannotReplaceSignature(t *testing.T) {
	body, headers := captureWebhook(t, WebhookConfig{
		WebhookContentType:       "json",
		WebhookSigningSecret:     "shh",
		WebhookAdditionalHeaders: `{"X-Webhook-Signature": "sha256=forged", "x-webhook-timestamp": "1"}`,
	})

	ts := headers.Get(webhookTimestampHeader)
	if ts == "1" {
		t.Errorf("additional headers replaced %s", webhookTimestampHeader)
	}
	got := headers.Get(webhookSignatureHeader)
	want := signWebhookPayload(ts, body, "shh")
	if got != want {
		t.Errorf("signature = %q, want %q", got, want)
	}
}

// The additional headers usually carry an Authorization token, so neither the log nor the
// error of a failed send may repeat them
func TestWebhookSend_AdditionalHeadersAreNotLoggedOrEchoed(t *testing.T) {
	const token = "Bearer SECRET-header-token"
	send := func(t *testing.T, headers string) (string, error) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		cfgJSON, err := json.Marshal(WebhookConfig{
			WebhookURL:               srv.URL,
			WebhookContentType:       "json",
			WebhookAdditionalHeaders: headers,
		})
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}

		core, logs := observer.New(zap.DebugLevel)
		sender := NewWebhookSender(zap.New(core).Sugar(), NewHTTPClient())
		sendErr := sender.Send(context.Background(), string(cfgJSON), "test message", &monitor.Model{ID: "m1", Name: "Test Monitor"}, &heartbeat.Model{Msg: "down"})

		var logged strings.Builder
		for _, entry := range logs.All() {
			logged.WriteString(entry.Message + fmt.Sprint(entry.ContextMap()) + "\n")
		}
		return logged.String(), sendErr
	}

	t.Run("headers that are not JSON", func(t *testing.T) {
		// The closing brace is missing
		logged, err := send(t, `{"Authorization": "`+token+`"`)

		if err == nil {
			t.Fatal("Send accepted headers that are not JSON")
		}
		if !strings.Contains(err.Error(), "additional Headers is not a valid JSON") {
			t.Errorf("error = %q, want it to say the headers are not valid JSON", err)
		}
		for _, leaked := range []string{"SECRET-header-token", "Authorization", "Bearer"} {
			if strings.Contains(err.Error(), leaked) {
				t.Errorf("error %q repeats %q", err, leaked)
			}
			if strings.Contains(logged, leaked) {
				t.Errorf("log repeats %q: %s", leaked, logged)
			}
		}
	})

	t.Run("headers that are JSON but not an object", func(t *testing.T) {
		logged, err := send(t, `["`+token+`"]`)

		if err == nil {
			t.Fatal("Send accepted headers that are not an object")
		}
		for _, leaked := range []string{"SECRET-header-token", "Bearer"} {
			if strings.Contains(err.Error(), leaked) || strings.Contains(logged, leaked) {
				t.Errorf("error %q or log %q repeats %q", err, logged, leaked)
			}
		}
	})

	t.Run("headers that are applied", func(t *testing.T) {
		logged, err := send(t, `{"Authorization": "`+token+`"}`)

		if err != nil {
			t.Fatalf("Send returned error: %v", err)
		}
		if strings.Contains(logged, "SECRET-header-token") {
			t.Errorf("log repeats the header: %s", logged)
		}
	})
}
