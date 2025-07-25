package bruteforce

import (
	"peekaping/src/config"
	"peekaping/src/utils"

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
	cfg     *config.Config
}

// NewGuard creates a new bruteforce Guard with sensible defaults for login protection
func NewGuard(params GuardParams) *Guard {
	cfg := Config{
		MaxAttempts:     params.cfg.BruteforceMaxAttempts,
		Window:          params.cfg.BruteforceWindow,
		Lockout:         params.cfg.BruteforceLockout,
		FailureStatuses: []int{401, 403},
	}

	// Use IP + email for key extraction to track per user per IP
	keyExtractor := KeyByIPAndBodyField("email")

	guard := New(cfg, params.Service, keyExtractor, params.Logger)

	return guard
}
