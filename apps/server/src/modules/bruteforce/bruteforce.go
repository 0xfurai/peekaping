package bruteforce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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

type KeyExtractor func(*gin.Context) (string, error)

type Config struct {
	MaxAttempts int
	Window      time.Duration
	Lockout     time.Duration
	// Which HTTP statuses of the wrapped handler mean "authentication failed"
	FailureStatuses []int
	// Optional custom blocked response (otherwise 429 with Retry-After)
	OnBlocked func(c *gin.Context, retryAfter time.Duration)
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

func (g *Guard) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, err := g.keyExtractor(c)
		if err != nil || key == "" {
			// If we cannot extract key, we fallback to IP only.
			key = c.ClientIP()
		}

		ctx := c.Request.Context()

		locked, until, err := g.service.IsLocked(ctx, key)
		if err != nil {
			// Fail safe: pass, log/monitor
			g.logger.Errorw("failed to check if locked", "error", err)
			c.Next()
			return
		}
		if locked {
			// Fix race condition: recalculate retry time to avoid negative values
			retryAfter := time.Until(until)
			if retryAfter <= 0 {
				// Lock expired between check and now, allow request to proceed
				g.logger.Debugw("lock expired, allowing request", "key", key)
				c.Next()
				return
			}
			g.block(c, retryAfter)
			return
		}

		c.Next()

		// After handler runs, decide success/failure by status
		status := c.Writer.Status()
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
}

func (g *Guard) isFailure(status int) bool {
	for _, s := range g.cfg.FailureStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func (g *Guard) block(c *gin.Context, retryAfter time.Duration) {
	if g.cfg.OnBlocked != nil {
		g.cfg.OnBlocked(c, retryAfter)
		return
	}

	// Ensure retry time is not negative
	if retryAfter < 0 {
		retryAfter = 0
	}

	c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success":     false,
		"message":     "too many attempts, try later",
		"retry_after": int(retryAfter.Seconds()),
	})
}

// KeyByIPAndBodyField makes a key "<ip>:<lower(username)>"
// It safely reads the field from JSON body without consuming it by preserving the original body.
func KeyByIPAndBodyField(field string) KeyExtractor {
	return func(c *gin.Context) (string, error) {
		ip := c.ClientIP()

		// Only process JSON requests
		if c.GetHeader("Content-Type") == "application/json" || strings.Contains(c.GetHeader("Content-Type"), "application/json") {
			// Read body safely without consuming it
			if c.Request.Body != nil {
				bodyBytes, err := io.ReadAll(c.Request.Body)
				if err != nil {
					// On error, fallback to IP only
					return ip, nil
				}

				// Restore the body for subsequent handlers
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

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
		if c.Request.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			if v := c.PostForm(field); v != "" {
				return fmt.Sprintf("%s:%s", ip, strings.ToLower(v)), nil
			}
		}

		// Fallback to IP only
		return ip, nil
	}
}
