package healthcheck

import (
	"context"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/proxy"
	"sync"
	"time"

	"go.uber.org/zap"
)

type HealthCheckSupervisor struct {
	mu     sync.RWMutex
	active map[string]*task
	// monitorSvc     monitor.Service
	// maintenanceSvc maintenance.Service
	execRegistry *executor.ExecutorRegistry
	// heartbeatService   heartbeat.Service
	eventBus         events.EventBus
	logger           *zap.SugaredLogger
	maxJitterSeconds int64 // configurable jitter for testing
}

type task struct {
	cancel         context.CancelFunc
	done           chan struct{}
	intervalUpdate chan time.Duration
}

func NewHealthCheck(
	// monitorService monitor.Service,
	// maintenanceService maintenance.Service,
	// heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
	// proxyService proxy.Service,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		active: make(map[string]*task),
		// monitorSvc:     monitorService,
		// maintenanceSvc: maintenanceService,
		execRegistry:     execRegistry,
		eventBus:         eventBus,
		logger:           logger.With("service", "[healthcheck]"),
		maxJitterSeconds: 20, // default production jitter
	}
}

// NewHealthCheckWithJitter creates a supervisor with configurable jitter for testing
func NewHealthCheckWithJitter(
	// monitorService monitor.Service,
	// maintenanceService maintenance.Service,
	// heartbeatService heartbeat.Service,
	eventBus events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
	proxyService proxy.Service,
	maxJitterSeconds int64,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		active:           make(map[string]*task),
		execRegistry:     execRegistry,
		eventBus:         eventBus,
		logger:           logger.With("service", "[healthcheck]"),
		maxJitterSeconds: maxJitterSeconds,
	}
}

// isUnderMaintenance checks if a monitor is under maintenance
func (s *HealthCheckSupervisor) isUnderMaintenance(ctx context.Context, monitorID string) (bool, error) {
	// TODO: implement
	// maintenances, err := s.maintenanceSvc.GetMaintenancesByMonitorID(ctx, monitorID)
	// if err != nil {
	// 	return false, err
	// }

	// s.logger.Infof("Found %d maintenances for monitor %s", len(maintenances), monitorID)

	// for _, m := range maintenances {
	// 	underMaintenance, err := s.maintenanceSvc.IsUnderMaintenance(ctx, m)
	// 	if err != nil {
	// 		s.logger.Warnf("Failed to get maintenance status for maintenance %s: %v", m.ID, err)
	// 		continue
	// 	}

	// 	// If any maintenance is under-maintenance, the monitor is under maintenance
	// 	if underMaintenance {
	// 		return true, nil
	// 	}
	// }

	return false, nil
}
