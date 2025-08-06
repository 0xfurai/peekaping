package monitor_tls_info

import (
	"peekaping/src/config"
	"peekaping/src/utils"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	// Register repository based on database type
	utils.RegisterRepositoryByDBType(
		container,
		cfg,
		func(db *bun.DB) Repository {
			return NewSQLRepository(db)
		},
		func(client *mongo.Client) Repository {
			return NewMongoRepository(client, cfg)
		},
	)

	// Register service
	container.Provide(func(
		repository Repository,
		logger *zap.SugaredLogger,
	) Service {
		return NewService(repository, logger)
	})
}
