package worker

import (
	"peekaping/internal/shutdown"

	"peekaping/internal/reporting"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Worker manages task processing from the queue
type Worker struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	healthCheck *HealthCheckTaskHandler
	running     shutdown.RunningTasks // the tasks the handler is running
	logger      *zap.SugaredLogger
}

// NewWorker creates a new worker
func NewWorker(
	server *asynq.Server,
	healthCheck *HealthCheckTaskHandler,
	logger *zap.SugaredLogger,
) *Worker {
	mux := asynq.NewServeMux()
	mux.Use(reporting.AsynqMiddleware)

	return &Worker{
		server:      server,
		mux:         mux,
		healthCheck: healthCheck,
		logger:      logger.With("component", "worker"),
	}
}

// Start starts the worker. It takes no context: the asynq server runs until Stop, and its
// Shutdown is what waits for the running tasks.
func (w *Worker) Start() error {
	w.logger.Info("Starting worker")

	// Register task handlers
	w.mux.HandleFunc(TaskTypeHealthCheck, w.running.Wrap(w.healthCheck.ProcessTask))

	// Start the server with the mux
	if err := w.server.Start(w.mux); err != nil {
		return err
	}

	w.logger.Info("Worker started successfully")
	return nil
}

// Stop stops the worker gracefully: it takes no new task, and waits for the running ones to
// finish and enqueue their results. The server's ShutdownTimeout bounds the wait. A task still
// running then goes back in the queue, for another worker to run again, and its context is
// cancelled, which ends its check. Stop returns once its handler has.
func (w *Worker) Stop() {
	w.logger.Info("Stopping worker")
	w.server.Shutdown()
	<-w.running.Done()
	w.logger.Info("Worker stopped")
}
