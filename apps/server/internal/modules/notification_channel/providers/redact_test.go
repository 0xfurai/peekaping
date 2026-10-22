package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactURLErrors(t *testing.T) {
	const secretURL = "https://hooks.example.com/services/T000/B000/secret-token"
	refused := errors.New("dial tcp 10.0.0.1:443: connect: connection refused")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a wrapped url.Error loses its URL",
			err:  fmt.Errorf("failed to send webhook: %w", &url.Error{Op: "Post", URL: secretURL, Err: refused}),
			want: "failed to send webhook: Post [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "a bare url.Error loses its URL",
			err:  &url.Error{Op: "Get", URL: secretURL, Err: refused},
			want: "Get [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "a url.Error behind several wrappers loses its URL",
			err:  fmt.Errorf("send: %w", fmt.Errorf("telegram: %w", &url.Error{Op: "Get", URL: secretURL, Err: refused})),
			want: "send: telegram: Get [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "a URL repeated by a wrapper is removed too",
			err:  fmt.Errorf("sending to %s: %w", secretURL, &url.Error{Op: "Post", URL: secretURL, Err: refused}),
			want: "sending to [redacted URL]: Post [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "a url.Error inside another one loses both URLs",
			err: &url.Error{Op: "Get", URL: "https://first.example.com/secret-one", Err: fmt.Errorf("redirect: %w",
				&url.Error{Op: "Get", URL: "https://second.example.com/secret-two", Err: refused})},
			want: "Get [redacted URL]: redirect: Get [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "a url.Error in a joined error loses its URL",
			err:  errors.Join(errors.New("first failure"), &url.Error{Op: "Post", URL: secretURL, Err: refused}),
			want: "first failure\nPost [redacted URL]: dial tcp 10.0.0.1:443: connect: connection refused",
		},
		{
			name: "an error without a URL is returned as it is",
			err:  fmt.Errorf("telegram API returned status: 400 Bad Request: %w", errors.New("chat not found")),
			want: "telegram API returned status: 400 Bad Request: chat not found",
		},
		{
			name: "a url.Error without a URL is returned as it is",
			err:  &url.Error{Op: "Get", URL: "", Err: refused},
			want: `Get "": dial tcp 10.0.0.1:443: connect: connection refused`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactURLErrors(tc.err)

			require.Error(t, got)
			assert.Equal(t, tc.want, got.Error())
		})
	}
}

func TestRedactURLErrors_NilStaysNil(t *testing.T) {
	assert.NoError(t, RedactURLErrors(nil))
}

func TestRedactURLErrors_ReturnsAnErrorWithoutAURLAsItIs(t *testing.T) {
	err := errors.New("telegram API returned status: 400 Bad Request")

	assert.Same(t, err, RedactURLErrors(err))
}

func TestRedactURLErrors_KeepsTheCauseForErrorsIs(t *testing.T) {
	err := fmt.Errorf("failed to send: %w", &url.Error{Op: "Post", URL: "https://hooks.example.com/secret", Err: context.DeadlineExceeded})

	got := RedactURLErrors(err)

	assert.ErrorIs(t, got, context.DeadlineExceeded, "a timeout must stay recognizable after its URL is removed")
	assert.NotContains(t, got.Error(), "hooks.example.com")
}

func TestRedactURLErrors_RemovesTheURLFromErrorsOfTheRealHTTPClient(t *testing.T) {
	// A closed server refuses the connection, so Do fails with a *url.Error
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	client := NewHTTPClient()
	secretURL := closed.URL + "/bot123456:secret-bot-token/sendMessage?chat_id=42"

	req, err := http.NewRequest(http.MethodGet, secretURL, nil)
	require.NoError(t, err)
	_, doErr := client.Do(req)
	require.Error(t, doErr)
	require.Contains(t, doErr.Error(), "secret-bot-token", "the client's own error carries the URL; that's what the helper removes")

	got := RedactURLErrors(fmt.Errorf("failed to send telegram message: %w", doErr))

	assert.NotContains(t, got.Error(), "secret-bot-token")
	assert.NotContains(t, got.Error(), "sendMessage")
	assert.Contains(t, got.Error(), "failed to send telegram message: Get [redacted URL]: dial tcp")
	assert.Contains(t, got.Error(), "connection refused")
}

func TestRedactURLErrors_RemovesTheURLFromAnErrorOfAnUnparsableURL(t *testing.T) {
	// A control character is shown escaped in the error, so the raw URL isn't in its text
	_, err := http.NewRequest(http.MethodGet, "https://hooks.example.com/secret-token\x7f", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret-token")

	got := RedactURLErrors(fmt.Errorf("failed to create request: %w", err))

	assert.NotContains(t, got.Error(), "secret-token")
	assert.Contains(t, got.Error(), "failed to create request: parse [redacted URL]: net/url: invalid control character in URL")
}
