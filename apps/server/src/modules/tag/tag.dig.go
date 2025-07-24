package tag

import (
	"peekaping/src/config"
	"peekaping/src/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)
	container.Provide(NewService)
	container.Provide(NewController)
	container.Provide(NewRoute)
}
