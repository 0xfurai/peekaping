package telemetry

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies provides the telemetry module. The transport of each binary provides the
// Mode (single or cluster), so that registration, not MODE, says how the install runs.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideService, NewController, NewRoute)
}

// provideService gives the service the parts of the modules it reads
func provideService(
	settings shared.SettingService,
	monitors monitor.Service,
	channels notification_channel.Service,
	statusPages status_page.Service,
	maintenances maintenance.Service,
	cfg *config.Config,
	mode Mode,
	logger *zap.SugaredLogger,
) Service {
	return NewService(settings, monitors, channels, statusPages, maintenances, cfg, mode, logger)
}
