package login_attempt

import (
	"context"
	"time"
)

// LoginAttemptRepository interface for login attempt operations
type LoginAttemptRepository interface {
	// Create a new login attempt record
	Create(ctx context.Context, attempt *LoginAttemptCreateModel) (*LoginAttempt, error)

	// Get failed attempts for an email within a time window
	GetFailedAttemptsByEmail(ctx context.Context, email string, since time.Time) ([]*LoginAttempt, error)

	// Get failed attempts for an IP address within a time window
	GetFailedAttemptsByIP(ctx context.Context, ipAddress string, since time.Time) ([]*LoginAttempt, error)

	// Get consecutive failed attempts for an email (until a successful login)
	GetConsecutiveFailedAttemptsByEmail(ctx context.Context, email string) ([]*LoginAttempt, error)

	// Get consecutive failed attempts for an IP address (until a successful login)
	GetConsecutiveFailedAttemptsByIP(ctx context.Context, ipAddress string) ([]*LoginAttempt, error)

	// Delete old login attempts (for cleanup)
	DeleteOldAttempts(ctx context.Context, olderThan time.Time) error

	// Get the last successful login for an email
	GetLastSuccessfulLogin(ctx context.Context, email string) (*LoginAttempt, error)

	// Get total attempts count for an email/IP in the last period
	GetAttemptsCount(ctx context.Context, email, ipAddress string, since time.Time) (int64, error)
}
