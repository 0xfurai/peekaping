package events

import "context"

// NewMemoryBus returns the event bus for single mode, where every part of Peekaping runs in
// one process: events never leave the process, so there is no Redis and no serialization.
// Handlers receive the very value that was published, and Publish returns before they run,
// so the payload belongs to nobody once published: handlers must not change it, and neither
// may the publisher. A publisher that keeps working on a value publishes a copy.
//
// Ordering, worker limits, panic recovery and Close behave exactly as on the Redis bus,
// because both are a Bus; only the transport differs.
func NewMemoryBus(opts ...Option) *Bus {
	return NewBus(&memoryTransport{}, opts...)
}

// memoryTransport hands each published message straight to the bus it belongs to. The bus
// queues the handlers and returns, so Publish never waits for them.
type memoryTransport struct {
	deliver func(Message)
}

var _ Transport = (*memoryTransport)(nil)

// Start implements Transport. Bus calls it before any Publish, so deliver needs no lock.
func (t *memoryTransport) Start(deliver func(Message)) { t.deliver = deliver }

// Publish implements Transport.
func (t *memoryTransport) Publish(_ context.Context, msg Message) error {
	t.deliver(msg)
	return nil
}

// Subscribe implements Transport. The bus already knows its handlers, so there is nothing to do.
func (t *memoryTransport) Subscribe(string) {}

// Close implements Transport. There is nothing to stop: Bus refuses publishes once it is closing.
func (t *memoryTransport) Close(context.Context) error { return nil }
