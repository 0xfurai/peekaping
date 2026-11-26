package badge

import (
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers badge module dependencies
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewBadgeService, NewController, NewRoute)
}

// ServiceDependencies represents the dependencies for the badge service
type ServiceDependencies struct {
	dig.In
	MonitorService    monitor.Service
	HeartbeatService  heartbeat.Service
	StatsService      stats.Service
	TLSInfoService    monitor_tls_info.Service
	StatusPageService status_page.Service
	Logger            *zap.SugaredLogger
}

// NewBadgeService creates a new badge service with injected dependencies
func NewBadgeService(deps ServiceDependencies) Service {
	return &ServiceImpl{
		monitorService:    deps.MonitorService,
		heartbeatService:  deps.HeartbeatService,
		statsService:      deps.StatsService,
		tlsInfoService:    deps.TLSInfoService,
		statusPageService: deps.StatusPageService,
		svgGenerator:      NewSVGBadgeGenerator(),
		logger:            deps.Logger.Named("[badge-service]"),
	}
}
