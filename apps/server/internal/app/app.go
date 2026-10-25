// Package app builds the dependency graph of each Peekaping binary, in two layers (ADR 0002,
// section 3). Register provides what every binary shares: the config, the logger, the
// database, and the modules' repositories, services, check executors, listeners and HTTP
// routes. A transport then provides how the parts reach each other.
//
// Cluster mode's transport is in cluster.go: Redis, the event bus over it, the stale.Store and
// the cleanup.RunLock in it, asynq and the leader election, and the cluster side of ADR 0002's
// two seams. On SQLite and PostgreSQL the probes run the checks, so the api ingests what it takes
// (a push, or the results of a probe) with ingester.NewInOrderService, counts the stats of what it
// stored, and runs the stale-monitor watchdog and the probe sweep under the leader lock. On
// MongoDB, which has no probes, "run a check" (check.Runner) enqueues the producer's checks for the
// worker, and "ingest a result" (ingester.Service) enqueues the worker's and the push URL's
// results for the ingester. Its RegisterCluster functions build each cluster binary's graph.
//
// Single mode's transport is in single.go: Register, then the in-memory event bus and
// stale.Store, the cleanup.RunLock that grants every hour, the scheduler and its watchdog, and
// one ingester.NewInOrderService as the ingester.Service that both the scheduler and the push
// URL ingest through. RegisterSingle builds cmd/peekaping's graph.
//
// Every provide is checked, so a refused one stops startup with an error that names its
// constructor.
package app

import (
	"errors"
	"fmt"

	"peekaping/internal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/bruteforce"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/metrics"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/telemetry"
	"peekaping/internal/modules/websocket"
	"peekaping/internal/utils"

	"go.uber.org/dig"
)

// Register provides what every binary shares. dig builds a value only when an Invoke needs
// it, so each binary builds just the part of the graph it runs: the worker, which has no
// database, never builds a repository. The transport provides the rest: the event bus, the
// stale.Store, the ingester.Service the push URL ingests through, and the telemetry.Mode that
// says whether the usage ping reports single or cluster mode (the MODE setting does not),
// and the error_reporting.Switch that tells the processes that report whether to.
func Register(container *dig.Container, cfg *config.Config) error {
	err := utils.Provide(container,
		func() *config.Config { return cfg },
		internal.ProvideLogger,
		internal.ProvideTrustedProxies,
		internal.ProvideCORSOrigins,
		provideProbeLookup,
		provideStaleProbeLookup,
	)
	if err != nil {
		return err
	}
	if err := registerDatabase(container, cfg); err != nil {
		return err
	}

	return errors.Join(
		heartbeat.RegisterDependencies(container, cfg),
		monitor.RegisterDependencies(container, cfg),
		healthcheck.RegisterDependencies(container),
		check.RegisterDependencies(container),
		bruteforce.RegisterDependencies(container, cfg),
		auth.RegisterDependencies(container, cfg),
		notification_channel.RegisterDependencies(container, cfg),
		monitor_notification.RegisterDependencies(container, cfg),
		probe_notification.RegisterDependencies(container, cfg),
		probe.RegisterDependencies(container, cfg),
		protocol.RegisterDependencies(container),
		proxy.RegisterDependencies(container, cfg),
		setting.RegisterDependencies(container, cfg),
		notification_sent_history.RegisterDependencies(container, cfg),
		monitor_tls_info.RegisterDependencies(container, cfg),
		certificate.RegisterDependencies(container),
		stats.RegisterDependencies(container, cfg),
		monitor_maintenance.RegisterDependencies(container, cfg),
		maintenance.RegisterDependencies(container, cfg),
		status_page.RegisterDependencies(container, cfg),
		monitor_status_page.RegisterDependencies(container, cfg),
		domain_status_page.RegisterDependencies(container, cfg),
		tag.RegisterDependencies(container, cfg),
		monitor_tag.RegisterDependencies(container, cfg),
		badge.RegisterDependencies(container, cfg),
		api_key.RegisterDependencies(container, cfg),
		middleware.RegisterDependencies(container),
		metrics.RegisterDependencies(container),
		telemetry.RegisterDependencies(container),
		error_reporting.RegisterDependencies(container),
		utils.Provide(container, websocket.NewServer, internal.ProvideServer),
	)
}

// registerDatabase provides the storage backend DB_TYPE names. The worker has no DB_TYPE and
// gets no database.
func registerDatabase(container *dig.Container, cfg *config.Config) error {
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		return utils.Provide(container, infra.ProvideSQLDB)
	case "mongo", "mongodb":
		return utils.Provide(container, infra.ProvideMongoDB)
	case "":
		return nil
	default:
		return fmt.Errorf("unsupported DB_TYPE %q", cfg.DBType)
	}
}
