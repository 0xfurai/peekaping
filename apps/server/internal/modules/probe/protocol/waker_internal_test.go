package protocol

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// waiting counts the waits the waker holds
func (w *Waker) waiting() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, set := range w.waiters {
		n += len(set)
	}
	return n
}

// A poll that ends, by a wake or by its client leaving, must not leave anything behind: polls come
// every 25 seconds for as long as the server runs
func TestWaker_ForgetsTheWaitsThatEnded(t *testing.T) {
	waker := NewWaker(events.NewMemoryBus(), zap.NewNop().Sugar())

	woken := waker.Wait(context.Background(), "probe-1")
	waker.wake("probe-1")
	<-woken
	cancelled, cancel := context.WithCancel(context.Background())
	gone := waker.Wait(cancelled, "probe-2")
	waker.Wait(context.Background(), "probe-3")
	require.Equal(t, 2, waker.waiting())
	cancel()
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled wait did not end")
	}
	waker.wakeAll("test")

	assert.Zero(t, waker.waiting())
	assert.Empty(t, waker.waiters, "and no probe is left with an empty set")
}
