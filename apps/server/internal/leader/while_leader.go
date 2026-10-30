package leader

import (
	"context"
	"time"

	"peekaping/internal/reporting"
)

// RunWhileLeader runs a job only while this node is the leader, until ctx is done. It looks at
// the node's leadership every `every`. When the node has become the leader it starts fn in a
// goroutine, with a context that it cancels when the node loses the leadership or ctx is done.
// Then it waits for fn to return before it looks again, so the job never runs twice at once, and
// it ends only once fn has returned: fn has to return when its context is done. name is what the
// log calls the job.
//
// A node that lost the leadership and got it back between two looks has lost it all the same: it
// leads in a new term, which RunWhileLeader tells from the term the job started in. So the job of
// the lost term is cancelled and started again, though no look fell in the gap.
//
// A job that returns an error is logged and started again at the next look, if the node still
// leads in the term the job started in. One that returns nil is done: it is not started again
// until the node has lost the leadership and got it back, in a new term. A job that runs until
// its context is done returns nil.
func (e *Elector) RunWhileLeader(ctx context.Context, name string, every time.Duration, fn func(ctx context.Context) error) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	e.runWhileLeader(ctx, name, ticker.C, fn)
}

// runWhileLeader is RunWhileLeader with a look at each value that comes from looks, which a test
// sends when it wants one
func (e *Elector) runWhileLeader(ctx context.Context, name string, looks <-chan time.Time, fn func(ctx context.Context) error) {
	job := &leaderJob{elector: e, name: name, fn: fn}

	for {
		select {
		case <-ctx.Done():
			job.onContextDone()
			return
		case err := <-job.returned:
			job.onReturned(ctx, err)
		case <-looks:
			job.onLook(ctx)
		}
	}
}

// leaderJob is the state of the job runWhileLeader runs
type leaderJob struct {
	elector *Elector
	name    string
	fn      func(ctx context.Context) error

	cancel   context.CancelFunc // ends the run of fn; nil while fn isn't running
	returned chan error         // gets what fn returned; nil while fn isn't running
	runTerm  uint64             // the term of leadership that fn was last started in
	done     bool               // fn returned nil in runTerm
}

// stop ends the run of fn and waits for it
func (j *leaderJob) stop() {
	j.cancel()
	<-j.returned
	j.cancel, j.returned = nil, nil
}

// onContextDone ends the run of fn, if there is one, when the caller's context is done
func (j *leaderJob) onContextDone() {
	if j.cancel != nil {
		j.elector.logger.Infof("Context cancelled, stopping %s", j.name)
		j.stop()
	}
}

// onReturned takes what fn returned: an error is logged, and nil makes the job done in its term
func (j *leaderJob) onReturned(ctx context.Context, err error) {
	j.cancel()
	j.cancel, j.returned = nil, nil
	switch {
	case ctx.Err() != nil:
		// Stopping, which is why fn failed: the next turn of the loop returns
	case err != nil:
		j.elector.logger.Errorw("Failed to run "+j.name, "error", err)
	default:
		j.done = true
	}
}

// onLook looks at the node's leadership: it ends a run of fn that the node no longer leads for, and
// starts fn when the node leads and fn isn't running or done
func (j *leaderJob) onLook(ctx context.Context) {
	leads, term := j.elector.leadership()
	if j.lostLeadership(leads, term) {
		j.elector.logger.Infof("Lost leadership, stopping %s", j.name)
		j.stop()
	}
	if term != j.runTerm {
		j.done = false
	}
	if leads && j.cancel == nil && !j.done {
		j.start(ctx, term)
	}
}

// lostLeadership is whether fn runs for a term of leadership that is over
func (j *leaderJob) lostLeadership(leads bool, term uint64) bool {
	return j.cancel != nil && (!leads || term != j.runTerm)
}

// start runs fn in a goroutine, for the term, with a context that ends the run
func (j *leaderJob) start(ctx context.Context, term uint64) {
	j.elector.logger.Infof("Starting %s as leader", j.name)
	var runCtx context.Context
	runCtx, j.cancel = context.WithCancel(ctx)
	j.runTerm = term
	result := make(chan error, 1)
	j.returned = result
	reporting.Go("leader", func() { result <- j.fn(runCtx) })
}
