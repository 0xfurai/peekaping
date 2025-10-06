package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/modules/queue"
	"strings"
	"time"

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
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	client := asynq.NewClient(redisOpt)

	logger.Info("Successfully created Asynq client")
	return client, nil
}

// ProvideAsynqServer creates and returns an asynq.Server for processing tasks
func ProvideAsynqServer(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*asynq.Server, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	// Configure server with appropriate concurrency and queue priorities
	// Note: Worker only processes healthcheck tasks. Ingester tasks are handled by a separate ingester service.
	serverCfg := asynq.Config{
		// Number of concurrent workers to process tasks
		Concurrency: cfg.QueueConcurrency,

		// Queue priorities - higher value means higher priority
		Queues: map[string]int{
			"critical":    6, // Highest priority
			"healthcheck": 5, // High priority for health checks
			"default":     3, // Medium priority
			"low":         1, // Lowest priority
		},

		// Error handler for logging failed tasks
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			logger.Errorw("Task processing failed",
				"type", task.Type(),
				"payload", string(task.Payload()),
				"error", err,
			)
		}),

		// Enable strict priority mode
		StrictPriority: true,

		// Logger adapter
		Logger: NewAsynqLogger(logger),
	}

	server := asynq.NewServer(redisOpt, serverCfg)

	logger.Info("Successfully created Asynq server")
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

// ProvideAsynqInspector creates and returns an asynq.Inspector for inspecting tasks and queues
func ProvideAsynqInspector(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*asynq.Inspector, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	inspector := asynq.NewInspector(redisOpt)

	logger.Info("Successfully created Asynq inspector")
	return inspector, nil
}

// ProvideAsynqScheduler creates and returns an asynq.Scheduler for scheduling periodic tasks
func ProvideAsynqScheduler(
	cfg *config.Config,
	logger *zap.SugaredLogger,
) (*asynq.Scheduler, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	// Create scheduler with location for cron expressions
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Warnw("Failed to load timezone, using UTC", "timezone", cfg.Timezone, "error", err)
		location = time.UTC
	}

	schedulerCfg := &asynq.SchedulerOpts{
		Location: location,
		Logger:   NewAsynqLogger(logger),
		// EnqueueErrorHandler is called when there's an error enqueuing a task
		EnqueueErrorHandler: func(task *asynq.Task, opts []asynq.Option, err error) {
			logger.Errorw("Failed to enqueue scheduled task",
				"type", task.Type(),
				"error", err,
			)
		},
	}

	scheduler := asynq.NewScheduler(redisOpt, schedulerCfg)

	logger.Info("Successfully created Asynq scheduler")
	return scheduler, nil
}

// queueServiceImpl is the implementation of the queue.Service interface using asynq
type queueServiceImpl struct {
	client    *asynq.Client
	inspector *asynq.Inspector
	logger    *zap.SugaredLogger
}

// ProvideQueueService creates a new queue service using asynq components
func ProvideQueueService(
	client *asynq.Client,
	inspector *asynq.Inspector,
	logger *zap.SugaredLogger,
) queue.Service {
	return &queueServiceImpl{
		client:    client,
		inspector: inspector,
		logger:    logger.Named("[queue-service]"),
	}
}

// Enqueue adds a task to the queue
func (s *queueServiceImpl) Enqueue(ctx context.Context, taskType string, payload interface{}, opts *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	if opts == nil {
		opts = queue.DefaultEnqueueOptions()
	}

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		s.logger.Errorw("Failed to marshal payload", "task_type", taskType, "error", err)
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the task
	task := asynq.NewTask(taskType, payloadBytes)

	// Build options
	asynqOpts := buildAsynqOptions(opts)

	// Enqueue the task
	var info *asynq.TaskInfo
	if opts.ProcessAt != nil {
		info, err = s.client.Enqueue(task, append(asynqOpts, asynq.ProcessAt(*opts.ProcessAt))...)
	} else if opts.ProcessIn != nil {
		info, err = s.client.Enqueue(task, append(asynqOpts, asynq.ProcessIn(*opts.ProcessIn))...)
	} else {
		info, err = s.client.Enqueue(task, asynqOpts...)
	}

	if err != nil {
		s.logger.Errorw("Failed to enqueue task", "task_type", taskType, "error", err)
		return nil, fmt.Errorf("failed to enqueue task: %w", err)
	}

	// s.logger.Infow("Task enqueued successfully",
	// 	"task_type", taskType,
	// 	"task_id", info.ID,
	// 	"queue", info.Queue,
	// )

	return convertTaskInfo(info), nil
}

// EnqueueUnique adds a task to the queue with deduplication
func (s *queueServiceImpl) EnqueueUnique(ctx context.Context, taskType string, payload interface{}, uniqueKey string, ttl time.Duration, opts *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	if opts == nil {
		opts = queue.DefaultEnqueueOptions()
	}

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		s.logger.Errorw("Failed to marshal payload", "task_type", taskType, "error", err)
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the task
	task := asynq.NewTask(taskType, payloadBytes)

	// Build options with uniqueness
	asynqOpts := buildAsynqOptions(opts)
	asynqOpts = append(asynqOpts, asynq.Unique(ttl))

	if opts.TaskID == "" {
		opts.TaskID = uniqueKey
	}
	asynqOpts = append(asynqOpts, asynq.TaskID(opts.TaskID))

	// Enqueue the task
	var info *asynq.TaskInfo
	if opts.ProcessAt != nil {
		info, err = s.client.Enqueue(task, append(asynqOpts, asynq.ProcessAt(*opts.ProcessAt))...)
	} else if opts.ProcessIn != nil {
		info, err = s.client.Enqueue(task, append(asynqOpts, asynq.ProcessIn(*opts.ProcessIn))...)
	} else {
		info, err = s.client.Enqueue(task, asynqOpts...)
	}

	if err != nil {
		// Check if this is a duplicate task error (expected behavior with unique constraint)
		errMsg := err.Error()
		if strings.Contains(errMsg, "task ID conflicts") ||
			strings.Contains(errMsg, "duplicated") ||
			strings.Contains(errMsg, "already exists") {
			// This is expected behavior - task is already queued (deduplication working)
			// Log at debug level instead of error, but still return error for caller to handle
			s.logger.Debugw("Task already queued (duplicate prevented by unique constraint)",
				"task_type", taskType,
				"unique_key", uniqueKey)
			return nil, fmt.Errorf("task already exists: %w", err)
		}

		// This is a real error - log at error level
		s.logger.Errorw("Failed to enqueue unique task", "task_type", taskType, "unique_key", uniqueKey, "error", err)
		return nil, fmt.Errorf("failed to enqueue unique task: %w", err)
	}

	// s.logger.Infow("Unique task enqueued successfully",
	// 	"task_type", taskType,
	// 	"task_id", info.ID,
	// 	"queue", info.Queue,
	// 	"unique_key", uniqueKey,
	// )

	return convertTaskInfo(info), nil
}

// GetQueueInfo returns information about a specific queue
func (s *queueServiceImpl) GetQueueInfo(ctx context.Context, queueName string) (*queue.QueueInfo, error) {
	info, err := s.inspector.GetQueueInfo(queueName)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue info: %w", err)
	}
	return convertQueueInfo(info), nil
}

// ListQueues returns a list of all queues
func (s *queueServiceImpl) ListQueues(ctx context.Context) ([]*queue.QueueInfo, error) {
	queues, err := s.inspector.Queues()
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}

	var queueInfos []*queue.QueueInfo
	for _, queueName := range queues {
		info, err := s.inspector.GetQueueInfo(queueName)
		if err != nil {
			s.logger.Warnw("Failed to get queue info", "queue", queueName, "error", err)
			continue
		}
		queueInfos = append(queueInfos, convertQueueInfo(info))
	}

	return queueInfos, nil
}

// GetTaskInfo returns information about a specific task
func (s *queueServiceImpl) GetTaskInfo(ctx context.Context, queueName, taskID string) (*queue.TaskInfo, error) {
	info, err := s.inspector.GetTaskInfo(queueName, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task info: %w", err)
	}
	return convertTaskInfo(info), nil
}

// DeleteTask deletes a task from the queue
func (s *queueServiceImpl) DeleteTask(ctx context.Context, queueName, taskID string) error {
	err := s.inspector.DeleteTask(queueName, taskID)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	s.logger.Infow("Task deleted", "queue", queueName, "task_id", taskID)
	return nil
}

// CancelTask cancels the processing of a task
func (s *queueServiceImpl) CancelTask(ctx context.Context, taskID string) error {
	err := s.inspector.CancelProcessing(taskID)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	s.logger.Infow("Task cancelled", "task_id", taskID)
	return nil
}

// PauseQueue pauses task processing for a specific queue
func (s *queueServiceImpl) PauseQueue(ctx context.Context, queueName string) error {
	err := s.inspector.PauseQueue(queueName)
	if err != nil {
		return fmt.Errorf("failed to pause queue: %w", err)
	}
	s.logger.Infow("Queue paused", "queue", queueName)
	return nil
}

// UnpauseQueue resumes task processing for a specific queue
func (s *queueServiceImpl) UnpauseQueue(ctx context.Context, queueName string) error {
	err := s.inspector.UnpauseQueue(queueName)
	if err != nil {
		return fmt.Errorf("failed to unpause queue: %w", err)
	}
	s.logger.Infow("Queue unpaused", "queue", queueName)
	return nil
}

// ListPendingTasks returns a list of pending tasks in a queue
func (s *queueServiceImpl) ListPendingTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	tasks, err := s.inspector.ListPendingTasks(queueName, asynq.PageSize(pageSize), asynq.Page(pageNum))
	if err != nil {
		return nil, fmt.Errorf("failed to list pending tasks: %w", err)
	}
	return convertTaskInfoList(tasks), nil
}

// ListActiveTasks returns a list of active tasks in a queue
func (s *queueServiceImpl) ListActiveTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	tasks, err := s.inspector.ListActiveTasks(queueName, asynq.PageSize(pageSize), asynq.Page(pageNum))
	if err != nil {
		return nil, fmt.Errorf("failed to list active tasks: %w", err)
	}
	return convertTaskInfoList(tasks), nil
}

// ListScheduledTasks returns a list of scheduled tasks in a queue
func (s *queueServiceImpl) ListScheduledTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	tasks, err := s.inspector.ListScheduledTasks(queueName, asynq.PageSize(pageSize), asynq.Page(pageNum))
	if err != nil {
		return nil, fmt.Errorf("failed to list scheduled tasks: %w", err)
	}
	return convertTaskInfoList(tasks), nil
}

// Close closes the queue connections
func (s *queueServiceImpl) Close() error {
	if err := s.client.Close(); err != nil {
		return err
	}
	if err := s.inspector.Close(); err != nil {
		return err
	}
	s.logger.Info("Queue service closed")
	return nil
}

// Helper functions

func buildAsynqOptions(opts *queue.EnqueueOptions) []asynq.Option {
	asynqOpts := []asynq.Option{
		asynq.Queue(opts.Queue),
		asynq.MaxRetry(opts.MaxRetry),
		asynq.Timeout(opts.Timeout),
		asynq.Retention(opts.Retention),
	}

	if opts.TaskID != "" {
		asynqOpts = append(asynqOpts, asynq.TaskID(opts.TaskID))
	}

	return asynqOpts
}

func convertTaskInfo(info *asynq.TaskInfo) *queue.TaskInfo {
	if info == nil {
		return nil
	}

	return &queue.TaskInfo{
		ID:            info.ID,
		Queue:         info.Queue,
		Type:          info.Type,
		Payload:       info.Payload,
		State:         info.State.String(),
		MaxRetry:      info.MaxRetry,
		Retried:       info.Retried,
		LastErr:       info.LastErr,
		LastFailedAt:  info.LastFailedAt,
		NextProcessAt: info.NextProcessAt,
	}
}

func convertTaskInfoList(infos []*asynq.TaskInfo) []*queue.TaskInfo {
	result := make([]*queue.TaskInfo, 0, len(infos))
	for _, info := range infos {
		result = append(result, convertTaskInfo(info))
	}
	return result
}

func convertQueueInfo(info *asynq.QueueInfo) *queue.QueueInfo {
	if info == nil {
		return nil
	}

	return &queue.QueueInfo{
		Queue:     info.Queue,
		Size:      info.Size,
		Pending:   info.Pending,
		Active:    info.Active,
		Scheduled: info.Scheduled,
		Retry:     info.Retry,
		Archived:  info.Archived,
		Completed: info.Completed,
		Processed: info.Processed,
		Failed:    info.Failed,
		Paused:    info.Paused,
		Timestamp: info.Timestamp,
	}
}
