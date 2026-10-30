// Package eventstest has what tests of event bus users and event bus implementations share:
// a contract suite every bus must pass, and a synchronous recording bus for unit tests.
package eventstest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sample is the payload of the contract suite's topics.
type Sample struct {
	MonitorID string `json:"monitor_id"`
	Seq       int    `json:"seq"`
}

// The contract suite's topics. They carry the same payload, so a subscriber's two topics
// can be checked for order against each other.
var (
	Alpha = events.NewTopic("eventstest.alpha", func(b Sample) string { return b.MonitorID })
	Beta  = events.NewTopic("eventstest.beta", func(b Sample) string { return b.MonitorID })
)

// waitFor is how long the suite waits for something it expects to happen.
const waitFor = 10 * time.Second

// RunContract runs the tests every events.EventBus must pass. newBus returns a new bus each
// call, and closes it when the test ends. A bus must deliver the events one goroutine
// publishes in the order it published them, across topics: a few tests use that to know that
// events have arrived.
func RunContract(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	for _, c := range contract {
		t.Run(c.name, func(t *testing.T) { c.run(t, newBus) })
	}
}

// contract is the tests every bus must pass, in the order they run
var contract = []struct {
	name string
	run  func(t *testing.T, newBus func(t *testing.T) events.EventBus)
}{
	{"delivers the typed value to every handler of the topic", deliversTheTypedValueToEveryHandler},
	{"handles one monitor's events in publish order across a subscriber's topics", handlesOneMonitorsEventsInPublishOrder},
	{"handles different monitors at the same time", handlesDifferentMonitorsAtTheSameTime},
	{"a blocked handler delays only its own monitor", aBlockedHandlerDelaysOnlyItsOwnMonitor},
	{"a panicking handler stops nothing else", aPanickingHandlerStopsNothingElse},
	{"publish returns while handlers are blocked", publishReturnsWhileHandlersAreBlocked},
	{"close handles the queued events before it returns", closeHandlesTheQueuedEvents},
	{"publish after close returns ErrClosed", publishAfterCloseReturnsErrClosed},
	{"close gives up at its deadline when a handler is stuck", closeGivesUpAtItsDeadline},
}

// deliversTheTypedValueToEveryHandler is the test "delivers the typed value to every handler of the topic"
func deliversTheTypedValueToEveryHandler(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	first, second := make(chan Sample, 4), make(chan Sample, 4)
	other := make(chan Sample, 4)
	events.Subscribe(events.NewSubscriber(bus, "one"), Alpha, func(_ context.Context, b Sample) { first <- b })
	events.Subscribe(events.NewSubscriber(bus, "one"), Alpha, func(_ context.Context, b Sample) { first <- b })
	events.Subscribe(events.NewSubscriber(bus, "two"), Alpha, func(_ context.Context, b Sample) { second <- b })
	events.Subscribe(events.NewSubscriber(bus, "two"), Beta, func(_ context.Context, b Sample) { other <- b })

	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1", Seq: 7}))

	want := Sample{MonitorID: "mon-1", Seq: 7}
	assert.Equal(t, want, receive(t, first), "first handler of subscriber one")
	assert.Equal(t, want, receive(t, first), "second handler of subscriber one")
	assert.Equal(t, want, receive(t, second), "subscriber two")
	assertNothing(t, other, "a handler of another topic")
	assertNothing(t, first, "a handler called twice")
}

// handlesOneMonitorsEventsInPublishOrder is the test "handles one monitor's events in publish order across a subscriber's topics"
func handlesOneMonitorsEventsInPublishOrder(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	const monitors, perMonitor = 4, 60
	var mu sync.Mutex
	seen := map[string][]int{}
	var handled atomic.Int32
	record := func(_ context.Context, b Sample) {
		// A handler that takes a different time each call would reorder events that
		// are handled concurrently
		time.Sleep(time.Duration(rand.IntN(300)) * time.Microsecond)
		mu.Lock()
		seen[b.MonitorID] = append(seen[b.MonitorID], b.Seq)
		mu.Unlock()
		handled.Add(1)
	}
	sub := events.NewSubscriber(bus, "ordered")
	events.Subscribe(sub, Alpha, record)
	events.Subscribe(sub, Beta, record)

	for seq := range perMonitor {
		for m := range monitors {
			topic := Alpha
			if (seq+m)%2 == 1 {
				topic = Beta
			}
			require.NoError(t, events.Publish(context.Background(), bus, topic, Sample{MonitorID: fmt.Sprintf("mon-%d", m), Seq: seq}))
		}
	}

	require.Eventually(t, func() bool { return handled.Load() == monitors*perMonitor }, waitFor, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for m := range monitors {
		got := seen[fmt.Sprintf("mon-%d", m)]
		want := make([]int, perMonitor)
		for i := range want {
			want[i] = i
		}
		assert.Equal(t, want, got, "mon-%d", m)
	}
}

// handlesDifferentMonitorsAtTheSameTime is the test "handles different monitors at the same time"
func handlesDifferentMonitorsAtTheSameTime(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	blocked, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	fast := make(chan Sample, 8)
	events.Subscribe(events.NewSubscriber(bus, "concurrent"), Alpha, func(_ context.Context, b Sample) {
		if b.MonitorID == "slow" {
			blocked <- struct{}{}
			<-release
			return
		}
		fast <- b
	})

	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "slow"}))
	receive(t, blocked)
	for seq := range 3 {
		require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: fmt.Sprintf("fast-%d", seq), Seq: seq}))
	}

	for range 3 {
		receive(t, fast)
	}
}

// aBlockedHandlerDelaysOnlyItsOwnMonitor is the test "a blocked handler delays only its own monitor"
func aBlockedHandlerDelaysOnlyItsOwnMonitor(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	started, release := make(chan struct{}, 8), make(chan struct{})
	t.Cleanup(func() { close(release) })
	var order []int
	var mu sync.Mutex
	events.Subscribe(events.NewSubscriber(bus, "blocked"), Alpha, func(_ context.Context, b Sample) {
		if b.MonitorID == "mon-1" && b.Seq == 0 {
			started <- struct{}{}
			<-release
		}
		if b.MonitorID == "mon-1" {
			mu.Lock()
			order = append(order, b.Seq)
			mu.Unlock()
		}
	})
	other := make(chan Sample, 1)
	events.Subscribe(events.NewSubscriber(bus, "bystander"), Alpha, func(_ context.Context, b Sample) {
		if b.MonitorID == "mon-2" {
			other <- b
		}
	})

	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1", Seq: 0}))
	receive(t, started)
	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1", Seq: 1}))
	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-2"}))

	assert.Equal(t, Sample{MonitorID: "mon-2"}, receive(t, other), "another subscriber is not held up")
	mu.Lock()
	assert.Empty(t, order, "mon-1's later event waits for the blocked one")
	mu.Unlock()
	release <- struct{}{}
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(order) == 2 }, waitFor, 5*time.Millisecond)
	mu.Lock()
	assert.Equal(t, []int{0, 1}, order)
	mu.Unlock()
}

// aPanickingHandlerStopsNothingElse is the test "a panicking handler stops nothing else"
func aPanickingHandlerStopsNothingElse(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	var mu sync.Mutex
	var survivors []int
	events.Subscribe(events.NewSubscriber(bus, "panicky"), Alpha, func(_ context.Context, b Sample) {
		if b.Seq == 0 {
			panic("handler bug")
		}
		mu.Lock()
		survivors = append(survivors, b.Seq)
		mu.Unlock()
	})
	neighbour := make(chan Sample, 4)
	events.Subscribe(events.NewSubscriber(bus, "neighbour"), Alpha, func(_ context.Context, b Sample) { neighbour <- b })

	for seq := range 3 {
		require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1", Seq: seq}))
	}

	for seq := range 3 {
		assert.Equal(t, Sample{MonitorID: "mon-1", Seq: seq}, receive(t, neighbour))
	}
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(survivors) == 2 }, waitFor, 5*time.Millisecond,
		"the events after the panic, for the same monitor, are handled")
	mu.Lock()
	assert.Equal(t, []int{1, 2}, survivors)
	mu.Unlock()
}

// publishReturnsWhileHandlersAreBlocked is the test "publish returns while handlers are blocked"
func publishReturnsWhileHandlersAreBlocked(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	release := make(chan struct{})
	var handled atomic.Int32
	t.Cleanup(func() { close(release) })
	events.Subscribe(events.NewSubscriber(bus, "stuck"), Alpha, func(_ context.Context, b Sample) {
		handled.Add(1)
		<-release
	})

	start := time.Now()
	for seq := range 100 {
		require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: fmt.Sprintf("mon-%d", seq%25), Seq: seq}))
	}

	assert.Less(t, time.Since(start), 5*time.Second)
	require.Eventually(t, func() bool { return handled.Load() > 0 }, waitFor, 5*time.Millisecond)
}

// closeHandlesTheQueuedEvents is the test "close handles the queued events before it returns"
func closeHandlesTheQueuedEvents(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	const queued = 5
	started, release, marker := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var returned atomic.Int32
	events.Subscribe(events.NewSubscriber(bus, "drained"), Alpha, func(_ context.Context, b Sample) {
		defer returned.Add(1)
		switch b.MonitorID {
		case "marker":
			close(marker)
		case "mon-1":
			if b.Seq == 0 {
				close(started)
				<-release
			}
		}
	})
	for seq := range queued {
		require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1", Seq: seq}))
	}
	// The marker is on another monitor, so it runs while mon-1 is blocked. Events reach
	// the subscriber in publish order, so once it has run, the mon-1 events are queued.
	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "marker"}))
	receive(t, started)
	receive(t, marker)

	closed := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	go func() { closed <- bus.Close(ctx) }()

	select {
	case err := <-closed:
		t.Fatalf("Close returned %v while a handler was still blocked", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, receive(t, closed))
	assert.EqualValues(t, queued+1, returned.Load(), "every handler has returned when Close does")
}

// publishAfterCloseReturnsErrClosed is the test "publish after close returns ErrClosed"
func publishAfterCloseReturnsErrClosed(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	events.Subscribe(events.NewSubscriber(bus, "closed"), Alpha, func(context.Context, Sample) {})

	require.NoError(t, bus.Close(context.Background()))

	err := events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1"})
	assert.ErrorIs(t, err, events.ErrClosed)
	assert.NoError(t, bus.Close(context.Background()), "closing twice is fine")
}

// closeGivesUpAtItsDeadline is the test "close gives up at its deadline when a handler is stuck"
func closeGivesUpAtItsDeadline(t *testing.T, newBus func(t *testing.T) events.EventBus) {
	bus := newBus(t)
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	cancelled := make(chan struct{})
	events.Subscribe(events.NewSubscriber(bus, "stuck"), Alpha, func(ctx context.Context, b Sample) {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-release:
		}
	})
	require.NoError(t, events.Publish(context.Background(), bus, Alpha, Sample{MonitorID: "mon-1"}))
	receive(t, started)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := bus.Close(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
	receive(t, cancelled) // a handler that watches its context learns it should stop
}

// receive returns the next value from ch, failing the test if none comes.
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(waitFor):
		t.Fatalf("timed out waiting for a %T", *new(T))
		panic("unreachable")
	}
}

// assertNothing fails the test if ch has a value within a short time.
func assertNothing[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case v := <-ch:
		t.Errorf("%s got %v", what, v)
	case <-time.After(100 * time.Millisecond):
	}
}
