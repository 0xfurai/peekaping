package healthcheck

import (
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, supervisor.NewHealthCheck, executor.NewExecutorRegistry)
}
