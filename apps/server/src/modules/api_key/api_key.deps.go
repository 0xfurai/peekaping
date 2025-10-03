package api_key

import (
	"peekaping/src/config"

	"go.uber.org/dig"
)

// RegisterDependencies registers all API key dependencies
func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	// Register repository based on database type
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(NewSQLRepository)
	case "mongo", "mongodb":
		container.Provide(NewMongoRepository)
	}

	// Register service
	container.Provide(NewService)

	// Register controller
	container.Provide(NewController)

	// Register middleware
	container.Provide(NewMiddlewareProvider)

	// Register route
	container.Provide(NewRoute)
}
