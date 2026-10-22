package check

import (
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers check module dependencies
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLoader)
}

// ProvideLoader provides the loader. dig matches parameter types exactly, so it hands the
// loader the services it provides, and the loader reads them through its narrower interfaces.
func ProvideLoader(
	monitorService monitor.Service,
	proxyService proxy.Service,
	maintenanceService maintenance.Service,
	heartbeatService heartbeat.Service,
	logger *zap.SugaredLogger,
) *Loader {
	return NewLoader(monitorService, proxyService, maintenanceService, heartbeatService, logger)
}
