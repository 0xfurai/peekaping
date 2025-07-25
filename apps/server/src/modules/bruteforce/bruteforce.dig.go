package bruteforce

import (
	"peekaping/src/config"
	"peekaping/src/utils"
	"time"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)

	container.Provide(NewService)
	container.Provide(NewGuard)
}

// GuardParams holds dependencies for creating a bruteforce Guard
type GuardParams struct {
	dig.In

	Service Service
	Logger  *zap.SugaredLogger
}

// NewGuard creates a new bruteforce Guard with sensible defaults for login protection
func NewGuard(params GuardParams) *Guard {
	cfg := Config{
		MaxAttempts:     5,                // Allow 5 failed attempts
		Window:          time.Minute,      // Within 1 minute window
		Lockout:         15 * time.Minute, // Lock for 15 minutes
		FailureStatuses: []int{400, 401},  // Consider 400 (validation errors) and 401 (invalid credentials) as failures
	}

	// Use IP + email for key extraction to track per user per IP
	keyExtractor := KeyByIPAndBodyField("email")

	guard := New(cfg, params.Service, keyExtractor)

	params.Logger.Info("Bruteforce protection initialized with 5 attempts per minute, 15 minute lockout")

	return guard
}
