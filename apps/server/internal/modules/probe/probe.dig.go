package probe

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies provides the probe repository, service, controller and route. The service
// takes the dbtx.Transactor that the monitor module provides.
func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	if err := utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewUnsupportedRepository); err != nil {
		return err
	}
	return utils.Provide(container, NewService, NewController, NewRoute)
}
