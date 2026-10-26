package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrClosed is returned by Publish once the bus has been closed.
var ErrClosed = errors.New("event bus is closed")

// EventBus is what both event buses implement: Redis pub/sub across processes, and an
// in-memory bus inside one. Publish and Subscribe are generic functions over it, because a Go
// interface can't have generic methods. Use those, not these methods, from application code.
// Bus is the shared implementation that a Transport plugs into.
type EventBus interface {
	// PublishMessage delivers msg to every handler registered for msg.Topic, in every
	// process that has one. It returns ErrClosed once the bus is closed.
	PublishMessage(ctx context.Context, msg Message) error

	// AddHandler registers a handler in the named subscriber. Calling it again with the
	// same subscriber name adds to the same subscriber.
	AddHandler(subscriber string, handler Handler)

	// Close refuses new publishes, lets the handlers of events already queued finish until
	// ctx is done, and returns once the bus's goroutines have exited.
	Close(ctx context.Context) error
}

// Message is an event on its way between a publisher and the handlers. Exactly one of
// Value and JSON is set.
type Message struct {
	Topic string
	// Value is the payload as published: the T of the Topic[T]. Messages that stay inside
	// the process carry it, and a transport that crosses processes encodes it as JSON.
	Value any
	// JSON is the payload as it came off the wire. A handler decodes it into its own T.
	JSON json.RawMessage
}

// Handler is a typed handler with its payload type erased, ready to register.
type Handler struct {
	topic string
	bind  func(Message) (string, func(context.Context), error)
	// workers is the worker limit of the handler's subscriber; zero means the bus's default
	workers int
}

// Workers is the worker limit the handler's subscriber asked for, or zero for the bus's default.
func (h Handler) Workers() int { return h.workers }

// Topic is the name of the topic the handler subscribes to.
func (h Handler) Topic() string { return h.topic }

// Bind decodes msg into the handler's payload type. It returns the ID of the monitor the
// event is about (empty for none) and the call that runs the handler.
func (h Handler) Bind(msg Message) (monitorID string, call func(context.Context), err error) {
	return h.bind(msg)
}

// Publish sends payload to every handler subscribed to topic, in this process and in every
// other process on the same bus. It doesn't wait for the handlers.
//
// Publish ignores the cancellation of ctx: an event announces a change that is already
// stored, so dropping it because the request that made the change was cancelled would leave
// the other processes out of step. The transport applies its own timeout instead.
func Publish[T any](ctx context.Context, bus EventBus, topic Topic[T], payload T) error {
	if err := bus.PublishMessage(ctx, Message{Topic: topic.name, Value: payload}); err != nil {
		return fmt.Errorf("publish %s: %w", topic.name, err)
	}
	return nil
}

// Subscriber is a named group of handlers. The bus gives each subscriber its own queues, so
// one monitor's events stay in order across all the subscriber's topics, and a slow handler
// delays only the events behind it for the same monitor. Name a subscriber after the part of
// the program that owns the handlers, for example "websocket" or "notification".
type Subscriber struct {
	bus     EventBus
	name    string
	workers int
}

// SubscriberOption configures a Subscriber.
type SubscriberOption func(*Subscriber)

// SubscriberWorkers sets how many events the subscriber handles at the same time, in place of
// the bus's default. Give it to a subscriber whose handlers wait on the network, so that a few
// slow monitors can't take every worker. The first registration of a subscriber name sets it.
func SubscriberWorkers(n int) SubscriberOption {
	return func(s *Subscriber) { s.workers = max(n, 1) }
}

// NewSubscriber returns the subscriber called name on bus. Subscribers with the same name
// on the same bus share their queues.
func NewSubscriber(bus EventBus, name string, opts ...SubscriberOption) Subscriber {
	s := Subscriber{bus: bus, name: name}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// Subscribe registers handler for topic. The bus decodes each event into a T of its own
// for the handler, and runs the handler on one of the subscriber's worker goroutines with a
// context that is cancelled if the bus is closed before the handler is done.
func Subscribe[T any](sub Subscriber, topic Topic[T], handler func(context.Context, T)) {
	sub.bus.AddHandler(sub.name, Handler{
		topic:   topic.name,
		workers: sub.workers,
		bind: func(msg Message) (string, func(context.Context), error) {
			payload, err := decode[T](msg)
			if err != nil {
				return "", nil, err
			}
			return topic.key(payload), func(ctx context.Context) { handler(ctx, payload) }, nil
		},
	})
}

// decode reads the payload of msg as a T.
func decode[T any](msg Message) (T, error) {
	var payload T
	if msg.JSON != nil {
		if err := json.Unmarshal(msg.JSON, &payload); err != nil {
			return payload, fmt.Errorf("decode payload: %w", err)
		}
		return payload, nil
	}
	payload, ok := msg.Value.(T)
	if !ok {
		return payload, fmt.Errorf("payload is %T, not %T", msg.Value, payload)
	}
	return payload, nil
}
