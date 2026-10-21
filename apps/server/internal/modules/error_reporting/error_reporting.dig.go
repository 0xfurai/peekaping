package error_reporting

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies provides the error reporting module. The transport of each binary
// provides the Switch: single mode's turns reporting on in its one process, and cluster mode's
// also copies the choice to Redis for the other processes.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideService, NewController, NewRoute)
}

func provideService(settings shared.SettingService, cfg *config.Config, sw Switch) Service {
	return NewService(settings, cfg, sw)
}
