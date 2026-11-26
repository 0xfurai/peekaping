// Package queue is the job queue the api sends the notification events through in cluster mode
// (see notification_channel.QueuedBus). The queue is asynq on Redis, in package infra.
package queue

import "context"

// Service puts tasks on the queue
type Service interface {
	// Enqueue adds a task to the queue: payload is encoded as JSON and opts says which queue
	Enqueue(ctx context.Context, taskType string, payload interface{}, opts *EnqueueOptions) error
}
