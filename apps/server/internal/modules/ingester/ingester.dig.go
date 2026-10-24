package ingester

import (
	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"

	"github.com/hibiken/asynq"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers ingester dependencies in the DI container: the service that
// stores check results, the ingester task handler and the ingester
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLocationStore, NewService, ProvideIngesterTaskHandler, ProvideIngester)
}

// ProvideLocationStore has the ingester keep a monitor's state on each probe in the monitor's own
// repository, where the monitor API reads it from
func ProvideLocationStore(repository monitor.MonitorRepository) LocationStore {
	return repository
}

// ProvideIngesterTaskHandler provides an ingester task handler
func ProvideIngesterTaskHandler(
	service Service,
	logger *zap.SugaredLogger,
) *IngesterTaskHandler {
	return NewIngesterTaskHandler(service, logger)
}

// ProvideIngester provides an ingester
func ProvideIngester(
	server *asynq.Server,
	handler *IngesterTaskHandler,
	logger *zap.SugaredLogger,
) *Ingester {
	return NewIngester(server, handler, logger)
}
