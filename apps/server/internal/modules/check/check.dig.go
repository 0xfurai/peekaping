package check

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies registers check module dependencies
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewLoader)
}
