package events

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

const (
	// DefaultWorkers is how many events one subscriber handles at the same time.
	DefaultWorkers = 16
	// DefaultHighWater is how many queued events make a subscriber log a warning.
	DefaultHighWater = 10_000
)

// Transport carries events between buses: over Redis pub/sub between processes, or inside
// one process. It owns the wire, and Bus owns everything after it: decoding, per-monitor
// queues, workers, panic recovery and shutdown. A new transport implements these four
// methods and nothing else.
type Transport interface {
	// Start gives the transport the function to call for each message that arrives for a
	// topic the bus subscribed to. Bus calls Start once, before anything else. deliver
	// doesn't block on handlers, and messages passed to it in order are handled in order.
	Start(deliver func(Message))

	// Publish sends msg to every bus that subscribed to msg.Topic, this one included.
	Publish(ctx context.Context, msg Message) error

	// Subscribe starts receiving messages for topic. Bus calls it once for each topic,
	// from the first AddHandler for it. It returns when events published from then on are
	// delivered, or when the transport gives up waiting for that.
	Subscribe(topic string)

	// Close stops receiving. Messages already passed to deliver still run.
	Close(ctx context.Context) error
}

// Option configures a Bus.
type Option func(*Bus)

// WithLogger sets the logger. The default logs nothing.
func WithLogger(logger *zap.SugaredLogger) Option {
	return func(b *Bus) { b.logger = logger }
}

// WithWorkers sets how many events one subscriber handles at the same time. Events for
// one monitor are handled one at a time whatever n is.
func WithWorkers(n int) Option {
	return func(b *Bus) { b.workers = max(n, 1) }
}

// WithHighWater sets how many queued events make a subscriber log a warning. A subscriber's
// queue has no limit: the warning is the only sign that a handler isn't keeping up.
func WithHighWater(n int) Option {
	return func(b *Bus) { b.highWater = max(n, 1) }
}

// Bus is the event bus both transports share. Each subscriber has its own set of worker
// goroutines and per-monitor queues, so ordering and isolation work the same over any
// Transport.
//
//   - A subscriber handles the events of one monitor in the order they reached the bus,
//     across all its topics, one at a time. Different monitors are handled at the same
//     time, up to the subscriber's worker limit.
//   - Publish never waits for a handler. A handler that is slow, blocked or panicking holds
//     up only the later events for its own monitor, and the other subscribers not at all. A
//     panic is recovered and logged with its topic.
//   - Queues are unbounded. A subscriber logs a warning once its queue passes the
//     high-water mark.
type Bus struct {
	transport Transport
	logger    *zap.SugaredLogger
	workers   int
	highWater int

	// handlerCtx is the context the handlers run with. It is cancelled when Close gives up
	// waiting for them, or after the drain.
	handlerCtx    context.Context
	cancelHandler context.CancelFunc

	mu          sync.Mutex
	closed      bool
	subscribers map[string]*subscriber
	topics      map[string]bool
	inflight    sync.WaitGroup // publishes that have passed the closed check

	done     chan struct{} // closed when Close has finished
	closeErr error
}

var _ EventBus = (*Bus)(nil)

// NewBus returns a Bus over transport.
func NewBus(transport Transport, opts ...Option) *Bus {
	b := &Bus{
		transport:   transport,
		logger:      zap.NewNop().Sugar(),
		workers:     DefaultWorkers,
		highWater:   DefaultHighWater,
		subscribers: make(map[string]*subscriber),
		topics:      make(map[string]bool),
		done:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	b.handlerCtx, b.cancelHandler = context.WithCancel(context.Background())
	transport.Start(b.deliver)
	return b
}

// PublishMessage implements EventBus.
func (b *Bus) PublishMessage(ctx context.Context, msg Message) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	b.inflight.Add(1)
	b.mu.Unlock()
	defer b.inflight.Done()

	return b.transport.Publish(ctx, msg)
}

// AddHandler implements EventBus.
func (b *Bus) AddHandler(name string, handler Handler) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.logger.Warnw("Handler registered on a closed event bus, ignoring it", "subscriber", name, "topic", handler.topic)
		return
	}
	sub := b.subscribers[name]
	if sub == nil {
		workers := handler.workers
		if workers == 0 {
			workers = b.workers
		}
		sub = newSubscriber(b, name, workers)
		b.subscribers[name] = sub
	}
	sub.addHandler(handler)
	first := !b.topics[handler.topic]
	b.topics[handler.topic] = true
	b.mu.Unlock()

	if first {
		b.transport.Subscribe(handler.topic)
	}
}

// deliver passes a message from the transport to every subscriber with a handler for its topic.
func (b *Bus) deliver(msg Message) {
	b.mu.Lock()
	subscribers := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subscribers = append(subscribers, sub)
	}
	b.mu.Unlock()

	for _, sub := range subscribers {
		sub.deliver(msg)
	}
}

// Close implements EventBus. It stops the transport, then waits for the queued events to be
// handled and for the workers to exit. If ctx is done first, it cancels the handlers' context,
// drops what is still queued and returns an error that wraps ctx's: a handler that doesn't
// watch its context is left running until it returns.
func (b *Bus) Close(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		select {
		case <-b.done:
			return b.closeErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.closed = true
	subscribers := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subscribers = append(subscribers, sub)
	}
	b.mu.Unlock()

	b.closeErr = b.shutdown(ctx, subscribers)
	close(b.done)
	return b.closeErr
}

func (b *Bus) shutdown(ctx context.Context, subscribers []*subscriber) error {
	defer b.cancelHandler()
	var errs []error

	// Publishes that got in before the flag may still add events
	if err := waitFor(ctx, &b.inflight); err != nil {
		errs = append(errs, fmt.Errorf("waiting for publishes: %w", err))
	}
	if err := b.transport.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("closing transport: %w", err))
	}

	// Workers exit once the queued events are handled
	for _, sub := range subscribers {
		sub.stop()
	}
	for _, sub := range subscribers {
		if err := waitFor(ctx, &sub.wg); err != nil {
			b.cancelHandler()
			dropped, running := 0, 0
			for _, other := range subscribers {
				d, r := other.discard()
				dropped, running = dropped+d, running+r
			}
			errs = append(errs, fmt.Errorf("%d queued events dropped, %d handlers still running: %w", dropped, running, err))
			break
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("closing event bus: %w", errors.Join(errs...))
	}
	return nil
}

// waitFor waits for wg or for ctx, whichever is first.
func waitFor(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}
