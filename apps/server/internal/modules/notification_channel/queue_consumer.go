package notification_channel

import (
	"context"
	"encoding/json"
	"fmt"

	"peekaping/internal/modules/events"
	"peekaping/internal/reporting/asynqmw"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// QueueConsumer takes the notification events a QueuedBus enqueued off the notification queue,
// one task per api replica, and hands each to the api's own event bus. From there the
// notification listener handles it as it would an event off Redis pub/sub: in the order of its
// monitor's events, within its worker limit, with its send timeouts and panic recovery.
type QueueConsumer struct {
	server *asynq.Server
	bus    *QueuedBus
	logger *zap.SugaredLogger
}

// NewQueueConsumer returns the consumer that serves the notification queue on server and
// feeds bus.
func NewQueueConsumer(server *asynq.Server, bus *QueuedBus, logger *zap.SugaredLogger) *QueueConsumer {
	return &QueueConsumer{server: server, bus: bus, logger: logger.With("component", "notification_queue")}
}

// Start starts taking tasks. Subscribe the notification listener first: a task handed to a bus
// with no handler for its topic is dropped, and it has already left the queue.
func (c *QueueConsumer) Start() error {
	c.logger.Info("Starting to take notifications from the queue")
	mux := asynq.NewServeMux()
	mux.Use(asynqmw.ReportPanics)
	mux.HandleFunc(TaskTypeNotification, c.ProcessTask)
	return c.server.Start(mux)
}

// Stop stops taking tasks and waits for the ones in progress, which only hand over their
// event. Stop it before closing the event bus, so no task finds the bus closed.
func (c *QueueConsumer) Stop() {
	c.logger.Info("Stopping the notification queue")
	c.server.Shutdown()
}

// ProcessTask implements asynq.HandlerFunc. It fails, and so queues the task for another try,
// only when the event bus refuses the event because it is closing.
func (c *QueueConsumer) ProcessTask(_ context.Context, task *asynq.Task) error {
	var event queuedEvent
	// A payload that doesn't make sense fails the same way on every try, so it is not retried
	if err := json.Unmarshal(task.Payload(), &event); err != nil {
		return fmt.Errorf("unmarshal task payload: %w: %w", err, asynq.SkipRetry)
	}
	if event.Topic == "" || len(event.Payload) == 0 {
		return fmt.Errorf("task payload has no topic or event: %w", asynq.SkipRetry)
	}
	if err := c.bus.Receive(events.Message{Topic: event.Topic, JSON: event.Payload}); err != nil {
		return fmt.Errorf("hand %s to the event bus: %w", event.Topic, err)
	}
	return nil
}
