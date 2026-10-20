package app

import (
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/worker"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// RegisterClusterAPI provides what cmd/api runs: the HTTP API and the websocket, with the
// stale monitors the producer's watchdog records in Redis. A push to a push URL is enqueued
// for the ingester.
func RegisterClusterAPI(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container, ingester.NewPushQueueService),
	)
}

// RegisterClusterProducer provides what cmd/producer runs: the producer, which schedules the
// checks and enqueues them for the worker while it is the leader, and its stale-monitor watchdog
func RegisterClusterProducer(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		producer.RegisterDependencies(container),
	)
}

// RegisterClusterWorker provides what cmd/worker runs: the worker, which runs the checks it takes
// from the healthcheck queue and enqueues their results for the ingester. It has no database.
func RegisterClusterWorker(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container, infra.ProvideAsynqServer, ingester.NewQueueService),
		worker.RegisterDependencies(container),
	)
}

// RegisterClusterIngester provides what cmd/ingester runs: the ingester, which stores the results
// it takes from the ingester queue as heartbeats
func RegisterClusterIngester(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container, infra.ProvideIngesterAsynqServer),
		ingester.RegisterDependencies(container),
	)
}

// registerCluster provides what every cluster binary shares, then cluster mode's transport: the
// Redis client, the event bus over Redis pub/sub, the stale monitors in a Redis hash, and asynq's
// client and inspector behind the queue service. The worker and the ingester each add their own
// asynq server, since they serve different queues.
func registerCluster(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return utils.Provide(container,
		infra.ProvideRedisClient,
		infra.ProvideRedisEventBus,
		stale.ProvideRedisStore,
		infra.ProvideAsynqClient,
		infra.ProvideAsynqInspector,
		infra.ProvideQueueService,
	)
}
