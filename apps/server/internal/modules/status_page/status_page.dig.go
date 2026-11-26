package status_page

import (
	"peekaping/internal/dbtx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewSQLRepository, provideService, NewController, NewRoute)
}

// serviceParams are what NewService takes
type serviceParams struct {
	dig.In

	Repository Repository
	EventBus   events.EventBus
	Monitors   monitor_status_page.Service
	Domains    domain_status_page.Service
	Logger     *zap.SugaredLogger
	// Limits are the workspaces' limits, from package limits, and Transactor the monitor module's.
	// A container without either has no limits.
	Limits     limits.Source   `optional:"true"`
	Transactor dbtx.Transactor `optional:"true"`
}

func provideService(p serviceParams) Service {
	var opts []ServiceOption
	if p.Limits != nil && p.Transactor != nil {
		opts = append(opts, WithLimits(p.Limits, p.Transactor))
	}
	return NewService(p.Repository, p.EventBus, p.Monitors, p.Domains, p.Logger, opts...)
}
