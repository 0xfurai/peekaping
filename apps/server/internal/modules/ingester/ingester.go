package ingester

import (
	"peekaping/internal/reporting"
	"peekaping/internal/shutdown"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Ingester manages task processing from the ingester queue
type Ingester struct {
	server  *asynq.Server
	mux     *asynq.ServeMux
	running shutdown.RunningTasks // the tasks the handler is running
	logger  *zap.SugaredLogger
}

// NewIngester creates a new ingester
func NewIngester(
	server *asynq.Server,
	handler *IngesterTaskHandler,
	logger *zap.SugaredLogger,
) *Ingester {
	mux := asynq.NewServeMux()
	mux.Use(reporting.AsynqMiddleware)

	i := &Ingester{
		server: server,
		mux:    mux,
		logger: logger.With("component", "ingester"),
	}
	// Registered here, not by Start: a mux panics on a second registration
	i.mux.HandleFunc(TaskTypeIngester, i.running.Wrap(handler.ProcessTask))
	return i
}

// Start starts the ingester. It takes no context: the asynq server runs until Stop, and its
// Shutdown is what waits for the running tasks.
func (i *Ingester) Start() error {
	i.logger.Info("Starting ingester")

	// Start the server with the mux
	if err := i.server.Start(i.mux); err != nil {
		return err
	}

	i.logger.Info("Ingester started successfully")
	return nil
}

// Stop stops the ingester gracefully: it takes no new task, and waits for the running ones to
// finish storing their heartbeats. The server's ShutdownTimeout bounds the wait. A task still
// running then goes back in the queue, to be ingested again, and its context is cancelled.
// Stop returns once its handler has.
func (i *Ingester) Stop() {
	i.logger.Info("Stopping ingester")
	i.server.Shutdown()
	<-i.running.Done()
	i.logger.Info("Ingester stopped")
}
