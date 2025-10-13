package worker

import (
	"go.uber.org/dig"
)

// RegisterDependencies registers worker module dependencies
func RegisterDependencies(container *dig.Container) {
	container.Provide(NewHealthCheckTaskHandler)
	container.Provide(NewWorker)
}
