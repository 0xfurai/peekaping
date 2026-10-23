package protocol_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// wakeTimeout is how long a test waits for an event to reach the waker through the bus
const wakeTimeout = 2 * time.Second

// subscribedWaker is a waker listening on a memory bus, like the one of a single-mode install
func subscribedWaker(t *testing.T) (*protocol.Waker, events.EventBus) {
	t.Helper()
	bus := events.NewMemoryBus()
	t.Cleanup(func() { require.NoError(t, bus.Close(context.Background())) })
	waker := protocol.NewWaker(bus, zap.NewNop().Sugar())
	waker.Subscribe()
	return waker, bus
}

// woken reports whether the wait has ended, within wakeTimeout
func woken(wait <-chan struct{}) bool {
	select {
	case <-wait:
		return true
	case <-time.After(wakeTimeout):
		return false
	}
}

// stillWaiting reports whether the wait is still going after a moment that is long enough for a
// wake that was sent along with the one that ended another wait
func stillWaiting(wait <-chan struct{}) bool {
	select {
	case <-wait:
		return false
	case <-time.After(100 * time.Millisecond):
		return true
	}
}

func TestWaker_WakesOnMonitorEvent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		publish func(ctx context.Context, bus events.EventBus) error
	}{
		{"created", func(ctx context.Context, bus events.EventBus) error {
			return events.Publish(ctx, bus, events.MonitorCreated, shared.Monitor{ID: "monitor-1"})
		}},
		{"updated", func(ctx context.Context, bus events.EventBus) error {
			return events.Publish(ctx, bus, events.MonitorUpdated, shared.Monitor{ID: "monitor-1"})
		}},
		{"deleted", func(ctx context.Context, bus events.EventBus) error {
			return events.Publish(ctx, bus, events.MonitorDeleted, "monitor-1")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			waker, bus := subscribedWaker(t)
			first, second := waker.Wait(ctx, "probe-1"), waker.Wait(ctx, "probe-2")
			require.True(t, stillWaiting(first), "nothing woke it yet")

			require.NoError(t, tc.publish(ctx, bus))

			assert.True(t, woken(first), "a monitor event wakes the waiters of every probe")
			assert.True(t, woken(second))
		})
	}
}

func TestWaker_WakesOnInstanceChange(t *testing.T) {
	ctx := context.Background()
	waker, bus := subscribedWaker(t)
	changed, other := waker.Wait(ctx, "probe-1"), waker.Wait(ctx, "probe-2")
	alsoChanged := waker.Wait(ctx, "probe-1")

	require.NoError(t, events.Publish(ctx, bus, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: "probe-1"}))

	assert.True(t, woken(changed))
	assert.True(t, woken(alsoChanged), "every waiter of the probe wakes, not one of them")
	assert.True(t, stillWaiting(other), "a waiter of another probe goes on waiting")
}

func TestWaker_AWaitAfterAWakeWaitsForTheNextOne(t *testing.T) {
	ctx := context.Background()
	waker, bus := subscribedWaker(t)
	before := waker.Wait(ctx, "probe-1")
	require.NoError(t, events.Publish(ctx, bus, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: "probe-1"}))
	require.True(t, woken(before))

	after := waker.Wait(ctx, "probe-1")

	assert.True(t, stillWaiting(after), "the wake that ended the first wait is not the second's")
	require.NoError(t, events.Publish(ctx, bus, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: "probe-1"}))
	assert.True(t, woken(after))
}

func TestWaker_ContextCancelStopsWaiting(t *testing.T) {
	waker, bus := subscribedWaker(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := waker.Wait(ctx, "probe-1")
	staying := waker.Wait(context.Background(), "probe-1")
	require.True(t, stillWaiting(cancelled))

	cancel()

	assert.True(t, woken(cancelled), "the wait ends when its context does")
	assert.True(t, stillWaiting(staying), "and only that one")
	require.NoError(t, events.Publish(context.Background(), bus, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: "probe-1"}))
	assert.True(t, woken(staying), "a wake after the cancel still reaches the others")
}

func TestWaker_ACancelledContextEndsTheWaitAtOnce(t *testing.T) {
	waker, _ := subscribedWaker(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.True(t, woken(waker.Wait(ctx, "probe-1")))
}

func TestWaker_CloseEndsEveryWaitAndSaysSo(t *testing.T) {
	waker, _ := subscribedWaker(t)
	first, second := waker.Wait(context.Background(), "probe-1"), waker.Wait(context.Background(), "probe-2")
	select {
	case <-waker.Closed():
		t.Fatal("the waker is closed before anyone closed it")
	default:
	}

	waker.Close()
	waker.Close()

	assert.True(t, woken(first), "a server that is stopping answers its polls")
	assert.True(t, woken(second))
	assert.True(t, woken(waker.Closed()))
}
