//go:build unix

package lifecycle_test

import (
	"os"
	"syscall"
	"testing"
	"time"

	"peekaping/internal/shutdown/lifecycle"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifySignals_ASecondSignalExitsWithoutWaitingForTheShutdown(t *testing.T) {
	logger, logs := observedLogger()
	exited := make(chan int, 1)
	ctx, stop := lifecycle.NotifySignals(logger, func(code int) { exited <- code })
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
