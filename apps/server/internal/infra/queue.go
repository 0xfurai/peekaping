package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/modules/queue"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// ProvideAsynqClient creates and returns an asynq.Client for enqueuing tasks
func ProvideAsynqClient(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*asynq.Client, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	client := asynq.NewClient(redisOpt)

	logger.Info("Successfully created Asynq client")
	return client, nil
}

// logFailedTask returns the error handler the asynq server uses: it logs a failed task's type,
// ID and error. It never logs the payload, which holds the event: a heartbeat, with the monitor's
// name and the check's message.
func logFailedTask(logger *zap.SugaredLogger) asynq.ErrorHandler {
	return asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		taskID, _ := asynq.GetTaskID(ctx)
		// asynq cancels a task it gives up on when the server stops, which is not a failure
		if errors.Is(err, context.Canceled) {
			logger.Infow("Task cancelled", "type", task.Type(), "task_id", taskID)
			return
		}
		logger.Errorw("Task processing failed",
			"type", task.Type(),
			"task_id", taskID,
			"error", err,
		)
	})
}

// ProvideNotificationAsynqServer creates the api's asynq.Server, which only processes tasks from
// the "notification" queue: the events the notification listener sends from, which cluster mode
// queues so that one api replica takes each (see notification_channel.QueuedBus).
func ProvideNotificationAsynqServer(cfg *config.Config, logger *zap.SugaredLogger) (*asynq.Server, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	serverCfg := asynq.Config{
		// A task only hands its event to the api's event bus, which queues it for the listener's
		// own workers and returns, so a task takes a moment and this is a ceiling the api never
		// reaches. The setting is the one the docs already list for the api.
		Concurrency: cfg.QueueConcurrency,
		Queues: map[string]int{
			"notification": 1, // Only process the notification queue
		},
		ErrorHandler:   logFailedTask(logger),
		StrictPriority: false, // We only have one queue, no need for strict priority
		Logger:         NewAsynqLogger(logger),
	}

	server := asynq.NewServer(redisOpt, serverCfg)

	logger.Info("Successfully created Asynq server for notifications")
	return server, nil
}

// AsynqLogger is an adapter to use zap logger with asynq
type AsynqLogger struct {
	logger *zap.SugaredLogger
}

// NewAsynqLogger creates a new asynq logger adapter
func NewAsynqLogger(logger *zap.SugaredLogger) *AsynqLogger {
	return &AsynqLogger{logger: logger}
}

func (l *AsynqLogger) Debug(args ...interface{}) {
	l.logger.Debug(args...)
}

func (l *AsynqLogger) Info(args ...interface{}) {
	l.logger.Info(args...)
}

func (l *AsynqLogger) Warn(args ...interface{}) {
	l.logger.Warn(args...)
}

func (l *AsynqLogger) Error(args ...interface{}) {
	l.logger.Error(args...)
}

func (l *AsynqLogger) Fatal(args ...interface{}) {
	l.logger.Fatal(args...)
}

// queueServiceImpl is the implementation of the queue.Service interface using asynq
type queueServiceImpl struct {
	client *asynq.Client
	logger *zap.SugaredLogger
}

// ProvideQueueService creates a new queue service using the asynq client
func ProvideQueueService(
	client *asynq.Client,
	logger *zap.SugaredLogger,
) queue.Service {
	return &queueServiceImpl{
		client: client,
		logger: logger.Named("[queue-service]"),
	}
}

// Enqueue adds a task to the queue
func (s *queueServiceImpl) Enqueue(_ context.Context, taskType string, payload interface{}, opts *queue.EnqueueOptions) error {
	if opts == nil {
		return fmt.Errorf("enqueue %s: no queue options", taskType)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		s.logger.Errorw("Failed to marshal payload", "task_type", taskType, "error", err)
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = s.client.Enqueue(asynq.NewTask(taskType, payloadBytes),
		asynq.Queue(opts.Queue),
		asynq.MaxRetry(opts.MaxRetry),
		asynq.Timeout(opts.Timeout),
	)
	if err != nil {
		s.logger.Errorw("Failed to enqueue task", "task_type", taskType, "error", err)
		return fmt.Errorf("failed to enqueue task: %w", err)
	}
	return nil
}
