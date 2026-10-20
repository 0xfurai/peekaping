package check

import (
	"context"
	"time"
)

// Runner runs the checks the producer schedules: ADR 0002's "run a check". Cluster mode's
// runner (producer.NewQueueRunner) enqueues each check as a monitor:healthcheck task for the
// worker, under a task ID fixed per monitor.
//
// Single mode has no producer. The scheduler's timers start each check in the scheduler's own
// pool, which keeps the same two rules: a check that falls due while the monitor's previous
// one is still running is skipped, and an update, pause or delete cancels the running check
// and drops its result.
type Runner interface {
	// Run starts the job's check, which was due at scheduledAt. A monitor runs one check at a
	// time: while its previous check hasn't ended, Run skips this one and reports false.
	Run(ctx context.Context, job *Job, scheduledAt time.Time) (bool, error)
	// Cancel drops the monitor's check that hasn't started and cancels the one running, whose
	// result is dropped, so that its next check runs with the monitor's current settings
	Cancel(ctx context.Context, monitorID string)
}
