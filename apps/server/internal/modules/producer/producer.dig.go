package producer

import (
	"go.uber.org/dig"
)

// RegisterDependencies registers producer dependencies with the DI container
func RegisterDependencies(container *dig.Container) {
	container.Provide(NewProducer)
}
