package ingester

import (
	"context"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Ingester manages task processing from the ingester queue
type Ingester struct {
	server  *asynq.Server
	mux     *asynq.ServeMux
	handler *IngesterTaskHandler
	logger  *zap.SugaredLogger
}

// NewIngester creates a new ingester
func NewIngester(
	server *asynq.Server,
	handler *IngesterTaskHandler,
	logger *zap.SugaredLogger,
) *Ingester {
	mux := asynq.NewServeMux()

	return &Ingester{
		server:  server,
		mux:     mux,
		handler: handler,
		logger:  logger.With("component", "ingester"),
	}
}

// Start starts the ingester
func (i *Ingester) Start(ctx context.Context) error {
	i.logger.Info("Starting ingester")

	// Register task handlers
	i.mux.HandleFunc(TaskTypeIngester, i.handler.ProcessTask)

	// Start the server with the mux
	if err := i.server.Start(i.mux); err != nil {
		return err
	}

	i.logger.Info("Ingester started successfully")
	return nil
}

// Stop stops the ingester gracefully
func (i *Ingester) Stop() {
	i.logger.Info("Stopping ingester")
	i.server.Shutdown()
	i.logger.Info("Ingester stopped")
}
