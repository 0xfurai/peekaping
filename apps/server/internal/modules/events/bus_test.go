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
