package bruteforce

import (
	"bytes"
	"context"
	"encoding/json"
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

type KeyExtractor func(*http.Request) (string, error)

type Config struct {
	MaxAttempts int
	Window      time.Duration
	Lockout     time.Duration
	// Which HTTP statuses of the wrapped handler mean "authentication failed"
	FailureStatuses []int
	// Optional custom blocked response (otherwise 429 with Retry-After)
	OnBlocked func(w http.ResponseWriter, r *http.Request, retryAfter time.Duration)
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
			if err != nil || key == "" {
				// If we cannot extract key, we fallback to IP only.
				key = httpx.ClientIP(r)
			}

			ctx := r.Context()

			locked, until, err := g.service.IsLocked(ctx, key)
			if err != nil {
				// Fail safe: pass, log/monitor
				g.logger.Errorw("failed to check if locked", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			if locked {
				// Fix race condition: recalculate retry time to avoid negative values
				retryAfter := time.Until(until)
				if retryAfter <= 0 {
					// Lock expired between check and now, allow request to proceed
					g.logger.Debugw("lock expired, allowing request", "key", key)
					next.ServeHTTP(w, r)
					return
				}
				g.block(w, r, retryAfter)
				return
			}

			recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)

			// After handler runs, decide success/failure by status
			status := recorder.status
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
		})
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

// KeyByIPAndBodyField makes a key "<ip>:<lower(username)>"
// It safely reads the field from JSON body without consuming it by preserving the original body.
func KeyByIPAndBodyField(field string) KeyExtractor {
	return func(r *http.Request) (string, error) {
		ip := httpx.ClientIP(r)

		// Only process JSON requests
		if r.Header.Get("Content-Type") == "application/json" || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			// Read body safely without consuming it
			if r.Body != nil {
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					// On error, fallback to IP only
					return ip, nil
				}

				// Restore the body for subsequent handlers
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Try to parse JSON and extract field
				var m map[string]any
				if err := json.Unmarshal(bodyBytes, &m); err == nil {
					if v, ok := m[field]; ok {
						if s, ok := v.(string); ok && s != "" {
							return fmt.Sprintf("%s:%s", ip, strings.ToLower(s)), nil
						}
					}
				}
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
