package queue

import (
	"time"
)

// EnqueueOptions contains options for enqueuing a task
type EnqueueOptions struct {
	// Queue name (e.g., "critical", "default", "low")
	Queue string

	// MaxRetry is the maximum number of times the task will be retried
	MaxRetry int

	// Timeout is the duration the task can run before being cancelled
	Timeout time.Duration

	// ProcessAt specifies when the task should be processed (for scheduled tasks)
	ProcessAt *time.Time

	// ProcessIn specifies the delay before the task should be processed
	ProcessIn *time.Duration

	// Retention is how long to keep the task in the queue after completion
	Retention time.Duration

	// TaskID is a unique identifier for the task (for deduplication)
	TaskID string

	// Deadline specifies when the task should expire (task will be discarded if not processed by this time)
	Deadline *time.Time
}

// DefaultEnqueueOptions returns default options for enqueuing
func DefaultEnqueueOptions() *EnqueueOptions {
	return &EnqueueOptions{
		Queue:     "default",
		MaxRetry:  3,
		Timeout:   5 * time.Minute,
		Retention: 24 * time.Hour,
	}
}

// TaskInfo represents information about a task
type TaskInfo struct {
	ID            string
	Queue         string
	Type          string
	Payload       []byte
	State         string
	MaxRetry      int
	Retried       int
	LastErr       string
	LastFailedAt  time.Time
	NextProcessAt time.Time
}

// QueueInfo represents information about a queue
type QueueInfo struct {
	Queue     string
	Size      int
	Pending   int
	Active    int
	Scheduled int
	Retry     int
	Archived  int
	Completed int
	Processed int
	Failed    int
	Paused    bool
	Timestamp time.Time
}
