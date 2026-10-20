package check

import (
	"go.uber.org/dig"
)

// RegisterDependencies registers check module dependencies
func RegisterDependencies(container *dig.Container) {
	container.Provide(NewLoader)
}
