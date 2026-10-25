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
	var (
		cancel   context.CancelFunc // ends the run of fn; nil while fn isn't running
		returned chan error         // gets what fn returned; nil while fn isn't running
		runTerm  uint64             // the term of leadership that fn was last started in
		done     bool               // fn returned nil in runTerm
	)
	// stop ends the run of fn and waits for it
	stop := func() {
		cancel()
		<-returned
		cancel, returned = nil, nil
	}

	for {
		select {
		case <-ctx.Done():
			if cancel != nil {
				e.logger.Infof("Context cancelled, stopping %s", name)
				stop()
			}
			return
		case err := <-returned:
			cancel()
			cancel, returned = nil, nil
			switch {
			case ctx.Err() != nil:
				// Stopping, which is why fn failed: the next turn of the loop returns
			case err != nil:
				e.logger.Errorw("Failed to run "+name, "error", err)
			default:
				done = true
			}
		case <-looks:
			leads, term := e.leadership()
			if cancel != nil && (!leads || term != runTerm) {
				e.logger.Infof("Lost leadership, stopping %s", name)
				stop()
			}
			if term != runTerm {
				done = false
			}
			if leads && cancel == nil && !done {
				e.logger.Infof("Starting %s as leader", name)
				var runCtx context.Context
				runCtx, cancel = context.WithCancel(ctx)
				runTerm = term
				result := make(chan error, 1)
				returned = result
				reporting.Go("leader", func() { result <- fn(runCtx) })
			}
		}
	}
}
