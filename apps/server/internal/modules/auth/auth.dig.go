package auth

import (
	"vigi/internal/config"
	"vigi/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)

	container.Provide(NewRoute)
	container.Provide(NewTokenMaker)

	// Register service with config
	container.Provide(func(repo Repository, tokenMaker *TokenMaker, logger *zap.SugaredLogger) Service {
		return NewService(repo, tokenMaker, logger, cfg)
	})
	container.Provide(NewController)
	container.Provide(NewMiddlewareProvider)
}
