//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifyShutdown_ASecondSignalExitsWithoutWaitingForTheShutdown(t *testing.T) {
	logger, logs := observedLogger()
	exited := make(chan int, 1)
	ctx, stop := notifyShutdown(logger, func(code int) { exited <- code })
	t.Cleanup(stop)

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	select {
	case <-ctx.Done():
	case <-time.After(wait):
		require.FailNow(t, "the first signal didn't start the shutdown")
	}
	select {
	case code := <-exited:
		require.FailNow(t, "the first signal exited", "code %d", code)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))
	select {
	case code := <-exited:
		assert.Equal(t, 1, code)
	case <-time.After(wait):
		require.FailNow(t, "the second signal didn't exit")
	}
	second := logs.FilterMessage("Second shutdown signal received, exiting without finishing the shutdown").All()
	require.Len(t, second, 1)
	assert.Equal(t, "interrupt", second[0].ContextMap()["signal"])
}
