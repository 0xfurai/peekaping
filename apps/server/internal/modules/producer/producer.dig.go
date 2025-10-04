package producer

import (
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers producer dependencies in the DI container
func RegisterDependencies(container *dig.Container) {
	// Note: Redis client is provided by infra.ProvideRedisClient

	// Provide leader election
	container.Provide(ProvideLeaderElection)

	// Provide monitor scheduler
	container.Provide(ProvideMonitorScheduler)

	// Provide event listener
	container.Provide(ProvideEventListener)

	// Provide producer
	container.Provide(ProvideProducer)
}

// ProvideLeaderElection provides a leader election instance
func ProvideLeaderElection(client *redis.Client, logger *zap.SugaredLogger) *LeaderElection {
	// Generate a unique node ID
	nodeID := uuid.New().String()
	return NewLeaderElection(client, nodeID, logger)
}

// ProvideMonitorScheduler provides a monitor scheduler
func ProvideMonitorScheduler(
	scheduler *asynq.Scheduler,
	monitorService monitor.Service,
	proxyService proxy.Service,
	maintenanceService maintenance.Service,
	logger *zap.SugaredLogger,
) *MonitorScheduler {
	return NewMonitorScheduler(scheduler, monitorService, proxyService, maintenanceService, logger)
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

// SubscribeToEvents subscribes the event listener to the Redis event bus
func SubscribeToEvents(listener *EventListener, eventBus events.EventBus) {
	listener.Subscribe(eventBus)
}
