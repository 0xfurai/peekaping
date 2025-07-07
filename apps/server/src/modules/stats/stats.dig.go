package stats

import (
	"peekaping/src/config"
	"peekaping/src/modules/events"
	"peekaping/src/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)
	container.Provide(NewService)
	container.Invoke(func(s Service, bus *events.EventBus) {
		if impl, ok := s.(*ServiceImpl); ok {
			impl.RegisterEventHandlers(bus)
		}
	})
}
