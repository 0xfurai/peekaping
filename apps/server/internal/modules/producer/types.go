package producer

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
)

// Producer is responsible for scheduling monitor health checks
type Producer struct {
	rdb                     *redis.Client
	queueService            queue.Service
	monitorService          monitor.Service
	proxyService            proxy.Service
	maintenanceService      maintenance.Service
	monitorNotificationSvc  monitor_notification.Service
	settingService          shared.SettingService
	logger                  *zap.SugaredLogger
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	mu                      sync.RWMutex
	monitorIntervals        map[string]int // monitor_id -> interval in seconds
	scheduleRefreshInterval time.Duration
	leaderElection          *LeaderElection
}
