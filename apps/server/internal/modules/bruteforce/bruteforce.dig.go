package bruteforce

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	if err := utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository); err != nil {
		return err
	}
	return utils.Provide(container, NewService, NewGuard)
}

// NewGuard creates a new bruteforce Guard with sensible defaults for login protection
func NewGuard(
	Service Service,
	Logger *zap.SugaredLogger,
	config *config.Config,
) *Guard {
	cfg := Config{
		MaxAttempts:     config.BruteforceMaxAttempts,
		Window:          config.BruteforceWindow,
		Lockout:         config.BruteforceLockout,
		FailureStatuses: []int{401, 403},
	}

	// Use IP + email for key extraction to track per user per IP
	keyExtractor := KeyByIPAndBodyField("email")

	guard := New(cfg, Service, keyExtractor, Logger)

	return guard
}
