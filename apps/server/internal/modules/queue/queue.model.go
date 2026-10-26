package queue

import (
	"time"
)

// EnqueueOptions contains options for enqueuing a task
type EnqueueOptions struct {
	// Queue name
	Queue string

	// MaxRetry is the maximum number of times the task will be retried
	MaxRetry int

	// Timeout is the duration the task can run before being cancelled
	Timeout time.Duration
}
