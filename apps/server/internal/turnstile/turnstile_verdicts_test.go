package turnstile_test

import (
	"context"
	"net/http"
	"testing"

	"peekaping/internal/turnstile"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify_ReadsTheVerdictFromTheStatusAndTheBody(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantIs  error
		wantMsg string // the whole message; nil wantIs means the answer is accepted
	}{
		{"a server error beats a good verdict", http.StatusInternalServerError, `{"success":true}`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 500"},
		{"a server error beats a body that is not JSON", http.StatusBadGateway, `<html>`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 502"},
		{"a body that is not JSON", http.StatusOK, `<html>oops</html>`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 200 with a body that is not a verdict"},
		{"a client error with a body that is not JSON", http.StatusBadRequest, `nope`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 400 with a body that is not a verdict"},
		{"a refusal gives its codes", http.StatusOK, `{"success":false,"error-codes":["timeout-or-duplicate","invalid-input-response"]}`, turnstile.ErrRejected, "the Turnstile answer was not accepted: timeout-or-duplicate, invalid-input-response"},
		{"a refusal without codes", http.StatusOK, `{"success":false}`, turnstile.ErrRejected, "the Turnstile answer was not accepted: "},
		{"a refusal with a client error", http.StatusBadRequest, `{"success":false,"error-codes":["invalid-input-secret"]}`, turnstile.ErrRejected, "the Turnstile answer was not accepted: invalid-input-secret"},
		{"a good verdict with a status that is not 200", http.StatusAccepted, `{"success":true}`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 202"},
		{"a good verdict with a client error", http.StatusForbidden, `{"success":true}`, turnstile.ErrUnavailable, "the Turnstile answer could not be checked: Cloudflare answered 403"},
		{"a good verdict", http.StatusOK, `{"success":true}`, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newSiteverify(t)
			server.status, server.body = tc.status, tc.body

			err := turnstile.New("s3cret", server.URL).Verify(context.Background(), "good", "203.0.113.9")

			if tc.wantIs == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.wantIs)
			assert.EqualError(t, err, tc.wantMsg)
		})
	}
}

func TestVerify_LeavesTheClientOutWhenItIsUnknown(t *testing.T) {
	server := newSiteverify(t)

	err := turnstile.New("s3cret", server.URL).Verify(context.Background(), "good", "")

	require.NoError(t, err)
	require.Len(t, server.requests, 1)
	assert.Equal(t, map[string]string{"secret": "s3cret", "response": "good", "remoteip": ""}, server.forms[0])
	assert.False(t, server.requests[0].PostForm.Has("remoteip"), "the field is not sent at all")
}

func TestVerify_AnAnswerOfTheLongestSizeIsAskedAbout(t *testing.T) {
	server := newSiteverify(t)
	v := turnstile.New("s3cret", server.URL)

	err := v.Verify(context.Background(), string(make([]byte, 2048)), "203.0.113.9")

	assert.ErrorIs(t, err, turnstile.ErrRejected, "Cloudflare refuses it, but it is asked")
	assert.Len(t, server.forms, 1)
	assert.EqualError(t, v.Verify(context.Background(), "", ""), "the Turnstile answer was not accepted: no answer was sent")
	assert.EqualError(t, v.Verify(context.Background(), string(make([]byte, 2049)), ""), "the Turnstile answer was not accepted: the answer is longer than 2048 characters")
}

func TestVerify_AnAddressThatCanNotBeRequestedIsUnavailableAndNamesNoSecret(t *testing.T) {
	err := turnstile.New("s3cret", "http://bad host/").Verify(context.Background(), "good", "")

	assert.ErrorIs(t, err, turnstile.ErrUnavailable)
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestVerify_ATransportErrorLeavesOutTheAddress(t *testing.T) {
	server := newSiteverify(t)
	url := server.URL
	server.Close()

	err := turnstile.New("s3cret", url).Verify(context.Background(), "good", "")

	assert.ErrorIs(t, err, turnstile.ErrUnavailable)
	assert.NotContains(t, err.Error(), url)
	assert.NotContains(t, err.Error(), "s3cret")
}
