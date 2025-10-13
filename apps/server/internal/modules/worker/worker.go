package worker

import (
	"context"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Worker manages task processing from the queue
type Worker struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	healthCheck *HealthCheckTaskHandler
	logger      *zap.SugaredLogger
}

// NewWorker creates a new worker
func NewWorker(
	server *asynq.Server,
	healthCheck *HealthCheckTaskHandler,
	logger *zap.SugaredLogger,
) *Worker {
	mux := asynq.NewServeMux()

	return &Worker{
		server:      server,
		mux:         mux,
		healthCheck: healthCheck,
		logger:      logger.With("component", "worker"),
	}
}

// Start starts the worker
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("Starting worker")

	// Register task handlers
	w.mux.HandleFunc(TaskTypeHealthCheck, w.healthCheck.ProcessTask)

	// Start the server with the mux
	if err := w.server.Start(w.mux); err != nil {
		return err
	}

	w.logger.Info("Worker started successfully")
	return nil
}

// Stop stops the worker gracefully
func (w *Worker) Stop() {
	w.logger.Info("Stopping worker")
	w.server.Shutdown()
	w.logger.Info("Worker stopped")
}
