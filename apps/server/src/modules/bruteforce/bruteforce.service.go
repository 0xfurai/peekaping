package bruteforce

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type ServiceImpl struct {
	repo   Repository
	logger *zap.SugaredLogger
}

func NewService(
	repo Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repo:   repo,
		logger: logger.Named("[bruteforce-service]"),
	}
}

// IsLocked returns current lock (if any).
func (s *ServiceImpl) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	return s.repo.IsLocked(ctx, key)
}

// OnFailure atomically updates counters and may set a lock.
// Returns (locked, until, err).
func (s *ServiceImpl) OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error) {
	return s.repo.OnFailure(ctx, key, now, window, max, lockout)
}

// Reset clears all state for the key (on successful auth).
func (s *ServiceImpl) Reset(ctx context.Context, key string) error {
	return s.repo.Delete(ctx, key)
}
