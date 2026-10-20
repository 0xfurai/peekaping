package healthcheck

import (
	"peekaping/internal/modules/healthcheck/executor"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) {
	container.Provide(NewHealthCheck)
	container.Provide(executor.NewExecutorRegistry)
}
