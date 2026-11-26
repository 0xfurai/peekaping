package leader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// A node that loses the leadership and gets it back between two looks of RunWhileLeader leads in
// a new term, and RunWhileLeader has to tell, though at neither look the node wasn't the leader.
// These tests take the timing away: the node's leadership changes by hand, and RunWhileLeader
// looks only when the test sends it a look.
type gapTest struct {
	*Elector
	logs  *observer.ObservedLogs
	looks chan time.Time
	job   *job
}

// startGapTest runs the job on a node that leads, and waits until the job has started
func startGapTest(t *testing.T, fn func(*job) func(context.Context) error) *gapTest {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	g := &gapTest{
		Elector: New(nil, "node1", zap.New(core).Sugar()),
		logs:    logs,
		looks:   make(chan time.Time),
		job:     &job{},
	}
	g.setLeaderStatus(true)

	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		g.runWhileLeader(ctx, "the job", g.looks, fn(g.job))
	}()
	t.Cleanup(func() {
		cancel()
		waitReturned(t, returned)
	})

	g.look(t)
	require.Eventually(t, func() bool { return g.job.started.Load() == 1 }, time.Second, time.Millisecond, "the leader never started the job")
	return g
}

// look makes RunWhileLeader look once. RunWhileLeader has taken its look at the node's leadership
// when the look is received and the effects of what it saw are seen, so the test can change the
// leadership again once it has seen them.
func (g *gapTest) look(t *testing.T) {
	t.Helper()
	select {
	case g.looks <- time.Now():
	case <-time.After(5 * time.Second):
		require.FailNow(t, "RunWhileLeader didn't look")
	}
}

// loseAndRegain is the node losing the leadership and getting it back, with no look in between
func (g *gapTest) loseAndRegain() {
	g.setLeaderStatus(false)
	g.setLeaderStatus(true)
}

func TestRunWhileLeader_RunsAJobThatIsDoneAgainWhenTheNodeLostTheLeadershipAndGotItBackBetweenTwoLooks(t *testing.T) {
	g := startGapTest(t, func(j *job) func(context.Context) error {
		return func(context.Context) error {
			j.started.Add(1)
			return nil
		}
	})

	g.loseAndRegain()
	g.look(t)

	require.Eventually(t, func() bool { return g.job.started.Load() == 2 }, time.Second, time.Millisecond, "the job didn't run again for the new term")
}

func TestRunWhileLeader_CancelsAJobThatRunsWhenTheNodeLostTheLeadershipAndGotItBackBetweenTwoLooks(t *testing.T) {
	g := startGapTest(t, func(j *job) func(context.Context) error { return j.run })
	require.Eventually(t, func() bool { return g.job.running.Load() == 1 }, time.Second, time.Millisecond)

	g.loseAndRegain()
	g.look(t)

	require.Eventually(t, func() bool { return g.job.started.Load() == 2 && g.job.running.Load() == 1 }, time.Second, time.Millisecond, "the job didn't run again for the new term")
	assert.Equal(t, int32(1), g.job.ended.Load(), "the job of the lost term wasn't cancelled")
	assert.Equal(t, int32(1), g.job.stopped.Load(), "the job was started again before it returned")
	assert.False(t, g.job.overlap.Load(), "two runs of the job overlapped")
	assert.NotZero(t, g.logs.FilterMessage("Lost leadership, stopping the job").Len())
}
