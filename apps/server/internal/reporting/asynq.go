package reporting

import (
	"context"
	"runtime/debug"

	"github.com/hibiken/asynq"
)

// AsynqMiddleware reports a panic in a task handler, with its stack. Use it on the ServeMux of
// every asynq server: mux.Use(reporting.AsynqMiddleware).
//
// asynq recovers a handler's panic itself, but it turns it into an error that holds a message
// ("panic [file:line]: value") and nothing else: the value's type and the stack are gone by the
// time the server's ErrorHandler gets it. So the report is made here, inside asynq's recover,
// and the panic then goes on to asynq unchanged: it recovers it, fails the task and retries it as
// before, and the stack it logs still holds the handler's frames.
func AsynqMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		returned := false
		defer func() {
			// While reporting is off the panic is not recovered here, so asynq sees it as it
			// would without the middleware
			if returned || !Enabled() {
				return
			}
			value := recover()
			if value == nil {
				return // runtime.Goexit
			}
			CapturePanic("asynq", value, debug.Stack())
			panic(value)
		}()
		err := next.ProcessTask(ctx, task)
		returned = true
		return err
	})
}
