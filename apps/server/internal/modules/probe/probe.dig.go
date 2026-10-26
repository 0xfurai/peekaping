package probe

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies provides the probe repository, service, availability service, controller
// and route. The service takes the dbtx.Transactor that the monitor module provides.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewService, provideAvailabilityService, NewController, NewRoute)
}
