package api_key

import (
	"vigi/internal/config"
	"vigi/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)

	container.Provide(NewRoute)
	container.Provide(NewService)
	container.Provide(NewController)
	container.Provide(NewMiddlewareProvider)
}
