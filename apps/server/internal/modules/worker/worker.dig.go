package worker

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies registers worker module dependencies
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewHealthCheckTaskHandler, NewWorker)
}
