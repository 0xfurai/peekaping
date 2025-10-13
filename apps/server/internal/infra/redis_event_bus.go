package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"sync"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// RedisEventChannelPrefix is the prefix for Redis pub/sub channels
	RedisEventChannelPrefix = "peekaping:events:"
)

// RedisEventBus is a distributed event bus implementation using Redis Pub/Sub
type RedisEventBus struct {
	client      *redis.Client
	pubClient   *redis.Client // Separate client for publishing
	logger      *zap.SugaredLogger
	mu          sync.RWMutex
	handlers    map[events.EventType][]events.EventHandler
	subscribers map[events.EventType]*redis.PubSub
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// SerializedEvent represents an event that can be serialized to JSON
type SerializedEvent struct {
	Type    events.EventType `json:"type"`
	Payload json.RawMessage  `json:"payload"`
}

// NewRedisEventBus creates a new Redis-based event bus
func NewRedisEventBus(client *redis.Client, logger *zap.SugaredLogger) *RedisEventBus {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a separate client for publishing to avoid blocking
	pubClient := redis.NewClient(client.Options())

	return &RedisEventBus{
		client:      client,
		pubClient:   pubClient,
		logger:      logger.With("component", "redis_event_bus"),
		handlers:    make(map[events.EventType][]events.EventHandler),
		subscribers: make(map[events.EventType]*redis.PubSub),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Subscribe registers a handler for a specific event type and starts listening
func (b *RedisEventBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	b.logger.Debugf("Subscribing to event: %s", eventType)
	b.mu.Lock()
	defer b.mu.Unlock()

	// Add handler to local handlers
	handlers := b.handlers[eventType]
	handlers = append(handlers, handler)
	b.handlers[eventType] = handlers

	// If this is the first handler for this event type, create Redis subscription
	if len(handlers) == 1 {
		b.startRedisSubscription(eventType)
	}
}

// startRedisSubscription starts a Redis pub/sub subscription for an event type
func (b *RedisEventBus) startRedisSubscription(eventType events.EventType) {
	channel := b.getChannelName(eventType)

	// Create pub/sub subscription
	pubsub := b.client.Subscribe(b.ctx, channel)
	b.subscribers[eventType] = pubsub

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.logger.Infof("Started Redis subscription for channel: %s", channel)

		// Wait for confirmation
		_, err := pubsub.Receive(b.ctx)
		if err != nil {
			b.logger.Errorw("Failed to receive subscription confirmation",
				"channel", channel,
				"error", err,
			)
			return
		}

		// Start listening for messages
		ch := pubsub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					b.logger.Infof("Channel closed for event type: %s", eventType)
					return
				}
				b.handleRedisMessage(eventType, msg)

			case <-b.ctx.Done():
				b.logger.Infof("Context cancelled, stopping subscription for: %s", eventType)
				return
			}
		}
	}()
}

// handleRedisMessage processes a message received from Redis
func (b *RedisEventBus) handleRedisMessage(eventType events.EventType, msg *redis.Message) {
	b.logger.Debugf("Received Redis message for event type: %s", eventType)

	// Deserialize the event
	var serialized SerializedEvent
	if err := json.Unmarshal([]byte(msg.Payload), &serialized); err != nil {
		b.logger.Errorw("Failed to unmarshal event",
			"event_type", eventType,
			"error", err,
		)
		return
	}

	// Reconstruct the event with raw payload
	event := events.Event{
		Type:    serialized.Type,
		Payload: serialized.Payload,
	}

	// Call all local handlers
	b.mu.RLock()
	handlers := b.handlers[eventType]
	b.mu.RUnlock()

	for _, handler := range handlers {
		go func(h events.EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Errorw("Event handler panicked",
						"event_type", eventType,
						"panic", r,
					)
				}
			}()
			h(event)
		}(handler)
	}
}

// Publish sends an event to all registered handlers across all instances
func (b *RedisEventBus) Publish(event events.Event) {
	b.logger.Debugf("Publishing event: %s", event.Type)

	// Serialize the event
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		b.logger.Errorw("Failed to marshal event payload",
			"event_type", event.Type,
			"error", err,
		)
		return
	}

	serialized := SerializedEvent{
		Type:    event.Type,
		Payload: payloadJSON,
	}

	data, err := json.Marshal(serialized)
	if err != nil {
		b.logger.Errorw("Failed to marshal event",
			"event_type", event.Type,
			"error", err,
		)
		return
	}

	// Publish to Redis
	channel := b.getChannelName(event.Type)
	err = b.pubClient.Publish(b.ctx, channel, data).Err()
	if err != nil {
		b.logger.Errorw("Failed to publish event to Redis",
			"event_type", event.Type,
			"channel", channel,
			"error", err,
		)
		return
	}

	b.logger.Debugf("Successfully published event to Redis: %s", event.Type)
}

// Close closes all Redis subscriptions and cleans up resources
func (b *RedisEventBus) Close() error {
	b.logger.Info("Closing Redis event bus")

	// Cancel context to stop all subscriptions
	b.cancel()

	// Close all pub/sub subscriptions
	b.mu.Lock()
	for eventType, pubsub := range b.subscribers {
		if err := pubsub.Close(); err != nil {
			b.logger.Errorw("Failed to close subscription",
				"event_type", eventType,
				"error", err,
			)
		}
	}
	b.subscribers = make(map[events.EventType]*redis.PubSub)
	b.mu.Unlock()

	// Wait for all goroutines to finish
	b.wg.Wait()

	// Close the pub client
	if err := b.pubClient.Close(); err != nil {
		b.logger.Errorw("Failed to close pub client", "error", err)
	}

	b.logger.Info("Redis event bus closed")
	return nil
}

// getChannelName returns the Redis channel name for an event type
func (b *RedisEventBus) getChannelName(eventType events.EventType) string {
	return fmt.Sprintf("%s%s", RedisEventChannelPrefix, eventType)
}

// GetStats returns statistics about the event bus
func (b *RedisEventBus) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_event_types"] = len(b.handlers)
	stats["total_subscriptions"] = len(b.subscribers)

	handlerCounts := make(map[events.EventType]int)
	for eventType, handlers := range b.handlers {
		handlerCounts[eventType] = len(handlers)
	}
	stats["handler_counts"] = handlerCounts

	return stats
}

// ProvideRedisClient provides a Redis client
func ProvideRedisClient(cfg *config.Config, logger *zap.SugaredLogger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	logger.Info("Successfully created Redis client")
	return client, nil
}

// ProvideRedisEventBus creates and returns a Redis-based event bus
func ProvideRedisEventBus(client *redis.Client, logger *zap.SugaredLogger) events.EventBus {
	logger.Info("Creating Redis-based event bus")
	return NewRedisEventBus(client, logger)
}
