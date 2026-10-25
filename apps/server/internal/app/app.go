// Package app builds the dependency graph of the binaries that run the server, in two layers
// (ADR 0002, section 3). Register provides what both share: the config, the logger, the
// database, and the modules' repositories, services, check executors, listeners and HTTP
// routes. A transport then provides how the parts reach each other.
//
// Cluster mode's transport is in cluster.go: Redis, the event bus over it, the stale.Store and
// the cleanup.RunLock in it, asynq (for the notification queue) and the leader election, and the
// cluster side of ADR 0002's two seams. The probes run the checks, so the api ingests what it takes
// (a push, or the results of a probe) with ingest.NewInOrderService, counts the stats of what it
// stored, and runs the stale-monitor watchdog and the probe sweep under the leader lock.
// RegisterClusterAPI builds cmd/api's graph.
//
// Single mode's transport is in single.go: Register, then the in-memory event bus and
// stale.Store, the cleanup.RunLock that grants every hour, the scheduler and its watchdog, and
// one ingest.NewInOrderService as the ingester.Service that both the scheduler and the push
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

// Register provides what both binaries share. dig builds a value only when an Invoke needs it.
// The transport provides the rest: the event bus, the stale.Store, the ingester.Service the push
// URL ingests through, and the telemetry.Mode that says whether the usage ping reports single or
// cluster mode (the MODE setting does not).
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
		heartbeat.RegisterDependencies(container),
		monitor.RegisterDependencies(container),
		healthcheck.RegisterDependencies(container),
		check.RegisterDependencies(container),
		bruteforce.RegisterDependencies(container),
		auth.RegisterDependencies(container),
		notification_channel.RegisterDependencies(container),
		monitor_notification.RegisterDependencies(container),
		probe_notification.RegisterDependencies(container),
		probe.RegisterDependencies(container),
		protocol.RegisterDependencies(container),
		proxy.RegisterDependencies(container),
		setting.RegisterDependencies(container),
		notification_sent_history.RegisterDependencies(container),
		monitor_tls_info.RegisterDependencies(container),
		certificate.RegisterDependencies(container),
		stats.RegisterDependencies(container),
		monitor_maintenance.RegisterDependencies(container),
		maintenance.RegisterDependencies(container),
		status_page.RegisterDependencies(container),
		monitor_status_page.RegisterDependencies(container),
		domain_status_page.RegisterDependencies(container),
		tag.RegisterDependencies(container),
		monitor_tag.RegisterDependencies(container),
		badge.RegisterDependencies(container),
		api_key.RegisterDependencies(container),
		middleware.RegisterDependencies(container),
		metrics.RegisterDependencies(container),
		telemetry.RegisterDependencies(container),
		error_reporting.RegisterDependencies(container),
		utils.Provide(container, websocket.NewServer, internal.ProvideServer),
	)
}

// registerDatabase provides the storage backend DB_TYPE names. MongoDB storage was removed: a
// config that names it is refused already, and this refuses it again for a graph built from a
// Config that wasn't validated.
func registerDatabase(container *dig.Container, cfg *config.Config) error {
	if err := config.RefuseMongoStorage(cfg.DBType); err != nil {
		return err
	}
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		return utils.Provide(container, infra.ProvideSQLDB)
	default:
		return fmt.Errorf("unsupported DB_TYPE %q", cfg.DBType)
	}
}
