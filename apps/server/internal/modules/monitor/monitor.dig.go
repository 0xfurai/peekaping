package monitor

import (
	"vigi/internal/config"
	"vigi/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)
	container.Provide(NewMonitorService)
	container.Provide(NewMonitorController)
	container.Provide(NewMonitorRoute)
	container.Provide(NewMonitorEventListener)
}
