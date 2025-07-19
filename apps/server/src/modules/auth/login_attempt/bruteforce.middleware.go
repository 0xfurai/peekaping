package login_attempt

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"peekaping/src/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// LoginDto defines the login request structure to avoid circular imports
type LoginDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
	Token    string `json:"token"`
}

type BruteforceMiddleware struct {
	bruteforceService BruteforceService
	logger            *zap.SugaredLogger
}

func NewBruteforceMiddleware(
	bruteforceService BruteforceService,
	logger *zap.SugaredLogger,
) *BruteforceMiddleware {
	return &BruteforceMiddleware{
		bruteforceService: bruteforceService,
		logger:            logger.Named("[bruteforce-middleware]"),
	}
}

// BruteforceProtection middleware checks for bruteforce attempts before login
func (m *BruteforceMiddleware) BruteforceProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only apply to login endpoint
		if c.Request.URL.Path != "/api/auth/login" || c.Request.Method != "POST" {
			c.Next()
			return
		}

		// Read the request body
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			m.logger.Errorw("Failed to read request body", "error", err)
			c.JSON(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
			c.Abort()
			return
		}

		// Restore the body for the next handler
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		// Parse the login DTO to get the email
		var loginDto LoginDto
		if err := json.Unmarshal(body, &loginDto); err != nil {
			m.logger.Debugw("Failed to parse login request body", "error", err)
			// Let the controller handle the validation error
			c.Next()
			return
		}

		// Validate that email is provided
		if loginDto.Email == "" {
			m.logger.Debugw("Empty email in login request")
			// Let the controller handle the validation error
			c.Next()
			return
		}

		// Store the parsed DTO for the controller
		c.Set("login_dto", loginDto)

		// Get client IP and user agent
		clientIP := m.getClientIP(c)
		userAgent := c.GetHeader("User-Agent")

		m.logger.Debugw("Processing login attempt", "email", loginDto.Email, "ip", clientIP, "user_agent", userAgent)

		// Check if the IP or email is currently blocked
		isBlocked, err := m.bruteforceService.IsBlocked(c.Request.Context(), loginDto.Email, clientIP)
		if err != nil {
			m.logger.Errorw("Failed to check bruteforce status", "error", err, "email", loginDto.Email, "ip", clientIP)
			// Continue with the request if we can't check the status
			c.Next()
			return
		}

		if isBlocked {
			m.logger.Warnw("Blocked login attempt", "email", loginDto.Email, "ip", clientIP, "user_agent", userAgent)
			c.JSON(http.StatusTooManyRequests, utils.NewFailResponse("Too many failed login attempts. Please try again later."))
			c.Abort()
			return
		}

		// Get current status to check for progressive delay
		status, err := m.bruteforceService.GetBruteforceStatus(c.Request.Context(), loginDto.Email, clientIP)
		if err != nil {
			m.logger.Errorw("Failed to get bruteforce status", "error", err, "email", loginDto.Email, "ip", clientIP)
			// Continue with the request if we can't get the status
			c.Next()
			return
		}

		// Apply progressive delay if required
		if status.RequiresProgressiveDelay && status.DelaySeconds > 0 {
			m.logger.Infow("Applying progressive delay", "email", loginDto.Email, "ip", clientIP, "delay_seconds", status.DelaySeconds)
			
			// Apply delay with a maximum cap for safety
			delayDuration := time.Duration(status.DelaySeconds) * time.Second
			if delayDuration > 5*time.Minute {
				delayDuration = 5 * time.Minute
			}
			
			time.Sleep(delayDuration)
		}

		// Continue with the request
		c.Next()
	}
}

// getClientIP extracts the real client IP from the request
func (m *BruteforceMiddleware) getClientIP(c *gin.Context) string {
	// Check X-Forwarded-For header first (proxy/load balancer)
	xForwardedFor := c.GetHeader("X-Forwarded-For")
	if xForwardedFor != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		parts := strings.Split(xForwardedFor, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP header (nginx proxy)
	xRealIP := c.GetHeader("X-Real-IP")
	if xRealIP != "" {
		return xRealIP
	}

	// Check CF-Connecting-IP header (Cloudflare)
	cfConnectingIP := c.GetHeader("CF-Connecting-IP")
	if cfConnectingIP != "" {
		return cfConnectingIP
	}

	// Fall back to ClientIP() which handles RemoteAddr and other headers
	ip := c.ClientIP()
	if ip != "" {
		return ip
	}

	// Last resort - use RemoteAddr directly
	return c.Request.RemoteAddr
}

// RecordLoginAttempt records a login attempt result
func (m *BruteforceMiddleware) RecordLoginAttempt(c *gin.Context, email string, success bool) {
	if email == "" {
		m.logger.Warnw("Attempted to record login attempt with empty email")
		return
	}

	clientIP := m.getClientIP(c)
	userAgent := c.GetHeader("User-Agent")

	m.logger.Debugw("Recording login attempt", "email", email, "ip", clientIP, "success", success)

	_, err := m.bruteforceService.CheckAndRecordAttempt(c.Request.Context(), email, clientIP, userAgent, success)
	if err != nil {
		m.logger.Errorw("Failed to record login attempt", "error", err, "email", email, "ip", clientIP, "success", success)
	}
}

// GetBruteforceStatus returns the current bruteforce status for debugging/monitoring
func (m *BruteforceMiddleware) GetBruteforceStatus(c *gin.Context, email string) (*BruteforceStatus, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}

	clientIP := m.getClientIP(c)
	return m.bruteforceService.GetBruteforceStatus(c.Request.Context(), email, clientIP)
}
