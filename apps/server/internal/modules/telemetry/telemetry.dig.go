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
// Mode (single or cluster), so that registration, not MODE, says how the install runs, and the
// Claimer that picks the one process that sends the day's ping.
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, provideService, provideSender, NewController, NewRoute)
}

// provideSender provides the sender that cmd/peekaping and cmd/api start
func provideSender(service Service, claimer Claimer, cfg *config.Config, logger *zap.SugaredLogger) *Sender {
	return NewSender(service, claimer, cfg.TelemetryURL, logger)
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
) Service {
	return NewService(settings, monitors, channels, statusPages, maintenances, cfg, mode)
}
