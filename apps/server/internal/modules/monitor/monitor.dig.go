package monitor

import (
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/stats"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container, cfg *config.Config) error {
	if err := utils.RegisterRepositoryByDBType(container, cfg, NewSQLRepository, NewMongoRepository); err != nil {
		return err
	}
	// The dbtx.Transactor is provided here, beside the repositories, so that it resolves
	// wherever the monitor service does: it writes a monitor and its links in one transaction
	if err := utils.RegisterRepositoryByDBType(container, cfg, dbtx.NewSQLTransactor, dbtx.NewMongoTransactor); err != nil {
		return err
	}
	return utils.Provide(container, provideMonitorService, NewMonitorController, NewMonitorRoute, NewMonitorEventListener)
}

// serviceParams are what NewMonitorService takes
type serviceParams struct {
	dig.In

	Repository       MonitorRepository
	Transactor       dbtx.Transactor
	Heartbeats       heartbeat.Service
	EventBus         events.EventBus
	Notifications    monitor_notification.Service
	Tags             monitor_tag.Service
	ExecutorRegistry *executor.ExecutorRegistry
	Stats            stats.Service
	// Probes is provided by internal/app, with the probe module. A container without it has no
	// probes but the local one, as an install on MongoDB does.
	Probes ProbeLookup `optional:"true"`
	Logger *zap.SugaredLogger
}

func provideMonitorService(p serviceParams) Service {
	return NewMonitorService(p.Repository, p.Transactor, p.Heartbeats, p.EventBus, p.Notifications, p.Tags, p.ExecutorRegistry, p.Stats, p.Probes, p.Logger)
}
