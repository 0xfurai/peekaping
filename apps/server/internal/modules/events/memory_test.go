package events_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMemoryBus(t *testing.T, opts ...events.Option) events.EventBus {
	t.Helper()
	bus := events.NewMemoryBus(opts...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bus.Close(ctx)
	})
	return bus
}

func TestMemoryBusContract(t *testing.T) {
	eventstest.RunContract(t, func(t *testing.T) events.EventBus { return newMemoryBus(t) })
}

func TestMemoryBusHandsHandlersThePublishedValue(t *testing.T) {
	type payload struct {
		MonitorID string
		// A channel can't be encoded as JSON, so it only arrives if nothing serializes the payload
		Done chan struct{}
	}
	topic := events.NewTopic("eventstest.memory", func(p *payload) string { return p.MonitorID })
	bus := newMemoryBus(t)
	received := make(chan *payload, 1)
	events.Subscribe(events.NewSubscriber(bus, "sub"), topic, func(_ context.Context, p *payload) { received <- p })

	sent := &payload{MonitorID: "mon-1", Done: make(chan struct{})}
	require.NoError(t, events.Publish(context.Background(), bus, topic, sent))

	select {
	case got := <-received:
		assert.Same(t, sent, got, "the handler gets the very value that was published")
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
}

func TestMemoryBusDropsEventsNobodySubscribedTo(t *testing.T) {
	bus := newMemoryBus(t)

	assert.NoError(t, events.Publish(context.Background(), bus, eventstest.Alpha, eventstest.Sample{MonitorID: "mon-1"}),
		"publishing to a topic with no handlers is not an error, as with Redis")
}
