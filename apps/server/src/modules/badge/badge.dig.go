package badge

import (
	"peekaping/src/config"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/monitor_status_page"
	"peekaping/src/modules/monitor_tls_info"
	"peekaping/src/modules/stats"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers badge module dependencies
func RegisterDependencies(container *dig.Container, cfg *config.Config) {
	container.Provide(NewBadgeService)
	container.Provide(NewController)
	container.Provide(NewRoute)
}

// ServiceDependencies represents the dependencies for the badge service
type ServiceDependencies struct {
	dig.In
	MonitorService           monitor.Service
	HeartbeatService         heartbeat.Service
	StatsService             stats.Service
	TLSInfoService           monitor_tls_info.Service
	MonitorStatusPageService monitor_status_page.Service
	Logger                   *zap.SugaredLogger
}

// NewBadgeService creates a new badge service with injected dependencies
func NewBadgeService(deps ServiceDependencies) Service {
	return &ServiceImpl{
		monitorService:           deps.MonitorService,
		heartbeatService:         deps.HeartbeatService,
		statsService:             deps.StatsService,
		tlsInfoService:           deps.TLSInfoService,
		monitorStatusPageService: deps.MonitorStatusPageService,
		svgGenerator:             NewSVGBadgeGenerator(),
		logger:                   deps.Logger.Named("[badge-service]"),
	}
}
