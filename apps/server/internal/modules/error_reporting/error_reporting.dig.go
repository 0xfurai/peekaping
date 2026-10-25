package error_reporting

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies provides the error reporting module. Each api replica, like the single-mode
// process, turns reporting on or off in its own process (LocalSwitch).
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideService, provideSwitch, NewController, NewRoute)
}

func provideSwitch() Switch { return LocalSwitch{} }

func provideService(settings shared.SettingService, cfg *config.Config, sw Switch, logger *zap.SugaredLogger) Service {
	return NewService(settings, cfg, sw, logger)
}
