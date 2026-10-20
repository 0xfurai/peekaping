package domain_status_page

import (
	"peekaping/internal/config"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	if err := utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository); err != nil {
		return err
	}
	return utils.Provide(container, NewService)
}
