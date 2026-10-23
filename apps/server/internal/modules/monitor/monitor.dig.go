package monitor

import (
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	if err := utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository); err != nil {
		return err
	}
	// The dbtx.Transactor is provided here, beside the repositories, so that it resolves
	// wherever the monitor service does: it writes a monitor and its links in one transaction
	if err := utils.RegisterRepositoryByDBType(container, cfg, dbtx.NewSQLTransactor, dbtx.NewMongoTransactor); err != nil {
		return err
	}
	return utils.Provide(container, NewMonitorService, NewMonitorController, NewMonitorRoute, NewMonitorEventListener)
}
