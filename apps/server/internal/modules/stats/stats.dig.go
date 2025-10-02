package stats

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository)
	container.Provide(NewService)
	container.Invoke(func(s Service, bus events.EventBus) {
		if impl, ok := s.(*ServiceImpl); ok {
			impl.RegisterEventHandlers(bus)
		}
	})
}
