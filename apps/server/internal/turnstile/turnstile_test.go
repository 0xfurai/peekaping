package turnstile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/turnstile"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSiteverify answers like Cloudflare's siteverify: it accepts the token "good" for the secret
// "s3cret", and records what it was asked
type fakeSiteverify struct {
	*httptest.Server
	mu       sync.Mutex
	requests []http.Request
	forms    []map[string]string
	status   int
	body     string
}

func newSiteverify(t *testing.T) *fakeSiteverify {
	t.Helper()
	f := &fakeSiteverify{status: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		form := map[string]string{"secret": r.PostForm.Get("secret"), "response": r.PostForm.Get("response"), "remoteip": r.PostForm.Get("remoteip")}
		f.mu.Lock()
		f.requests = append(f.requests, *r)
		f.forms = append(f.forms, form)
		status, body := f.status, f.body
		f.mu.Unlock()
		if body != "" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if form["secret"] == "s3cret" && form["response"] == "good" {
			_, _ = w.Write([]byte(`{"success":true,"challenge_ts":"2026-10-28T10:00:00Z","hostname":"peekaping.com","error-codes":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func TestVerify_AGoodAnswerPassesAndTheServerIsAskedWithTheSecretTheAnswerAndTheClient(t *testing.T) {
	server := newSiteverify(t)
	v := turnstile.New("s3cret", server.URL)

	err := v.Verify(context.Background(), "good", "203.0.113.9")

	require.NoError(t, err)
	require.Len(t, server.forms, 1)
	assert.Equal(t, map[string]string{"secret": "s3cret", "response": "good", "remoteip": "203.0.113.9"}, server.forms[0])
	assert.Equal(t, http.MethodPost, server.requests[0].Method)
	assert.Equal(t, "application/x-www-form-urlencoded", server.requests[0].Header.Get("Content-Type"))
}

func TestVerify_ABadAnswerIsRejected(t *testing.T) {
	server := newSiteverify(t)
	v := turnstile.New("s3cret", server.URL)

	err := v.Verify(context.Background(), "forged", "203.0.113.9")

	assert.ErrorIs(t, err, turnstile.ErrRejected)
	assert.ErrorContains(t, err, "invalid-input-response", "the reason Cloudflare gave is kept for the log")
}

func TestVerify_AnEmptyOrOversizedAnswerIsRejectedWithoutAskingCloudflare(t *testing.T) {
	server := newSiteverify(t)
	v := turnstile.New("s3cret", server.URL)

	assert.ErrorIs(t, v.Verify(context.Background(), "", "203.0.113.9"), turnstile.ErrRejected)
	assert.ErrorIs(t, v.Verify(context.Background(), string(make([]byte, 2049)), "203.0.113.9"), turnstile.ErrRejected)

	assert.Empty(t, server.forms)
}

func TestVerify_WhenCloudflareCanNotBeTrustedTheAnswerFailsClosed(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantRejected bool
	}{
		{name: "an error status", status: http.StatusInternalServerError, body: `{"success":true}`},
		{name: "a body that is not JSON", status: http.StatusOK, body: `<html>oops</html>`},
		{name: "a rejection with a 400, as Cloudflare answers a bad secret", status: http.StatusBadRequest, body: `{"success":false,"error-codes":["invalid-input-secret"]}`, wantRejected: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newSiteverify(t)
			server.status, server.body = tc.status, tc.body
			v := turnstile.New("s3cret", server.URL)

			err := v.Verify(context.Background(), "good", "203.0.113.9")

			require.Error(t, err)
			if tc.wantRejected {
				assert.ErrorIs(t, err, turnstile.ErrRejected)
			} else {
				assert.ErrorIs(t, err, turnstile.ErrUnavailable)
			}
		})
	}
}

func TestVerify_ACloudflareThatCanNotBeReachedIsUnavailable(t *testing.T) {
	server := newSiteverify(t)
	url := server.URL
	server.Close()
	v := turnstile.New("s3cret", url)

	err := v.Verify(context.Background(), "good", "203.0.113.9")

	assert.ErrorIs(t, err, turnstile.ErrUnavailable)
}

func TestVerify_ACloudflareThatDoesNotAnswerInTimeIsUnavailable(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	v := turnstile.New("s3cret", slow.URL).WithTimeout(50 * time.Millisecond)

	start := time.Now()
	err := v.Verify(context.Background(), "good", "203.0.113.9")

	assert.ErrorIs(t, err, turnstile.ErrUnavailable)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestNew_TheTimeoutIsFiveSecondsAndTheAddressIsCloudflaresByDefault(t *testing.T) {
	assert.Equal(t, 5*time.Second, turnstile.Timeout)
	assert.Equal(t, "https://challenges.cloudflare.com/turnstile/v0/siteverify", turnstile.DefaultVerifyURL)
}
