// Package turnstile checks the answer of a Cloudflare Turnstile widget on the server. Open signup
// asks for one when SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET are set (PP-155).
//
// The check fails closed: an answer Cloudflare did not accept, and an answer Cloudflare could not be
// asked about, are both refused. The caller tells them apart only to log which it was.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultVerifyURL is Cloudflare's siteverify endpoint
	DefaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	// Timeout is how long a check may take, the request and the answer together
	Timeout = 5 * time.Second
	// maxTokenLength is the longest answer Cloudflare accepts
	maxTokenLength = 2048
	// maxResponseBytes bounds what a reply may make the server read
	maxResponseBytes = 1 << 20
)

var (
	// ErrRejected means Cloudflare did not accept the answer (or it was missing or too long to be one)
	ErrRejected = errors.New("the Turnstile answer was not accepted")
	// ErrUnavailable means the answer could not be checked: Cloudflare could not be reached, did not
	// answer in time, or answered something that is not a verdict
	ErrUnavailable = errors.New("the Turnstile answer could not be checked")
)

// Verifier asks Cloudflare whether an answer is good
type Verifier struct {
	secret  string
	url     string
	timeout time.Duration
	client  *http.Client
}

// New returns a verifier for the secret. verifyURL is where the check is sent, Cloudflare's by
// default; tests point it at a fake server.
func New(secret, verifyURL string) *Verifier {
	if verifyURL == "" {
		verifyURL = DefaultVerifyURL
	}
	return &Verifier{secret: secret, url: verifyURL, timeout: Timeout, client: &http.Client{}}
}

// WithTimeout replaces the 5-second timeout, for tests
func (v *Verifier) WithTimeout(d time.Duration) *Verifier {
	v.timeout = d
	return v
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify returns nil when Cloudflare accepts the answer for this secret. remoteIP is the visitor's
// address, which Cloudflare uses as one more signal. The error wraps ErrRejected or ErrUnavailable
// and says why, for the log; it never holds the secret.
func (v *Verifier) Verify(ctx context.Context, token, remoteIP string) error {
	if token == "" {
		return fmt.Errorf("%w: no answer was sent", ErrRejected)
	}
	if len(token) > maxTokenLength {
		return fmt.Errorf("%w: the answer is longer than %d characters", ErrRejected, maxTokenLength)
	}
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	status, body, err := v.ask(ctx, token, remoteIP)
	if err != nil {
		return err
	}
	return judge(status, body)
}

// ask sends the answer to Cloudflare and returns the status and the body of the reply. The error
// wraps ErrUnavailable.
func (v *Verifier) ask(ctx context.Context, token, remoteIP string) (int, []byte, error) {
	form := url.Values{"secret": {v.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := v.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrUnavailable, unwrapURLError(err))
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: reading the reply: %s", ErrUnavailable, unwrapURLError(err))
	}
	return res.StatusCode, body, nil
}

// judge reads Cloudflare's reply: nil for an accepted answer, else an error that wraps ErrRejected
// or, for a reply that is not a verdict, ErrUnavailable
func judge(status int, body []byte) error {
	var verdict siteverifyResponse
	jsonErr := json.Unmarshal(body, &verdict)
	switch {
	case status >= 500:
		return fmt.Errorf("%w: Cloudflare answered %d", ErrUnavailable, status)
	case jsonErr != nil:
		return fmt.Errorf("%w: Cloudflare answered %d with a body that is not a verdict", ErrUnavailable, status)
	case !verdict.Success:
		return fmt.Errorf("%w: %s", ErrRejected, strings.Join(verdict.ErrorCodes, ", "))
	case status != http.StatusOK:
		return fmt.Errorf("%w: Cloudflare answered %d", ErrUnavailable, status)
	}
	return nil
}

// unwrapURLError drops the request URL from a transport error, which is not worth repeating in a log
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
