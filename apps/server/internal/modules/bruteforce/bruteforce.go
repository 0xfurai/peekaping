package bruteforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"peekaping/internal/httpx"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Service interface for bruteforce protection
type Service interface {
	// IsLocked returns current lock (if any).
	IsLocked(ctx context.Context, key string) (bool, time.Time, error)
	// OnFailure atomically updates counters and may set a lock.
	// Returns (locked, until, err).
	OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error)
	// Reset clears all state for the key (on successful auth).
	Reset(ctx context.Context, key string) error
}

// KeyExtractor returns the key a request's failures are counted under. It returns ErrNoKey for a
// request that has nothing to count. On any other error, or an empty key, the guard counts the
// failure under the client's IP address.
type KeyExtractor func(*http.Request) (string, error)

// ErrNoKey is what a KeyExtractor returns for a request that has no key, such as a login
// without an email. The guard lets it through without counting it.
var ErrNoKey = errors.New("bruteforce: the request has no key")

// ErrBodyUnreadable is what a KeyExtractor returns when it can't read the request body. The guard
// answers 400 instead of passing the request on: a request it can't key must not reach the handler.
var ErrBodyUnreadable = errors.New("bruteforce: the request body can't be read")

type Config struct {
	MaxAttempts int
	Window      time.Duration
	Lockout     time.Duration
	// Which HTTP statuses of the wrapped handler mean "authentication failed"
	FailureStatuses []int
	// Optional custom blocked response (otherwise 429 with Retry-After)
	OnBlocked func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration)
	// TrustedProxies decide whose forwarding headers count when the guard falls back to
	// counting by the client's IP address. Nil trusts nobody.
	TrustedProxies *httpx.TrustedProxies
}

type Guard struct {
	cfg          Config
	service      Service
	keyExtractor KeyExtractor
	logger       *zap.SugaredLogger
}

func New(cfg Config, service Service, ke KeyExtractor, logger *zap.SugaredLogger) *Guard {
	// sensible defaults
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.Lockout <= 0 {
		cfg.Lockout = 1 * time.Minute
	}
	if cfg.FailureStatuses == nil {
		cfg.FailureStatuses = []int{401, 403}
	}
	return &Guard{
		cfg:          cfg,
		service:      service,
		keyExtractor: ke,
		logger:       logger,
	}
}

func (g *Guard) Middleware() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, err := g.keyExtractor(r)
			if errors.Is(err, ErrNoKey) {
				next.ServeHTTP(w, r)
				return
			}
			if answerUnkeyable(w, err) {
				return
			}
			if err != nil || key == "" {
				// If we cannot extract key, we fallback to IP only.
				key = g.cfg.TrustedProxies.ClientIP(r)
			}

			ctx := r.Context()

			switch state, retryAfter := g.checkLock(ctx, key); state {
			case lockPass:
				next.ServeHTTP(w, r)
				return
			case lockBlocked:
				g.block(w, r, retryAfter)
				return
			}

			recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)

			// After handler runs, decide success/failure by status
			g.record(ctx, key, recorder.status)
		})
	}
}

// answerUnkeyable answers a request whose key the extractor could not read for a reason that must
// not reach the handler, and says whether it did
func answerUnkeyable(w http.ResponseWriter, err error) bool {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		httpx.JSON(w, http.StatusRequestEntityTooLarge, httpx.ErrorBody{Message: "request body too large"})
		return true
	case errors.Is(err, ErrBodyUnreadable):
		httpx.JSON(w, http.StatusBadRequest, httpx.ErrorBody{Message: "failed to read the request body"})
		return true
	}
	return false
}

// lockState is what the guard finds when it asks whether a key is locked
type lockState int

const (
	// lockClear: the key is not locked, so the request goes through the handler and is counted
	lockClear lockState = iota
	// lockPass: the request goes to the handler uncounted, because the lock can't be read or has ended
	lockPass
	// lockBlocked: the key is locked
	lockBlocked
)

// checkLock asks whether the key is locked. A blocked key also gets how long until its lock ends.
func (g *Guard) checkLock(ctx context.Context, key string) (lockState, time.Duration) {
	locked, until, err := g.service.IsLocked(ctx, key)
	if err != nil {
		// Fail safe: pass, log/monitor
		g.logger.Errorw("failed to check if locked", "error", err)
		return lockPass, 0
	}
	if !locked {
		return lockClear, 0
	}
	// Fix race condition: recalculate retry time to avoid negative values
	retryAfter := time.Until(until)
	if retryAfter <= 0 {
		// Lock expired between check and now, allow request to proceed
		g.logger.Debugw("lock expired, allowing request", "key", key)
		return lockPass, 0
	}
	return lockBlocked, retryAfter
}

// record counts the handler's answer: a failure status counts against the key, and a 2xx clears it
func (g *Guard) record(ctx context.Context, key string, status int) {
	if g.isFailure(status) {
		now := time.Now()
		// OnFailure atomically handles counting and locking
		locked, until, err := g.service.OnFailure(ctx, key, now, g.cfg.Window, g.cfg.MaxAttempts, g.cfg.Lockout)
		if err != nil {
			// Log error but don't fail the request - the auth failure should still be returned
			g.logger.Errorw("failed to record failure", "key", key, "error", err)
		} else if locked {
			// If we just got locked, we could optionally notify the client
			// For now, just let the request complete normally
			g.logger.Infow("account locked due to too many failures", "key", key, "until", until)
		}
		return
	}

	// Only reset on explicit success statuses (200-299), not on other statuses like 400, 500
	if status >= 200 && status < 300 {
		if err := g.service.Reset(ctx, key); err != nil {
			g.logger.Errorw("failed to reset bruteforce state", "key", key, "error", err)
		}
	}
}

func (g *Guard) isFailure(status int) bool {
	for _, s := range g.cfg.FailureStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func (g *Guard) block(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	if g.cfg.OnBlocked != nil {
		g.cfg.OnBlocked(w, r, retryAfter)
		return
	}

	// Ensure retry time is not negative
	if retryAfter < 0 {
		retryAfter = 0
	}

	w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
	httpx.JSON(w, http.StatusTooManyRequests, BlockResponse{Success: false, Message: "too many attempts, try later", RetryAfter: int(retryAfter.Seconds())})
}

// KeyByIPAndBodyField makes a key "<ip>:<lower(username)>", where <ip> is the client address
// that proxies resolves (nil trusts no forwarding headers).
// It safely reads the field from JSON body without consuming it by preserving the original body.
func KeyByIPAndBodyField(field string, proxies *httpx.TrustedProxies) KeyExtractor {
	return func(r *http.Request) (string, error) {
		ip := proxies.ClientIP(r)

		// Only process JSON requests
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			if key, ok := jsonBodyKey(r, ip, field); ok {
				return key, nil
			}
		}

		// For form requests, try PostForm (this doesn't interfere with JSON parsing)
		if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			if v := r.PostFormValue(field); v != "" {
				return fmt.Sprintf("%s:%s", ip, strings.ToLower(v)), nil
			}
		}

		// Fallback to IP only
		return ip, nil
	}
}

// jsonBodyKey is the key of a JSON request: the address and the lower-cased field of its body, or
// the address alone when the body can't be read. It says false when the body has no such field, so
// that the caller falls back to the address.
func jsonBodyKey(r *http.Request, ip, field string) (string, bool) {
	// Read body safely without consuming it
	if r.Body == nil {
		return "", false
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// On error, fallback to IP only
		return ip, true
	}

	// Restore the body for subsequent handlers
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Try to parse JSON and extract field
	var m map[string]any
	if err := json.Unmarshal(bodyBytes, &m); err != nil {
		return "", false
	}
	if s, ok := m[field].(string); ok && s != "" {
		return fmt.Sprintf("%s:%s", ip, strings.ToLower(s)), true
	}
	return "", false
}

// accountKeyPrefix keeps an account's key apart from the "<ip>:<email>" keys in the same table.
const accountKeyPrefix = "account:"

// KeyByAccountEmail makes a key "account:<lower(email)>" that doesn't depend on the client's
// address, so failures count against the account however many addresses they come from.
//
// It reads the email the way the login handler does: the first JSON value in the body, whatever
// the Content-Type, with the field name matched case-insensitively. Reading it any other way
// would let a client keep its email from the guard while the handler still sees it. A request
// without an email returns ErrNoKey; the handler rejects it without checking a password. A body
// that can't be read returns ErrBodyUnreadable, or the *http.MaxBytesError of a body over its
// limit (set it with httpx.LimitBody), and the guard answers 400 or 413 without running the
// handler.
func KeyByAccountEmail() KeyExtractor {
	return func(r *http.Request) (string, error) {
		if r.Body == nil {
			return "", ErrNoKey
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return "", err
			}
			return "", fmt.Errorf("%w: %v", ErrBodyUnreadable, err)
		}
		// Restore the body for the handler
		r.Body = io.NopCloser(bytes.NewReader(body))
		var login struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&login); err != nil || login.Email == "" {
			return "", ErrNoKey
		}
		return accountKeyPrefix + strings.ToLower(login.Email), nil
	}
}

// This wrapper is only used around the login handler, which does not stream or upgrade.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// BlockResponse is the existing login lockout response.
type BlockResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after"`
}
