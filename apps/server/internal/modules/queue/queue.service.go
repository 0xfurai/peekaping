package queue

import (
	"context"
	"time"
)

// Service provides an abstraction layer for queue operations
type Service interface {
	// Enqueue adds a task to the queue
	Enqueue(ctx context.Context, taskType string, payload interface{}, opts *EnqueueOptions) (*TaskInfo, error)

	// EnqueueUnique adds a task to the queue with deduplication
	EnqueueUnique(ctx context.Context, taskType string, payload interface{}, uniqueKey string, ttl time.Duration, opts *EnqueueOptions) (*TaskInfo, error)

	// GetQueueInfo returns information about a specific queue
	GetQueueInfo(ctx context.Context, queueName string) (*QueueInfo, error)

	// ListQueues returns a list of all queues
	ListQueues(ctx context.Context) ([]*QueueInfo, error)

	// GetTaskInfo returns information about a specific task
	GetTaskInfo(ctx context.Context, queueName, taskID string) (*TaskInfo, error)

	// DeleteTask deletes a task from the queue
	DeleteTask(ctx context.Context, queueName, taskID string) error

	// CancelTask cancels the processing of a task
	CancelTask(ctx context.Context, taskID string) error

	// PauseQueue pauses task processing for a specific queue
	PauseQueue(ctx context.Context, queueName string) error

	// UnpauseQueue resumes task processing for a specific queue
	UnpauseQueue(ctx context.Context, queueName string) error

	// ListPendingTasks returns a list of pending tasks in a queue
	ListPendingTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*TaskInfo, error)

	// ListActiveTasks returns a list of active tasks in a queue
	ListActiveTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*TaskInfo, error)

	// ListScheduledTasks returns a list of scheduled tasks in a queue
	ListScheduledTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*TaskInfo, error)

	// Close closes the queue connections
	Close() error
}
