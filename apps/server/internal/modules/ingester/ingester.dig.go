package ingester

import (
	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterDependencies registers ingester dependencies in the DI container: the location store and
// the service that stores check results
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLocationStore, NewService)
}

// ProvideLocationStore has the ingester keep a monitor's state on each probe in the monitor's own
// repository, where the monitor API reads it from
func ProvideLocationStore(repository monitor.MonitorRepository) LocationStore {
	return repository
}
