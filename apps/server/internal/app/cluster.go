package app

import (
	"context"
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/leader"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
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

// RegisterClusterAPI provides what cmd/api runs: the HTTP API and the websocket. The api takes the
// notification events from the notification queue, which gives each to one replica, with its own
// asynq server.
//
// On SQLite and PostgreSQL the probes run the checks, so the api does the rest. It stores what it
// takes, a push to a push URL or the results of a probe, through ingester.NewInOrderService on the
// replica that took the request, as single mode does (see ingestOnTheAPI), and aggregates the stats
// of the heartbeats it stored. And it has the jobs the producer ran: the stale-monitor watchdog and
// the probe sweep, which run on the one replica that holds the leader lock (LeaderJobs).
//
// On MongoDB, which has no probes (ADR 0001), the producer, the worker and the ingester still run
// the checks and store their results, and the api only enqueues what it takes for the ingester.
func RegisterClusterAPI(container *dig.Container, cfg *config.Config) error {
	mongo := config.IsMongoDBType(cfg.DBType)
	eventBus := any(provideAPIEventBus)
	if mongo {
		eventBus = provideClusterEventBus
	}
	return errors.Join(
		registerCluster(container, cfg, eventBus),
		utils.Provide(container,
			infra.ProvideNotificationAsynqServer,
			notification_channel.NewQueueConsumer,
			provideClusterClaimer,
		),
		registerAPIStorage(container, mongo),
		warnAboutSQLite(container, cfg),
	)
}

// registerAPIStorage provides what the api does with the storage backend it runs on: on MongoDB,
// which has no probes, it enqueues a push for the ingester, and the producer elects the leader that
// runs the watchdog and the sweep; on SQLite and PostgreSQL it ingests, and runs the leader jobs.
func registerAPIStorage(container *dig.Container, mongo bool) error {
	if mongo {
		return utils.Provide(container, ingester.NewPushQueueService)
	}
	return utils.Provide(container,
		ingester.ProvideLocationStore,
		provideInOrderIngester,
		leader.ProvideElector,
		provideClusterWatchdog,
		provideAnnouncements,
		provideSilentWatch,
		probe.ProvideSilentSweep,
		provideLeaderJobs,
	)
}

// provideAnnouncements provides the record, in the Redis the api replicas share, of the probes the
// leader reported offline
func provideAnnouncements(client *redis.Client) probe.Announcements {
	return probe.NewRedisAnnouncements(client)
}

// provideSilentWatch provides what makes the sweep report the local probe offline when it has
// monitors to run and no instance. A cluster upgraded without deploying probes has such a local
// probe, which was never online, so the sweep would never see it go quiet. The monitors that need an
// instance are the active monitors of the local probe that run checks, which a push monitor does not.
func provideSilentWatch(source protocol.MonitorSource, announced probe.Announcements) *probe.SilentWatch {
	return &probe.SilentWatch{
		Monitors: func(ctx context.Context) (int, error) {
			monitors, err := source.FindActiveByProbe(ctx, probe.LocalID)
			if err != nil {
				return 0, err
			}
			return len(protocol.Dealable(monitors, nil)), nil
		},
		Announced: announced,
	}
}

// provideClusterWatchdog provides the stale-monitor watchdog of the api. No process of its own runs
// checks in cluster mode, so it has no rechecker: a stale monitor is recorded and notified as one
// on a remote probe is. Redis's clock is the one the replicas share, whichever holds the lock.
func provideClusterWatchdog(
	store stale.Store,
	monitors monitor.Service,
	heartbeats heartbeat.Service,
	eventBus events.EventBus,
	probeOffline stale.ProbeOfflineFunc,
	client *redis.Client,
	logger *zap.SugaredLogger,
) *stale.Watchdog {
	return stale.NewWatchdog(stale.WatchdogConfig{
		Store:        store,
		Monitors:     monitors,
		Heartbeats:   heartbeats,
		EventBus:     eventBus,
		Now:          stale.RedisClock(client, logger),
		CheckTimeout: worker.HealthCheckTaskTimeout,
		ProbeOffline: probeOffline,
		Logger:       logger.With("component", "watchdog"),
	})
}

// provideLeaderJobs provides the jobs that run on the api replica that holds the leader lock
func provideLeaderJobs(elector *leader.Elector, watchdog *stale.Watchdog, sweep *probe.Sweep) *LeaderJobs {
	return NewLeaderJobs(elector, []LeaderJob{
		{Name: "the stale-monitor watchdog", Run: watchdog.Run},
		{Name: "the probe sweep", Run: sweep.Run},
	})
}

// provideClusterClaimer provides the claimer of the usage ping: of the api replicas, the first
// to set the key in Redis sends the day's ping
func provideClusterClaimer(client *redis.Client) telemetry.Claimer {
	return telemetry.NewRedisClaimer(client)
}

// RegisterClusterProducer provides what cmd/producer runs on MongoDB, the only storage it still
// runs on: the producer, which schedules the checks and enqueues them for the worker while it is
// the leader, and its stale-monitor watchdog and the sweep that sets silent probes offline, which
// run beside it. On SQLite and PostgreSQL the api runs the watchdog and the sweep, and the probes
// run the checks.
func RegisterClusterProducer(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg, provideClusterEventBus),
		utils.Provide(container, producer.ProvideQueueRunner, probe.ProvideSweep),
		producer.RegisterDependencies(container),
		warnAboutSQLite(container, cfg),
	)
}

// RegisterClusterWorker provides what cmd/worker runs: the worker, which runs the checks it takes
// from the healthcheck queue and enqueues their results for the ingester. It has no database.
func RegisterClusterWorker(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg, provideClusterEventBus),
		utils.Provide(container, infra.ProvideAsynqServer, ingester.NewQueueService),
		worker.RegisterDependencies(container),
	)
}

// RegisterClusterIngester provides what cmd/ingester runs: the ingester, which stores the results
// it takes from the ingester queue as heartbeats
func RegisterClusterIngester(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg, provideClusterEventBus),
		utils.Provide(container, infra.ProvideIngesterAsynqServer),
		ingester.RegisterDependencies(container),
		warnAboutSQLite(container, cfg),
	)
}

// warnAboutSQLite logs one warning when the storage backend is SQLite, for the binaries that open
// the database. On SQLite that is the api (the producer and the ingester refuse to start on it, see
// RefuseOnSQLStorage), whose replicas run as separate processes on one file. SQLite lets one process
// write at a time, so each waits for the others' writes. That works for a small install, but single
// mode runs all the roles in one process, and PostgreSQL takes concurrent writers, so either fits
// better. The worker has no database and doesn't log it. The warning comes before the database
// opens, so it is in the log when a startup check stops.
func warnAboutSQLite(container *dig.Container, cfg *config.Config) error {
	if cfg.DBType != "sqlite" {
		return nil
	}
	return container.Invoke(func(logger *zap.SugaredLogger) {
		logger.Warnf("SQLite in cluster mode: the api replicas share one database file "+
			"and write to it one at a time. Single mode (the SQLite bundle image) or PostgreSQL fits better. "+
			"See %s", sqliteClusterDocsURL)
	})
}

// registerCluster provides what every cluster binary shares, then cluster mode's transport: the
// Redis client, the event bus over Redis pub/sub with the notification topics sent through the
// notification queue (eventBus is the constructor of the events.EventBus the binary uses, and of
// the QueuedBus under it), the stale monitors in a Redis hash, the run lock of the api's hourly cleanup
// in Redis, and asynq's client and inspector behind the queue service. The api, the worker and
// the ingester each add their own asynq server, since they serve different queues.
func registerCluster(container *dig.Container, cfg *config.Config, eventBus any) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return utils.Provide(container,
		infra.ProvideRedisClient,
		infra.ProvideRedisEventBus,
		eventBus,
		stale.ProvideRedisStore,
		cleanup.ProvideRedisRunLock,
		infra.ProvideAsynqClient,
		infra.ProvideAsynqInspector,
		infra.ProvideQueueService,
		provideClusterMode,
		provideClusterSwitch,
		provideClusterWindow,
		provideClusterLocalProbe(cfg),
	)
}

// provideClusterLocalProbe provides how the local probe is known to be online in the cluster. On
// SQLite and PostgreSQL no process of Peekaping's own runs checks: peekaping-probe instances
// started with the local probe's token (PROBE_BOOTSTRAP_TOKEN) do, so the local probe is online
// while one of them is connected, like a remote probe. On MongoDB, which has no probes, the
// producer, worker and ingester still run the checks, and the local probe is never offline.
func provideClusterLocalProbe(cfg *config.Config) func() shared.LocalProbeLiveness {
	if config.IsMongoDBType(cfg.DBType) {
		return func() shared.LocalProbeLiveness { return shared.LocalProbeIsTheProcess }
	}
	return func() shared.LocalProbeLiveness { return shared.LocalProbeIsItsInstances }
}

// provideClusterWindow provides the duplicate window of the probe protocol's results route in
// Redis, so that the API replicas share it: a batch retried on another replica finds the ids of the
// results the first took
func provideClusterWindow(client *redis.Client) protocol.Window {
	return protocol.NewRedisWindow(client)
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
