package notification_sent_history

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, func(
		repository Repository,
		logger *zap.SugaredLogger,
	) Service {
		return NewService(repository, logger)
	})
}
