package monitor_tls_info

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	// Register repository based on database type
	err := utils.RegisterRepositoryByDBType(
		container,
		cfg,
		func(db *bun.DB) Repository {
			return NewSQLRepository(db)
		},
		func(client *mongo.Client) Repository {
			return NewMongoRepository(client, cfg)
		},
	)
	if err != nil {
		return err
	}

	// Register service
	return utils.Provide(container, func(
		repository Repository,
		logger *zap.SugaredLogger,
	) Service {
		return NewService(repository, logger)
	})
}
