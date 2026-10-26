package notification_channel

import (
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, NewProviderRegistry, NewService, NewController, NewRoute, NewNotificationEventListener)
}
