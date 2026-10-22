package app

import (
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/worker"
	"peekaping/internal/utils"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// sqliteClusterDocsURL is the part of the SQLite page that explains cluster mode on SQLite
const sqliteClusterDocsURL = "https://docs.peekaping.com/self-hosting/docker-with-sqlite#cluster-mode"

// RegisterClusterAPI provides what cmd/api runs: the HTTP API and the websocket, with the
// stale monitors the producer's watchdog records in Redis. A push to a push URL is enqueued
// for the ingester. The api takes the notification events from the notification queue, which
// gives each to one replica, with its own asynq server.
func RegisterClusterAPI(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container,
			ingester.NewPushQueueService,
			infra.ProvideNotificationAsynqServer,
			notification_channel.NewQueueConsumer,
			provideClusterClaimer,
		),
		warnAboutSQLite(container, cfg),
	)
}

// provideClusterClaimer provides the claimer of the usage ping: of the api replicas, the first
// to set the key in Redis sends the day's ping
func provideClusterClaimer(client *redis.Client) telemetry.Claimer {
	return telemetry.NewRedisClaimer(client)
}

// RegisterClusterProducer provides what cmd/producer runs: the producer, which schedules the
// checks and enqueues them for the worker while it is the leader, and its stale-monitor watchdog
func RegisterClusterProducer(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container, producer.ProvideQueueRunner),
		producer.RegisterDependencies(container),
		warnAboutSQLite(container, cfg),
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
		warnAboutSQLite(container, cfg),
	)
}

// warnAboutSQLite logs one warning when the storage backend is SQLite, for the binaries that open
// the database: the api, the producer and the ingester. They run as separate processes on one
// file, and SQLite lets one process write at a time, so each waits for the others' writes. That
// works for a small install, but single mode runs all the roles in one process, and PostgreSQL
// takes concurrent writers, so either fits better. The worker has no database and doesn't log it.
// The warning comes before the database opens, so it is in the log when a startup check stops.
func warnAboutSQLite(container *dig.Container, cfg *config.Config) error {
	if cfg.DBType != "sqlite" {
		return nil
	}
	return container.Invoke(func(logger *zap.SugaredLogger) {
		logger.Warnf("SQLite in cluster mode: the api, producer and ingester share one database file "+
			"and write to it one at a time. Single mode (the SQLite bundle image) or PostgreSQL fits better. "+
			"See %s", sqliteClusterDocsURL)
	})
}

// registerCluster provides what every cluster binary shares, then cluster mode's transport: the
// Redis client, the event bus over Redis pub/sub with the notification topics sent through the
// notification queue, the stale monitors in a Redis hash, the run lock of the api's hourly cleanup
// in Redis, and asynq's client and inspector behind the queue service. The api, the worker and
// the ingester each add their own asynq server, since they serve different queues.
func registerCluster(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return utils.Provide(container,
		infra.ProvideRedisClient,
		infra.ProvideRedisEventBus,
		provideClusterEventBus,
		stale.ProvideRedisStore,
		cleanup.ProvideRedisRunLock,
		infra.ProvideAsynqClient,
		infra.ProvideAsynqInspector,
		infra.ProvideQueueService,
		provideClusterMode,
		provideClusterSwitch,
	)
}

// provideClusterSwitch provides the error reporting switch of the api: it also copies the admin's
// choice to Redis, where the other processes read it
func provideClusterSwitch(client *redis.Client) error_reporting.Switch {
	return error_reporting.NewRedisSwitch(client)
}

// provideClusterMode tells the usage ping that this install runs in cluster mode
func provideClusterMode() telemetry.Mode { return telemetry.ModeCluster }

// provideClusterEventBus provides the Redis event bus with the notification topics sent through
// the queue, as the events.EventBus every module publishes and subscribes on, and as the
// QueuedBus the api's QueueConsumer hands the queue's events to
func provideClusterEventBus(bus *events.Bus, q queue.Service) (events.EventBus, *notification_channel.QueuedBus) {
	queued := notification_channel.NewQueuedBus(bus, q)
	return queued, queued
}
