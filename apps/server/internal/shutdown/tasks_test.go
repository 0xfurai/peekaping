package shutdown_test

import (
	"context"
	"testing"

	"peekaping/internal/shutdown"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
)

func TestRunningTasks_WrapRunsTheHandlerAndPassesOnWhatItReturns(t *testing.T) {
	var tasks shutdown.RunningTasks
	failed := assert.AnError
	handler := tasks.Wrap(func(_ context.Context, task *asynq.Task) error {
		assert.Equal(t, "monitor:ingest", task.Type())
		return failed
	})

	err := handler(context.Background(), asynq.NewTask("monitor:ingest", nil))

	assert.ErrorIs(t, err, failed)
	assert.True(t, isClosed(tasks.Done()), "the handler has returned")
}

func TestRunningTasks_DoneClosesOnceTheRunningHandlerReturns(t *testing.T) {
	var tasks shutdown.RunningTasks
	entered, release := make(chan struct{}), make(chan struct{})
	handler := tasks.Wrap(func(context.Context, *asynq.Task) error {
		close(entered)
		<-release
		return nil
	})
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_ = handler(context.Background(), asynq.NewTask("monitor:ingest", nil))
	}()
	receive(t, entered, "the handler didn't run")

	done := tasks.Done()
	assert.False(t, isClosed(done), "the handler is running")

	close(release)
	receive(t, returned, "the handler didn't return")
	receive(t, done, "Done didn't close once the handler returned")
}
