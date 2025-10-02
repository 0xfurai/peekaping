package events

import (
	"go.uber.org/dig"
)

// RegisterDependencies registers event module dependencies
// Note: The EventBus implementation is provided by the infra layer
func RegisterDependencies(container *dig.Container) {
	// No dependencies to register
	// EventBus implementation is provided by infra.ProvideRedisEventBus
}
