package utils

import (
	"peekaping/src/config"

	"go.uber.org/dig"
)

// RegisterRepositoryByDBType registers the appropriate repository based on database type
// sqlRepoConstructor: constructor function for SQL repositories (e.g., NewSQLRepository)
// mongoRepoConstructor: constructor function for MongoDB repositories (e.g., NewMongoRepository)
func RegisterRepositoryByDBType(container *dig.Container, cfg *config.Config, sqlRepoConstructor, mongoRepoConstructor interface{}) {
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(sqlRepoConstructor)
	case "mongo":
		container.Provide(mongoRepoConstructor)
	}
}
