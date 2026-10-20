package ingester

import (
	"github.com/hibiken/asynq"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers ingester dependencies in the DI container
func RegisterDependencies(container *dig.Container) {
	// Provide the service that stores check results
	container.Provide(NewService)

	// Provide ingester task handler
	container.Provide(ProvideIngesterTaskHandler)

	// Provide ingester
	container.Provide(ProvideIngester)
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
