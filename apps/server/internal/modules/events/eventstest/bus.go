package eventstest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"peekaping/internal/modules/events"
)

// Bus is an events.EventBus for unit tests. It handles an event inside the Publish call that
// published it, on the caller's goroutine, so a test can publish and then check the result
// with no waiting. A handler's panic reaches the test. It also records what was published.
//
// It has no queues, no workers and no ordering to speak of: use RunContract, against a real
// bus, for that.
type Bus struct {
	mu        sync.Mutex
	handlers  map[string][]events.Handler // by topic name
	published []events.Message
	closed    bool
}

var _ events.EventBus = (*Bus)(nil)

// NewBus returns a bus with nothing subscribed and nothing published.
func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]events.Handler)}
}

// PublishMessage implements events.EventBus.
func (b *Bus) PublishMessage(ctx context.Context, msg events.Message) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return events.ErrClosed
	}
	b.published = append(b.published, msg)
	handlers := append([]events.Handler(nil), b.handlers[msg.Topic]...)
	b.mu.Unlock()

	var firstErr error
	for _, h := range handlers {
		_, call, err := h.Bind(msg)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("deliver %s: %w", msg.Topic, err)
			}
			continue
		}
		call(ctx)
	}
	return firstErr
}

// AddHandler implements events.EventBus.
func (b *Bus) AddHandler(_ string, handler events.Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[handler.Topic()] = append(b.handlers[handler.Topic()], handler)
}

// Close implements events.EventBus. Publishing after it returns events.ErrClosed.
func (b *Bus) Close(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// PublishJSON delivers a payload as it arrives from another process: as JSON, to be decoded
// into each handler's type. A payload a handler can't decode is dropped, as the real buses do,
// and PublishJSON returns the error.
func (b *Bus) PublishJSON(topic string, payload string) error {
	return b.PublishMessage(context.Background(), events.Message{Topic: topic, JSON: json.RawMessage(payload)})
}

// SubscribedTopics returns the names of the topics with a handler.
func (b *Bus) SubscribedTopics() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	topics := make([]string, 0, len(b.handlers))
	for topic := range b.handlers {
		topics = append(topics, topic)
	}
	return topics
}

// Topics returns the names of the topics published to so far, oldest first.
func (b *Bus) Topics() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	topics := make([]string, 0, len(b.published))
	for _, msg := range b.published {
		topics = append(topics, msg.Topic)
	}
	return topics
}

// TakeTopics is Topics, and forgets what was published.
func (b *Bus) TakeTopics() []string {
	topics := b.Topics()
	b.mu.Lock()
	b.published = nil
	b.mu.Unlock()
	return topics
}

// Published returns the payloads published to topic so far, oldest first.
func Published[T any](b *Bus, topic events.Topic[T]) []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	payloads := []T{}
	for _, msg := range b.published {
		if msg.Topic != topic.Name() {
			continue
		}
		if payload, ok := msg.Value.(T); ok {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}
