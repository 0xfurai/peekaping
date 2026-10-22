package shutdown_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest/observer"
)

// recordedSteps is what Stop's collaborators record, to show the order they ran in
type recordedSteps struct{ steps []string }

func (r *recordedSteps) add(step string) { r.steps = append(r.steps, step) }

// fakeBus is an event bus that records its Close, and fails it with err
type fakeBus struct {
	recorded *recordedSteps
	err      error
}

func (b fakeBus) Close(context.Context) error {
	b.recorded.add("close the event bus")
	return b.err
}

// messages are the messages logged, in order
func messages(logs *observer.ObservedLogs) []string {
	var got []string
	for _, entry := range logs.All() {
		got = append(got, entry.Message)
	}
	return got
}

var quickTimeouts = shutdown.Timeouts{Jobs: 200 * time.Millisecond, EventBus: 200 * time.Millisecond}

func TestStop_StopsTheJobsThenClosesTheEventBusThenTheDatabase(t *testing.T) {
	logger, logs := observedLogger()
	recorded := &recordedSteps{}

	err := shutdown.Stop(quickTimeouts, "the producer",
		func() { recorded.add("stop the jobs") },
		fakeBus{recorded: recorded},
		func() error { recorded.add("close the database"); return nil },
		logger)

	require.NoError(t, err)
	assert.Equal(t, []string{"stop the jobs", "close the event bus", "close the database"}, recorded.steps)
	assert.Equal(t, []string{"Stopped the producer", "Closed the event bus", "Closed the database"}, messages(logs))
}

func TestStop_ClosesNoDatabaseWhenTheBinaryHasNone(t *testing.T) {
	logger, logs := observedLogger()
	recorded := &recordedSteps{}

	err := shutdown.Stop(quickTimeouts, "the worker", func() { recorded.add("stop the jobs") }, fakeBus{recorded: recorded}, nil, logger)

	require.NoError(t, err)
	assert.Equal(t, []string{"Stopped the worker", "Closed the event bus"}, messages(logs))
}

func TestStop_FailsAndLeavesTheDatabaseOpenWhenTheJobsOverrunTheirShare(t *testing.T) {
	logger, logs := observedLogger()
	recorded := &recordedSteps{}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	err := shutdown.Stop(quickTimeouts, "the ingester",
		func() { <-release },
		fakeBus{recorded: recorded},
		func() error { recorded.add("close the database"); return nil },
		logger)

	require.Error(t, err)
	assert.ErrorContains(t, err, "the ingester didn't stop in time")
	assert.Equal(t, []string{"close the event bus"}, recorded.steps, "the event bus still closes, and the database stays open")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"the ingester"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Stopped the ingester").Len())
}

func TestStop_FailsAndLeavesTheDatabaseOpenWhenTheEventBusDoesntClose(t *testing.T) {
	logger, logs := observedLogger()
	recorded := &recordedSteps{}

	err := shutdown.Stop(quickTimeouts, "the producer",
		func() {},
		fakeBus{recorded: recorded, err: context.DeadlineExceeded},
		func() error { recorded.add("close the database"); return nil },
		logger)

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to close the event bus")
	assert.NotContains(t, recorded.steps, "close the database")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"event handlers"}, left[0].ContextMap()["running"])
}

func TestStop_ReturnsTheErrorOfTheDatabaseClosing(t *testing.T) {
	logger, logs := observedLogger()
	closeFailed := errors.New("the disk is gone")

	err := shutdown.Stop(quickTimeouts, "the producer", func() {}, fakeBus{recorded: &recordedSteps{}}, func() error { return closeFailed }, logger)

	assert.ErrorIs(t, err, closeFailed)
	assert.Zero(t, logs.FilterMessage("Closed the database").Len())
}

func TestStop_GivesTheEventBusItsOwnShareWhenTheJobsOverrun(t *testing.T) {
	logger, _ := observedLogger()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var busDeadline time.Duration
	bus := busFunc(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		busDeadline = time.Until(deadline)
		return nil
	})

	_ = shutdown.Stop(quickTimeouts, "the producer", func() { <-release }, bus, nil, logger)

	assert.Greater(t, busDeadline, 100*time.Millisecond, "the jobs used up the event bus's share")
}

// busFunc is an event bus whose Close is the function
type busFunc func(ctx context.Context) error

func (f busFunc) Close(ctx context.Context) error { return f(ctx) }

func TestTimeouts_QueueShutdownLeavesAMarginInTheJobsShare(t *testing.T) {
	assert.Equal(t, 22*time.Second, shutdown.Timeouts{Jobs: 25 * time.Second}.QueueShutdown(), "the margin for the rest of the queue server to stop")
	assert.Equal(t, 1500*time.Millisecond, shutdown.Timeouts{Jobs: 3 * time.Second}.QueueShutdown(), "half of a share too short for the margin")
}
