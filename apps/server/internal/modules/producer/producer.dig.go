package producer

import (
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/queue"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers producer dependencies in the DI container
func RegisterDependencies(container *dig.Container) {
	// Provide Redis client for leader election
	container.Provide(ProvideRedisClient)

	// Provide leader election
	container.Provide(ProvideLeaderElection)

	// Provide monitor scheduler
	container.Provide(ProvideMonitorScheduler)

	// Provide event listener
	container.Provide(ProvideEventListener)

	// Provide producer
	container.Provide(ProvideProducer)
}

// ProvideRedisClient provides a Redis client for the producer
func ProvideRedisClient(cfg *config.Config, logger *zap.SugaredLogger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	logger.Info("Successfully created Redis client for producer")
	return client, nil
}

// ProvideLeaderElection provides a leader election instance
func ProvideLeaderElection(client *redis.Client, logger *zap.SugaredLogger) *LeaderElection {
	// Generate a unique node ID
	nodeID := uuid.New().String()
	return NewLeaderElection(client, nodeID, logger)
}

// ProvideMonitorScheduler provides a monitor scheduler
func ProvideMonitorScheduler(
	monitorService monitor.Service,
	queueService queue.Service,
	logger *zap.SugaredLogger,
) *MonitorScheduler {
	return NewMonitorScheduler(monitorService, queueService, logger)
}

// ProvideEventListener provides an event listener
func ProvideEventListener(
	scheduler *MonitorScheduler,
	logger *zap.SugaredLogger,
) *EventListener {
	return NewEventListener(scheduler, logger)
}

// ProvideProducer provides a producer
func ProvideProducer(
	election *LeaderElection,
	scheduler *MonitorScheduler,
	eventListener *EventListener,
	logger *zap.SugaredLogger,
) *Producer {
	return NewProducer(election, scheduler, eventListener, logger)
}

// SubscribeToEvents subscribes the event listener to the event bus
func SubscribeToEvents(listener *EventListener, eventBus *events.EventBus) {
	listener.Subscribe(eventBus)
}
