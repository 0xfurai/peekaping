package ingester

import (
	"peekaping/internal/utils"

	"github.com/hibiken/asynq"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers ingester dependencies in the DI container: the service that
// stores check results, the ingester task handler and the ingester
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, NewService, ProvideIngesterTaskHandler, ProvideIngester)
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
