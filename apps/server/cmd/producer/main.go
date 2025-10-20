package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/producer"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/tag"
	"peekaping/internal/version"
	"syscall"
	"time"

	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	log.Printf("Starting Peekaping Producer v%s", version.Version)

	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("Failed to load and validate Producer config: %v", err)
	}

	os.Setenv("TZ", cfg.Timezone)

	container := dig.New()

	internalCfg := cfg.ToInternalConfig()

	container.Provide(func() *config.Config { return internalCfg })

	container.Provide(func(cfg *config.Config) (*zap.SugaredLogger, error) {
		var zapLogger *zap.Logger
		var err error

		if cfg.Mode == "prod" {
			zapLogger, err = zap.NewProduction()
		} else {
			cfg := zap.NewDevelopmentConfig()
			cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel) // filter out Debug
			cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoder(func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
				enc.AppendString("[" + t.Format("15:04:05.000") + "]")
			})
			cfg.EncoderConfig.LevelKey = "" // remove level
			cfg.EncoderConfig.CallerKey = ""
			zapLogger, err = cfg.Build()
		}

		if err != nil {
			return nil, err
		}

		return zapLogger.Sugar(), nil
	})

	switch internalCfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		container.Provide(infra.ProvideSQLDB)
	case "mongo", "mongodb":
		container.Provide(infra.ProvideMongoDB)
	default:
		log.Fatalf("Unsupported DB_TYPE: %s", internalCfg.DBType)
	}

	// Provide Redis infrastructure
	container.Provide(infra.ProvideRedisClient)
	container.Provide(infra.ProvideRedisEventBus)

	// Provide queue infrastructure
	container.Provide(infra.ProvideAsynqClient)
	container.Provide(infra.ProvideAsynqInspector)
	container.Provide(infra.ProvideQueueService)

	// Register module dependencies that producer needs
	heartbeat.RegisterDependencies(container, internalCfg)
	healthcheck.RegisterDependencies(container) // Provides ExecutorRegistry
	tag.RegisterDependencies(container, internalCfg)
	monitor_tag.RegisterDependencies(container, internalCfg)
	monitor.RegisterDependencies(container, internalCfg)
	proxy.RegisterDependencies(container, internalCfg)
	maintenance.RegisterDependencies(container, internalCfg)
	monitor_maintenance.RegisterDependencies(container, internalCfg)
	monitor_notification.RegisterDependencies(container, internalCfg)
	setting.RegisterDependencies(container, internalCfg)
	notification_sent_history.RegisterDependencies(container, internalCfg)
	monitor_tls_info.RegisterDependencies(container, internalCfg)
	certificate.RegisterDependencies(container)
	stats.RegisterDependencies(container, internalCfg)

	// Register producer dependencies
	producer.RegisterDependencies(container)

	// Start the producer
	err = container.Invoke(func(
		prod *producer.Producer,
		eventListener *producer.EventListener,
		eventBus events.EventBus,
		logger *zap.SugaredLogger,
	) error {
		eventListener.Subscribe(eventBus)
		logger.Info("Event listener subscribed to monitor events")

		// Start the producer
		if err := prod.Start(); err != nil {
			return fmt.Errorf("failed to start producer: %w", err)
		}

		logger.Info("Producer started successfully")

		// Wait for termination signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		logger.Info("Producer is running. Press Ctrl+C to stop.")
		<-sigChan

		logger.Info("Shutdown signal received, stopping producer...")
		prod.Stop()

		// Close event bus
		if err := eventBus.Close(); err != nil {
			logger.Errorw("Failed to close event bus", "error", err)
		}

		logger.Info("Producer stopped gracefully")

		return nil
	})

	if err != nil {
		log.Fatalf("Producer error: %v", err)
	}
}
