package bruteforce

import (
	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewService, NewGuard, NewAccountGuard)
}

// NewGuard creates a new bruteforce Guard with sensible defaults for login protection
func NewGuard(
	Service Service,
	Logger *zap.SugaredLogger,
	config *config.Config,
	trustedProxies *httpx.TrustedProxies,
) *Guard {
	cfg := Config{
		MaxAttempts:     config.BruteforceMaxAttempts,
		Window:          config.BruteforceWindow,
		Lockout:         config.BruteforceLockout,
		FailureStatuses: []int{401, 403},
		TrustedProxies:  trustedProxies,
	}

	// Use IP + email for key extraction to track per user per IP
	keyExtractor := KeyByIPAndBodyField("email", trustedProxies)

	guard := New(cfg, Service, keyExtractor, Logger)

	return guard
}

// AccountGuard is the second guard on the login: it counts an account's failed logins whatever
// address they come from. It is its own type so that the container can provide it beside the
// Guard that counts per address.
type AccountGuard struct{ *Guard }

// NewAccountGuard creates the per-account guard for the login. It stops guessing spread over many
// addresses: a lock lasts the lockout, but the failure count is kept until the window has passed,
// so each further failure after a lock ends locks the account again. After an attack stops, the
// owner is back in within the window at most; one that keeps going can keep the account locked.
func NewAccountGuard(
	Service Service,
	Logger *zap.SugaredLogger,
	config *config.Config,
) *AccountGuard {
	cfg := Config{
		MaxAttempts:     config.BruteforceAccountMaxAttempts,
		Window:          config.BruteforceAccountWindow,
		Lockout:         config.BruteforceAccountLockout,
		FailureStatuses: []int{401, 403},
	}

	return &AccountGuard{New(cfg, Service, KeyByAccountEmail(), Logger)}
}
