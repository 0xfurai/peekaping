package login_attempt

import (
	"time"
)

// LoginAttempt represents a login attempt record
type LoginAttempt struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	IPAddress   string    `json:"ip_address"`
	UserAgent   string    `json:"user_agent"`
	Success     bool      `json:"success"`
	AttemptedAt time.Time `json:"attempted_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// LoginAttemptCreateModel for creating new login attempts
type LoginAttemptCreateModel struct {
	Email     string `json:"email"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
}

// BruteforceStatus represents the current bruteforce protection status
type BruteforceStatus struct {
	IsBlocked                bool      `json:"is_blocked"`
	RemainingAttempts        int       `json:"remaining_attempts"`
	BlockedUntil             time.Time `json:"blocked_until"`
	FailedAttempts           int       `json:"failed_attempts"`
	LastFailedAttempt        time.Time `json:"last_failed_attempt"`
	ConsecutiveFailures      int       `json:"consecutive_failures"`
	RequiresProgressiveDelay bool      `json:"requires_progressive_delay"`
	DelaySeconds             int       `json:"delay_seconds"`
}
