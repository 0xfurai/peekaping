package healthcheck

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/netguard"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideGuard, provideExecutorRegistry, provideSupervisor)
}

// provideGuard is the install's guard against checks that reach the internal network
// (BLOCK_PRIVATE_TARGETS). The executors dial through it, and the API checks the targets of a
// monitor or proxy it saves against it. It is a single value so that they agree.
func provideGuard(cfg *config.Config) *netguard.Guard {
	return netguard.New(cfg.BlockPrivateTargets)
}

type registryParams struct {
	dig.In
	Logger *zap.SugaredLogger
	// Guard is left out of a container that has none, whose checks reach everything
	Guard *netguard.Guard `optional:"true"`
}

func provideExecutorRegistry(p registryParams) *executor.ExecutorRegistry {
	return executor.NewExecutorRegistry(p.Logger, executor.WithGuard(p.Guard))
}

// provideSupervisor provides the supervisor, which spaces the checks to one target host to
// CHECKS_PER_HOST_PER_MINUTE (off at 0): the scheduler of single mode and the push checks of the
// cluster api run their checks through it.
func provideSupervisor(
	cfg *config.Config,
	eventBus events.EventBus,
	registry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
) *supervisor.HealthCheckSupervisor {
	checks := supervisor.NewHealthCheck(eventBus, registry, logger)
	checks.SetChecksPerHostPerMinute(cfg.ChecksPerHostPerMinute)
	return checks
}
