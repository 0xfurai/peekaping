package login_attempt

import (
	"context"
	"errors"
	"math"
	"peekaping/src/config"
	"time"

	"go.uber.org/zap"
)

type BruteforceService interface {
	CheckAndRecordAttempt(ctx context.Context, email, ipAddress, userAgent string, success bool) (*BruteforceStatus, error)
	GetBruteforceStatus(ctx context.Context, email, ipAddress string) (*BruteforceStatus, error)
	IsBlocked(ctx context.Context, email, ipAddress string) (bool, error)
	CleanupOldAttempts(ctx context.Context) error
	ResetFailedAttempts(ctx context.Context, email, ipAddress string) error
}

type BruteforceServiceImpl struct {
	repo   LoginAttemptRepository
	config *config.Config
	logger *zap.SugaredLogger
}

func NewBruteforceService(
	repo LoginAttemptRepository,
	config *config.Config,
	logger *zap.SugaredLogger,
) BruteforceService {
	return &BruteforceServiceImpl{
		repo:   repo,
		config: config,
		logger: logger.Named("[bruteforce-service]"),
	}
}

func (s *BruteforceServiceImpl) CheckAndRecordAttempt(ctx context.Context, email, ipAddress, userAgent string, success bool) (*BruteforceStatus, error) {
	// Record the attempt
	attempt := &LoginAttemptCreateModel{
		Email:     email,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Success:   success,
	}

	_, err := s.repo.Create(ctx, attempt)
	if err != nil {
		s.logger.Errorw("Failed to record login attempt", "error", err, "email", email, "ip", ipAddress)
		return nil, err
	}

	// If the attempt was successful, return success status
	if success {
		s.logger.Infow("Successful login recorded", "email", email, "ip", ipAddress)
		return &BruteforceStatus{
			IsBlocked:                false,
			RemainingAttempts:        s.config.BruteforceMaxAttempts,
			FailedAttempts:           0,
			ConsecutiveFailures:      0,
			RequiresProgressiveDelay: false,
			DelaySeconds:             0,
		}, nil
	}

	// For failed attempts, log and return current status
	s.logger.Warnw("Failed login attempt recorded", "email", email, "ip", ipAddress)
	return s.GetBruteforceStatus(ctx, email, ipAddress)
}

func (s *BruteforceServiceImpl) GetBruteforceStatus(ctx context.Context, email, ipAddress string) (*BruteforceStatus, error) {
	windowStart := time.Now().Add(-time.Duration(s.config.BruteforceWindowMinutes) * time.Minute)

	// Get failed attempts for email in the window
	emailAttempts, err := s.repo.GetFailedAttemptsByEmail(ctx, email, windowStart)
	if err != nil {
		s.logger.Errorw("Failed to get failed attempts by email", "error", err, "email", email)
		return nil, err
	}

	// Get failed attempts for IP in the window
	ipAttempts, err := s.repo.GetFailedAttemptsByIP(ctx, ipAddress, windowStart)
	if err != nil {
		s.logger.Errorw("Failed to get failed attempts by IP", "error", err, "ip", ipAddress)
		return nil, err
	}

	// Get consecutive failures for email
	consecutiveEmailFailures, err := s.repo.GetConsecutiveFailedAttemptsByEmail(ctx, email)
	if err != nil {
		s.logger.Errorw("Failed to get consecutive failures by email", "error", err, "email", email)
		return nil, err
	}

	// Get consecutive failures for IP
	consecutiveIPFailures, err := s.repo.GetConsecutiveFailedAttemptsByIP(ctx, ipAddress)
	if err != nil {
		s.logger.Errorw("Failed to get consecutive failures by IP", "error", err, "ip", ipAddress)
		return nil, err
	}

	emailFailureCount := len(emailAttempts)
	ipFailureCount := len(ipAttempts)
	consecutiveEmailCount := len(consecutiveEmailFailures)
	consecutiveIPCount := len(consecutiveIPFailures)

	// Check if blocked by email attempts
	emailBlocked := emailFailureCount >= s.config.BruteforceMaxAttempts
	ipBlocked := ipFailureCount >= s.config.BruteforceMaxAttemptsPerIP

	isBlocked := emailBlocked || ipBlocked

	// Calculate remaining attempts
	remainingAttempts := s.config.BruteforceMaxAttempts - emailFailureCount
	if remainingAttempts < 0 {
		remainingAttempts = 0
	}

	// Calculate when the block will be lifted
	var blockedUntil time.Time
	var lastFailedAttempt time.Time

	if len(emailAttempts) > 0 {
		lastFailedAttempt = emailAttempts[0].AttemptedAt
	}
	if len(ipAttempts) > 0 && ipAttempts[0].AttemptedAt.After(lastFailedAttempt) {
		lastFailedAttempt = ipAttempts[0].AttemptedAt
	}

	if isBlocked {
		blockedUntil = lastFailedAttempt.Add(s.config.BruteforceBlockDuration)
	}

	// Progressive delay calculation
	var delaySeconds int
	requiresProgressiveDelay := false

	if s.config.BruteforceProgressiveDelay && (consecutiveEmailCount > 0 || consecutiveIPCount > 0) {
		// Use the higher of the two consecutive failure counts
		consecutiveFailures := consecutiveEmailCount
		if consecutiveIPCount > consecutiveFailures {
			consecutiveFailures = consecutiveIPCount
		}

		if consecutiveFailures >= 3 {
			requiresProgressiveDelay = true
			// Exponential backoff: 2^(failures-3) seconds, max 300 seconds (5 minutes)
			delaySeconds = int(math.Min(math.Pow(2, float64(consecutiveFailures-3)), 300))
		}
	}

	// Use the higher failure count for reporting
	totalFailures := emailFailureCount
	if ipFailureCount > totalFailures {
		totalFailures = ipFailureCount
	}

	consecutiveFailures := consecutiveEmailCount
	if consecutiveIPCount > consecutiveFailures {
		consecutiveFailures = consecutiveIPCount
	}

	status := &BruteforceStatus{
		IsBlocked:                isBlocked,
		RemainingAttempts:        remainingAttempts,
		BlockedUntil:             blockedUntil,
		FailedAttempts:           totalFailures,
		LastFailedAttempt:        lastFailedAttempt,
		ConsecutiveFailures:      consecutiveFailures,
		RequiresProgressiveDelay: requiresProgressiveDelay,
		DelaySeconds:             delaySeconds,
	}

	s.logger.Debugw("Bruteforce status calculated", 
		"email", email, 
		"ip", ipAddress,
		"blocked", isBlocked,
		"email_failures", emailFailureCount,
		"ip_failures", ipFailureCount,
		"consecutive_failures", consecutiveFailures,
		"remaining_attempts", remainingAttempts,
		"blocked_until", blockedUntil,
	)

	return status, nil
}

func (s *BruteforceServiceImpl) IsBlocked(ctx context.Context, email, ipAddress string) (bool, error) {
	status, err := s.GetBruteforceStatus(ctx, email, ipAddress)
	if err != nil {
		return false, err
	}

	// Check if currently blocked
	if status.IsBlocked {
		// Check if the block has expired
		if time.Now().After(status.BlockedUntil) {
			s.logger.Infow("Block expired", "email", email, "ip", ipAddress, "blocked_until", status.BlockedUntil)
			return false, nil
		}
		s.logger.Infow("Account/IP is blocked", "email", email, "ip", ipAddress, "blocked_until", status.BlockedUntil)
		return true, nil
	}

	return false, nil
}

func (s *BruteforceServiceImpl) ResetFailedAttempts(ctx context.Context, email, ipAddress string) error {
	s.logger.Infow("Resetting failed attempts after successful login", "email", email, "ip", ipAddress)
	// Note: We don't actually delete the records, as successful login attempts
	// are recorded and will naturally reset the consecutive failure count
	// This method is here for potential future use or explicit reset scenarios
	return nil
}

func (s *BruteforceServiceImpl) CleanupOldAttempts(ctx context.Context) error {
	// Clean up attempts older than the cleanup interval
	cutoffTime := time.Now().Add(-s.config.BruteforceCleanupInterval)

	err := s.repo.DeleteOldAttempts(ctx, cutoffTime)
	if err != nil {
		s.logger.Errorw("Failed to cleanup old login attempts", "error", err)
		return err
	}

	s.logger.Infow("Successfully cleaned up old login attempts", "cutoff_time", cutoffTime)
	return nil
}

// GetProgressiveDelay calculates the progressive delay based on consecutive failures
func (s *BruteforceServiceImpl) GetProgressiveDelay(consecutiveFailures int) time.Duration {
	if !s.config.BruteforceProgressiveDelay || consecutiveFailures < 3 {
		return 0
	}

	// Exponential backoff: 2^(failures-3) seconds, max 300 seconds (5 minutes)
	delaySeconds := int(math.Min(math.Pow(2, float64(consecutiveFailures-3)), 300))
	return time.Duration(delaySeconds) * time.Second
}

// Common errors
var (
	ErrAccountBlocked = errors.New("account temporarily blocked due to too many failed login attempts")
	ErrIPBlocked      = errors.New("IP address temporarily blocked due to too many failed login attempts")
	ErrRateLimited    = errors.New("too many login attempts, please try again later")
)
