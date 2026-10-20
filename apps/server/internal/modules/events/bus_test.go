package events_test

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// loopback is a transport that hands a published message straight to the bus. With wire set
// it encodes the payload as JSON first, as a transport between processes does.
type loopback struct {
	wire    bool
	deliver func(events.Message)
}

func (l *loopback) Start(deliver func(events.Message)) { l.deliver = deliver }

func (l *loopback) Publish(_ context.Context, msg events.Message) error {
	if l.wire {
		raw, err := json.Marshal(msg.Value)
		if err != nil {
			return err
		}
		msg = events.Message{Topic: msg.Topic, JSON: raw}
	}
	l.deliver(msg)
	return nil
}

func (l *loopback) Subscribe(string) {}

func (l *loopback) Close(context.Context) error { return nil }

func newLoopbackBus(t *testing.T, wire bool, opts ...events.Option) events.EventBus {
	t.Helper()
	bus := events.NewBus(&loopback{wire: wire}, opts...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Close(ctx)
	})
	return bus
}

func TestBusContract(t *testing.T) {
	t.Run("in process", func(t *testing.T) {
		eventstest.RunContract(t, func(t *testing.T) events.EventBus { return newLoopbackBus(t, false) })
	})
	t.Run("encoded as JSON", func(t *testing.T) {
		eventstest.RunContract(t, func(t *testing.T) events.EventBus { return newLoopbackBus(t, true) })
	})
}

func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core).Sugar(), logs
}

func TestBusLogsAPanicWithItsTopic(t *testing.T) {
	logger, logs := observedLogger()
	bus := newLoopbackBus(t, false, events.WithLogger(logger))
	events.Subscribe(events.NewSubscriber(bus, "panicky"), eventstest.Beta, func(context.Context, eventstest.Sample) {
		panic("handler bug")
	})

	require.NoError(t, events.Publish(context.Background(), bus, eventstest.Beta, eventstest.Sample{MonitorID: "mon-1"}))

	require.Eventually(t, func() bool { return logs.FilterMessage("Event handler panicked").Len() == 1 }, 5*time.Second, 5*time.Millisecond)
	entry := logs.FilterMessage("Event handler panicked").All()[0]
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Equal(t, "eventstest.beta", entry.ContextMap()["topic"])
	assert.Equal(t, "panicky", entry.ContextMap()["subscriber"])
	assert.Equal(t, "handler bug", entry.ContextMap()["panic"])
}

func TestBusDropsAnEventItCannotDecode(t *testing.T) {
	logger, logs := observedLogger()
	transport := &loopback{}
	bus := events.NewBus(transport, events.WithLogger(logger))
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	called := make(chan eventstest.Sample, 1)
	events.Subscribe(events.NewSubscriber(bus, "decoding"), eventstest.Alpha, func(_ context.Context, b eventstest.Sample) { called <- b })

	transport.deliver(events.Message{Topic: eventstest.Alpha.Name(), JSON: json.RawMessage(`{"monitor_id": 1}`)})
	transport.deliver(events.Message{Topic: eventstest.Alpha.Name(), JSON: json.RawMessage(`"mon-1"`)})
	transport.deliver(events.Message{Topic: eventstest.Alpha.Name(), Value: "mon-1"})
	transport.deliver(events.Message{Topic: eventstest.Alpha.Name(), JSON: json.RawMessage(`{"monitor_id": "mon-2", "seq": 3}`)})

	assert.Equal(t, eventstest.Sample{MonitorID: "mon-2", Seq: 3}, <-called, "the valid event after the bad ones is handled")
	assert.Equal(t, 3, logs.FilterMessageSnippet("Failed to decode event").Len())
	assert.Equal(t, "eventstest.alpha", logs.FilterMessageSnippet("Failed to decode event").All()[0].ContextMap()["topic"])
	assert.Empty(t, called)
}

func TestBusWarnsOnceQueuedEventsPassTheHighWaterMark(t *testing.T) {
	logger, logs := observedLogger()
	bus := newLoopbackBus(t, false, events.WithLogger(logger), events.WithHighWater(5), events.WithWorkers(1))
	release := make(chan struct{})
	var handled atomic.Int32
	events.Subscribe(events.NewSubscriber(bus, "slow"), eventstest.Alpha, func(context.Context, eventstest.Sample) {
		<-release
		handled.Add(1)
	})

	for seq := range 40 {
		require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1", Seq: seq}))
	}

	warnings := logs.FilterMessageSnippet("high-water mark")
	require.Equal(t, 1, warnings.Len(), "one warning, not one per event")
	assert.Equal(t, zapcore.WarnLevel, warnings.All()[0].Level)
	assert.Equal(t, "slow", warnings.All()[0].ContextMap()["subscriber"])
	close(release)
	require.Eventually(t, func() bool { return handled.Load() == 40 }, 5*time.Second, 5*time.Millisecond, "the queue has no limit")
}

func TestBusHandlesAtMostItsWorkerLimitAtOnce(t *testing.T) {
	bus := newLoopbackBus(t, false, events.WithWorkers(3))
	var running, peak, handled atomic.Int32
	events.Subscribe(events.NewSubscriber(bus, "bounded"), eventstest.Alpha, func(context.Context, eventstest.Sample) {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		handled.Add(1)
	})

	for m := range 12 {
		require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: string(rune('a' + m))}))
	}

	require.Eventually(t, func() bool { return handled.Load() == 12 }, 5*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 3, peak.Load())
}

func TestSubscriberCanHaveMoreWorkersThanTheBusDefault(t *testing.T) {
	bus := newLoopbackBus(t, false, events.WithWorkers(2))
	var started sync.WaitGroup
	started.Add(6)
	release := make(chan struct{})
	defer close(release)
	sub := events.NewSubscriber(bus, "wide", events.SubscriberWorkers(6))
	events.Subscribe(sub, eventstest.Alpha, func(context.Context, eventstest.Sample) {
		started.Done()
		<-release
	})

	for m := range 6 {
		require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: string(rune('a' + m))}))
	}

	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	select {
	case <-allStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("six blocked monitors should each hold one of the subscriber's six workers")
	}
}

func TestBusHandlesEventsWithoutAMonitorAtTheSameTime(t *testing.T) {
	unkeyed := events.NewTopic[eventstest.Sample]("eventstest.unkeyed", nil)
	bus := newLoopbackBus(t, false)
	var started sync.WaitGroup
	started.Add(3)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	var handled atomic.Int32
	events.Subscribe(events.NewSubscriber(bus, "unkeyed"), unkeyed, func(context.Context, eventstest.Sample) {
		started.Done()
		<-allStarted // returns only if the three are running together
		handled.Add(1)
	})

	for range 3 {
		require.NoError(t, events.Publish(context.Background(), bus, unkeyed, eventstest.Sample{}))
	}

	require.Eventually(t, func() bool { return handled.Load() == 3 }, 5*time.Second, 5*time.Millisecond)
}

func TestBusCloseStopsEveryGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	bus := events.NewBus(&loopback{})
	for _, name := range []string{"one", "two", "three"} {
		events.Subscribe(events.NewSubscriber(bus, name), eventstest.Alpha, func(context.Context, eventstest.Sample) {})
	}
	require.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1"}))

	require.NoError(t, bus.Close(context.Background()))

	// assert.Eventually runs its condition in a goroutine, which would count
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	assert.LessOrEqual(t, runtime.NumGoroutine(), before, "goroutines left after Close")
}

func TestBusIgnoresHandlersAddedAfterClose(t *testing.T) {
	logger, logs := observedLogger()
	bus := events.NewBus(&loopback{}, events.WithLogger(logger))
	require.NoError(t, bus.Close(context.Background()))

	events.Subscribe(events.NewSubscriber(bus, "late"), eventstest.Alpha, func(context.Context, eventstest.Sample) {})

	assert.Equal(t, 1, logs.FilterMessageSnippet("closed event bus").Len())
}

// silentTransport records what is published to it and delivers nothing, like a transport
// that no other process is listening on
type silentTransport struct {
	mu        sync.Mutex
	published []events.Message
}

func (s *silentTransport) Start(func(events.Message)) {}

func (s *silentTransport) Publish(_ context.Context, msg events.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, msg)
	return nil
}

func (s *silentTransport) Subscribe(string) {}

func (s *silentTransport) Close(context.Context) error { return nil }

func TestBusReceiveHandsAMessageToItsHandlersWithoutPublishingIt(t *testing.T) {
	transport := &silentTransport{}
	bus := events.NewBus(transport)
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	called := make(chan eventstest.Sample, 1)
	events.Subscribe(events.NewSubscriber(bus, "queue"), eventstest.Alpha, func(_ context.Context, b eventstest.Sample) { called <- b })

	// A message that reached the process by a route other than the transport arrives as JSON
	err := bus.Receive(events.Message{Topic: eventstest.Alpha.Name(), JSON: json.RawMessage(`{"monitor_id": "mon-1", "seq": 3}`)})

	require.NoError(t, err)
	assert.Equal(t, eventstest.Sample{MonitorID: "mon-1", Seq: 3}, <-called)
	assert.Empty(t, transport.published, "Receive doesn't publish")
}

func TestBusReceiveRefusesAMessageOnceClosed(t *testing.T) {
	bus := events.NewBus(&silentTransport{})
	called := make(chan eventstest.Sample, 1)
	events.Subscribe(events.NewSubscriber(bus, "queue"), eventstest.Alpha, func(_ context.Context, b eventstest.Sample) { called <- b })
	require.NoError(t, bus.Close(context.Background()))

	err := bus.Receive(events.Message{Topic: eventstest.Alpha.Name(), JSON: json.RawMessage(`{"monitor_id": "mon-1"}`)})

	assert.ErrorIs(t, err, events.ErrClosed)
	assert.Empty(t, called)
}

// gatedPayload is an event payload that stops while the bus decodes it, so a test can hold a
// message partway through its delivery. It uses decodeGate, which a test sets before it
// delivers a message.
type gatedPayload struct{}

var decodeGate struct {
	started chan struct{} // closed when a payload begins to decode
	release chan struct{} // the payload finishes decoding once this is closed
}

func (*gatedPayload) UnmarshalJSON([]byte) error {
	close(decodeGate.started)
	<-decodeGate.release
	return nil
}

// Close can run at any point of a Receive that got past the closed check. Receive either hands
// the message to the handlers, which Close then lets finish, or tells the caller that the bus is
// closed. It must not return nil and drop the message: the caller would take the message as
// delivered, and the queue task it came from as done.
func TestBusReceiveDeliversOrReturnsErrClosedWhenCloseRunsDuringIt(t *testing.T) {
	gated := events.NewTopic[gatedPayload]("eventstest.gated", nil)
	decodeGate.started, decodeGate.release = make(chan struct{}), make(chan struct{})
	bus := events.NewBus(&silentTransport{})
	var handled atomic.Int32
	events.Subscribe(events.NewSubscriber(bus, "gated"), gated, func(context.Context, gatedPayload) { handled.Add(1) })

	received := make(chan error, 1)
	go func() { received <- bus.Receive(events.Message{Topic: gated.Name(), JSON: json.RawMessage(`{}`)}) }()
	<-decodeGate.started // Receive is past the closed check, decoding the message for the handler
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var closeErr error
	closed := make(chan struct{})
	go func() {
		closeErr = bus.Close(closeCtx)
		close(closed)
	}()
	select {
	case <-closed:
		// Close didn't wait for the Receive in progress
	case <-time.After(250 * time.Millisecond):
		// Close is waiting for it
	}
	close(decodeGate.release)

	receiveErr := <-received
	<-closed
	require.NoError(t, closeErr)
	if receiveErr == nil {
		assert.EqualValues(t, 1, handled.Load(), "Receive returned nil, but the handler never got the message")
	} else {
		assert.ErrorIs(t, receiveErr, events.ErrClosed)
		assert.Zero(t, handled.Load(), "Receive returned an error, but the handler got the message")
	}
}
