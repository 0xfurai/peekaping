package producer

import (
	"context"
	"fmt"
	"time"

	"vigi/internal/config"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/queue"
	"vigi/internal/modules/shared"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// NewProducer creates a new producer instance
func NewProducer(
	rdb *redis.Client,
	queueService queue.Service,
	monitorService monitor.Service,
	proxyService proxy.Service,
	maintenanceService maintenance.Service,
	monitorNotificationSvc monitor_notification.Service,
	settingService shared.SettingService,
	heartbeatService heartbeat.Service,
	leaderElection *LeaderElection,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *Producer {
	ctx, cancel := context.WithCancel(context.Background())

	// Use configured concurrency, fallback to constant if not set
	concurrency := cfg.ProducerConcurrency
	if concurrency <= 0 {
		concurrency = ConcurrentProducers
	}

	return &Producer{
		rdb:                     rdb,
		queueService:            queueService,
		monitorService:          monitorService,
		proxyService:            proxyService,
		maintenanceService:      maintenanceService,
		monitorNotificationSvc:  monitorNotificationSvc,
		settingService:          settingService,
		heartbeatService:        heartbeatService,
		logger:                  logger.With("component", "producer"),
		ctx:                     ctx,
		cancel:                  cancel,
		monitorIntervals:        make(map[string]int),
		scheduleRefreshInterval: 30 * time.Second, // Refresh schedule every 30 seconds
		leaderElection:          leaderElection,
		concurrency:             concurrency,
	}
}

// Start starts the producer with leader election
func (p *Producer) Start() error {
	p.logger.Info("Starting producer with leader election")

	p.leaderElection.Start(p.ctx)

	if err := p.startJobProcessing(); err != nil {
		return fmt.Errorf("failed to start job processing: %w", err)
	}

	// Start a goroutine to monitor leadership changes for monitor syncing
	p.wg.Add(1)
	go p.runLeadershipMonitor()

	p.logger.Info("Producer started successfully")
	return nil
}

// Stop stops the producer gracefully
func (p *Producer) Stop() {
	p.logger.Info("Stopping producer")
	p.cancel()
	p.wg.Wait()
	p.logger.Info("Producer stopped")
}
