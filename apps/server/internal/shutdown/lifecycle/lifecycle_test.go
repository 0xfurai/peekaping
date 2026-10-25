package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/shutdown/lifecycle"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the code under test does on its own
const wait = 10 * time.Second

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

func TestNotifySignals_StopCancelsTheContextAndMayBeCalledAgain(t *testing.T) {
	logger, _ := observedLogger()
	ctx, stop := lifecycle.NotifySignals(logger, func(code int) { assert.Fail(t, "exit was called", "code %d", code) })

	require.NoError(t, ctx.Err())
	stop()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(wait):
		require.FailNow(t, "stop didn't cancel the context")
	}
}

func TestAwait_ReturnsOnceDoneClosesBeforeTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	done := make(chan struct{})
	awaited := make(chan error, 1)
	go func() { awaited <- lifecycle.Await(ctx, "the scheduler", done) }()

	select {
	case err := <-awaited:
		require.FailNow(t, "Await returned before done closed", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(done)

	select {
	case err := <-awaited:
		assert.NoError(t, err)
	case <-time.After(wait):
		require.FailNow(t, "Await didn't return once done closed")
	}
}

func TestAwait_FailsNamingTheStepWhenTheContextIsDoneFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := lifecycle.Await(ctx, "the scheduler", make(chan struct{}))

	require.Error(t, err)
	assert.EqualError(t, err, "the scheduler didn't stop in time: context deadline exceeded")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// Both channels are ready when the context is already done. A select picks one of them at random,
// so one call could fail by chance: the loop makes a missing check fail every time.
func TestAwait_CountsADoneThatClosedEvenOnceTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	close(done)

	for range 100 {
		require.NoError(t, lifecycle.Await(ctx, "the scheduler", done))
	}
}
