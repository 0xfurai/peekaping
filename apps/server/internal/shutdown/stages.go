package shutdown

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// queueMargin is what a queue server needs, once it gives up on its running tasks, to stop the
// rest of itself and for the handlers it cancelled to return
const queueMargin = 3 * time.Second

// QueueShutdown is how long a queue server (asynq) waits for its running tasks: the jobs' share
// less queueMargin, or half of a share too short for that. Set as the server's ShutdownTimeout,
// it makes the server give up before the share ends, so that the shutdown goes on instead of
// running out of time on it. A task it gives up on goes back to the queue, to be run again.
func (t Timeouts) QueueShutdown() time.Duration {
	return max(t.Jobs-queueMargin, t.Jobs/2)
}

// Stop shuts down a binary that serves no HTTP, in order, each step within its share of
// timeouts. stopJobs stops what the binary runs, and returns once it has stopped: jobs names it,
// as in "the producer". The event bus lets the handlers of the events queued finish. The database
// closes last, and only once nothing uses it: when a job or an event handler is still running at
// the end of its share, the database stays open for the process's exit to release, since closing
// it would fail the writes in progress halfway through. A binary without a database passes nil
// for closeDatabase, which gets what is left of the event bus's share.
func Stop(timeouts Timeouts, jobs string, stopJobs func(), bus interface{ Close(context.Context) error }, closeDatabase func(ctx context.Context) error, logger *zap.SugaredLogger) error {
	var errs []error
	// What uses the database and still runs once its share is up
	var running []string

	jobsCtx, cancelJobs := context.WithTimeout(context.Background(), timeouts.Jobs)
	defer cancelJobs()
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		stopJobs()
	}()
	if err := Await(jobsCtx, jobs, jobsDone); err != nil {
		errs = append(errs, err)
		running = append(running, jobs)
	} else {
		logger.Infof("Stopped %s", jobs)
	}

	busCtx, cancelBus := context.WithTimeout(context.Background(), timeouts.EventBus)
	defer cancelBus()
	if err := bus.Close(busCtx); err != nil {
		errs = append(errs, fmt.Errorf("failed to close the event bus: %w", err))
		running = append(running, "event handlers")
	} else {
		logger.Info("Closed the event bus")
	}

	if closeDatabase == nil {
		return errors.Join(errs...)
	}
	if len(running) > 0 {
		logger.Errorw("Left the database open, since some of what uses it is still running", "running", running)
		return errors.Join(errs...)
	}
	if err := closeDatabase(busCtx); err != nil {
		errs = append(errs, err)
	} else {
		logger.Info("Closed the database")
	}
	return errors.Join(errs...)
}
