package infra

import (
	"fmt"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// NewRedisEventBus returns the event bus that spans processes: events go through Redis
// pub/sub, on the client the bus is given. Closing the bus leaves the client open.
func NewRedisEventBus(client *redis.Client, logger *zap.SugaredLogger, opts ...events.Option) *events.Bus {
	logger = logger.With("component", "redis_event_bus")
	opts = append([]events.Option{events.WithLogger(logger)}, opts...)
	return events.NewBus(newRedisTransport(client, logger), opts...)
}

// ProvideRedisClient provides a Redis client
func ProvideRedisClient(cfg *config.Config, logger *zap.SugaredLogger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	logger.Info("Successfully created Redis client")
	return client, nil
}

// ProvideRedisEventBus creates and returns a Redis-based event bus. Cluster mode provides the
// events.EventBus the modules use from it, with the notification topics sent through a queue.
func ProvideRedisEventBus(client *redis.Client, logger *zap.SugaredLogger) *events.Bus {
	logger.Info("Creating Redis-based event bus")
	return NewRedisEventBus(client, logger)
}
