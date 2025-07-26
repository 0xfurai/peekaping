package bruteforce

import (
	"context"
	"time"
)

type Repository interface {
	// FindByKey retrieves login state by key
	FindByKey(ctx context.Context, key string) (*Model, error)

	// Create creates a new login state record
	Create(ctx context.Context, model *Model) (*Model, error)

	// Update updates an existing login state record
	Update(ctx context.Context, key string, updateModel *UpdateModel) error

	// Delete removes a login state record
	Delete(ctx context.Context, key string) error

	// IsLocked checks if a key is currently locked
	IsLocked(ctx context.Context, key string) (bool, time.Time, error)

	// OnFailure atomically handles failure logic with window and locking
	OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error)
}
