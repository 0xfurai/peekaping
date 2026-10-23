package shutdown

import (
	"context"

	"github.com/hibiken/asynq"
)

// RunningTasks counts the tasks the handlers of a queue server are running. The server's
// Shutdown waits for its tasks only until its ShutdownTimeout: it then puts them back in the
// queue, cancels their context and returns, without waiting for their handlers to return. Done
// is how a shutdown knows every handler has, and is done with what it is about to close.
type RunningTasks struct {
	inFlight
}

// Wrap returns the handler, counted as running a task until it returns
func (r *RunningTasks) Wrap(handler func(context.Context, *asynq.Task) error) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		r.started()
		defer r.returned()
		return handler(ctx, task)
	}
}
