package queue

import (
	"context"
	"errors"
	"time"
)

// ErrTaskIDConflict is returned by Enqueue when a task with the same TaskID
// already exists in the queue, in any state (including archived). EnqueueUnique
// returns it when the task's unique key is held already, or an identical task is.
var ErrTaskIDConflict = errors.New("task ID conflicts with another task")

// ErrTaskNotFound is returned when the requested task doesn't exist
var ErrTaskNotFound = errors.New("task not found")

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
