package internal

import (
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/bruteforce"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/websocket"

	"go.uber.org/dig"
)

// RegisterAPIDependencies provides everything the API server needs, so the
// API binary and the API tests build the same router.
func RegisterAPIDependencies(container *dig.Container, cfg *config.Config) error {
	container.Provide(func() *config.Config { return cfg })
	container.Provide(ProvideLogger)
	container.Provide(ProvideServer)
	container.Provide(websocket.NewServer)

	// database-specific deps
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		return fmt.Errorf("unsupported DB_TYPE %q", cfg.DBType)
	}

	// Provide Redis event bus
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// The stale monitors the producer's watchdog records
	container.Provide(stale.NewStore)

	// Provide queue infrastructure (for push endpoint)
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register dependencies in the correct order to handle circular dependencies
	heartbeat.RegisterDependencies(container, cfg)
	monitor.RegisterDependencies(container, cfg)
	healthcheck.RegisterDependencies(container)
	bruteforce.RegisterDependencies(container, cfg)
	auth.RegisterDependencies(container, cfg)
	notification_channel.RegisterDependencies(container, cfg)
	monitor_notification.RegisterDependencies(container, cfg)
	proxy.RegisterDependencies(container, cfg)
	setting.RegisterDependencies(container, cfg)
	notification_sent_history.RegisterDependencies(container, cfg)
	monitor_tls_info.RegisterDependencies(container, cfg)
	certificate.RegisterDependencies(container)
	stats.RegisterDependencies(container, cfg)
	monitor_maintenance.RegisterDependencies(container, cfg)
	maintenance.RegisterDependencies(container, cfg)
	status_page.RegisterDependencies(container, cfg)
	monitor_status_page.RegisterDependencies(container, cfg)
	domain_status_page.RegisterDependencies(container, cfg)
	tag.RegisterDependencies(container, cfg)
	monitor_tag.RegisterDependencies(container, cfg)
	badge.RegisterDependencies(container, cfg)
	queue.RegisterDependencies(container, cfg)
	api_key.RegisterDependencies(container, cfg)
	middleware.RegisterDependencies(container)

	return nil
}
