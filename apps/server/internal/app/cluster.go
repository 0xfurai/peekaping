package app

import (
	"context"
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/leader"
	"peekaping/internal/modules/cleanup"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/telemetry"
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
// The probes run the checks, so the api does the rest. It stores what it takes, a push to a push
// URL or the results of a probe, through ingest.NewInOrderService on the replica that took the
// request, as single mode does (see provideInOrderIngester), and aggregates the stats of the
// heartbeats it stored. And it runs the stale-monitor watchdog, the probe sweep and the timeout
// checks of push monitors on the one replica that holds the leader lock (LeaderJobs).
func RegisterClusterAPI(container *dig.Container, cfg *config.Config) error {
	return errors.Join(
		registerCluster(container, cfg),
		utils.Provide(container,
			infra.ProvideNotificationAsynqServer,
			notification_channel.NewQueueConsumer,
			provideClusterClaimer,
		),
		registerAPIStorage(container),
		warnAboutSQLite(container, cfg),
	)
}

// registerAPIStorage provides what the api does with its storage: it ingests, and runs the leader jobs
func registerAPIStorage(container *dig.Container) error {
	return utils.Provide(container,
		ingester.ProvideLocationStore,
		provideInOrderIngester,
		leader.ProvideElector,
		provideClusterWatchdog,
		provideAnnouncements,
		provideSilentWatch,
		probe.ProvideSilentSweep,
		providePushChecks,
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

// clusterWatchdogParams are what the api's stale-monitor watchdog is built from
type clusterWatchdogParams struct {
	dig.In

	Store        stale.Store
	Monitors     monitor.Service
	Heartbeats   heartbeat.Service
	EventBus     events.EventBus
	ProbeOffline stale.ProbeOfflineFunc
	Probes       stale.ProbeLookup
	Client       *redis.Client
	Logger       *zap.SugaredLogger
}

// config is the watchdog's configuration. Probes is what lets a stale monitor's notification name
// the probes that don't run its type, as single mode's does.
func (p clusterWatchdogParams) config() stale.WatchdogConfig {
	return stale.WatchdogConfig{
		Store:        p.Store,
		Monitors:     p.Monitors,
		Heartbeats:   p.Heartbeats,
		EventBus:     p.EventBus,
		Now:          stale.RedisClock(p.Client, p.Logger),
		CheckTimeout: scheduler.CheckTimeout,
		ProbeOffline: p.ProbeOffline,
		Probes:       p.Probes,
		Logger:       p.Logger.With("component", "watchdog"),
	}
}

// provideClusterWatchdog provides the stale-monitor watchdog of the api. No process of its own runs
// checks in cluster mode, so it has no rechecker: a stale monitor is recorded and notified as one
// on a remote probe is. Redis's clock is the one the replicas share, whichever holds the lock.
func provideClusterWatchdog(p clusterWatchdogParams) *stale.Watchdog {
	return stale.NewWatchdog(p.config())
}

// provideLeaderJobs provides the jobs that run on the api replica that holds the leader lock
func provideLeaderJobs(elector *leader.Elector, watchdog *stale.Watchdog, sweep *probe.Sweep, pushChecks *PushChecks) *LeaderJobs {
	return NewLeaderJobs(elector, []LeaderJob{
		{Name: "the stale-monitor watchdog", Run: watchdog.Run},
		{Name: "the probe sweep", Run: sweep.Run},
		{Name: "the push monitors' checks", Run: pushChecks.Run},
	})
}

// provideClusterClaimer provides the claimer of the usage ping: of the api replicas, the first
// to set the key in Redis sends the day's ping
func provideClusterClaimer(client *redis.Client) telemetry.Claimer {
	return telemetry.NewRedisClaimer(client)
}

// warnAboutSQLite logs one warning when the storage backend is SQLite. The api replicas of a
// cluster run as separate processes on one file, and SQLite lets one process write at a time, so
// each waits for the others' writes. That works for a small install, but single mode runs all the
// roles in one process, and PostgreSQL takes concurrent writers, so either fits better. The
// warning comes before the database opens, so it is in the log when a startup check stops.
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

// registerCluster provides what the api shares with the rest of the graph, then cluster mode's
// transport: the Redis client, the event bus over Redis pub/sub with the notification topics sent
// through the notification queue (provideAPIEventBus), the stale monitors in a Redis hash, the run
// lock of the api's hourly cleanup in Redis, and asynq's client behind the queue service. The api
// adds the asynq server that takes the notification queue (RegisterClusterAPI).
func registerCluster(container *dig.Container, cfg *config.Config) error {
	if err := Register(container, cfg); err != nil {
		return err
	}
	return utils.Provide(container,
		infra.ProvideRedisClient,
		infra.ProvideRedisEventBus,
		provideAPIEventBus,
		stale.ProvideRedisStore,
		cleanup.ProvideRedisRunLock,
		infra.ProvideAsynqClient,
		infra.ProvideQueueService,
		provideClusterMode,
		provideClusterWindow,
		provideClusterLocalProbe,
	)
}

// provideClusterLocalProbe provides how the local probe is known to be online in the cluster. No
// process of Peekaping's own runs checks there: peekaping-probe instances started with the local
// probe's token (PROBE_BOOTSTRAP_TOKEN) do, so the local probe is online while one of them is
// connected, like a remote probe.
func provideClusterLocalProbe() shared.LocalProbeLiveness { return shared.LocalProbeIsItsInstances }

// provideClusterWindow provides the duplicate window of the probe protocol's results route in
// Redis, so that the API replicas share it: a batch retried on another replica finds the ids of the
// results the first took
func provideClusterWindow(client *redis.Client) protocol.Window {
	return protocol.NewRedisWindow(client)
}

// provideClusterMode tells the usage ping that this install runs in cluster mode
func provideClusterMode() telemetry.Mode { return telemetry.ModeCluster }
