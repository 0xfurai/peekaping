package healthcheck

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"peekaping/src/modules/events"
	"peekaping/src/modules/healthcheck/executor"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/maintenance"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/proxy"
	"sync"
	"time"

	"go.uber.org/zap"
)

type HealthCheckSupervisor struct {
	mu               sync.RWMutex
	active           map[string]*task
	monitorSvc       monitor.Service
	maintenanceSvc   maintenance.Service
	execRegistry     *executor.ExecutorRegistry
	heartbeatService heartbeat.Service
	eventBus         *events.EventBus
	logger           *zap.SugaredLogger
	proxyService     proxy.Service
	maxJitterSeconds int64 // configurable jitter for testing
}

type task struct {
	cancel         context.CancelFunc
	done           chan struct{}
	intervalUpdate chan time.Duration
}

func NewHealthCheck(
	monitorService monitor.Service,
	maintenanceService maintenance.Service,
	heartbeatService heartbeat.Service,
	eventBus *events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
	proxyService proxy.Service,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		active:           make(map[string]*task),
		monitorSvc:       monitorService,
		maintenanceSvc:   maintenanceService,
		execRegistry:     execRegistry,
		heartbeatService: heartbeatService,
		eventBus:         eventBus,
		logger:           logger.With("service", "[healthcheck]"),
		proxyService:     proxyService,
		maxJitterSeconds: 20, // default production jitter
	}
}

// NewHealthCheckWithJitter creates a supervisor with configurable jitter for testing
func NewHealthCheckWithJitter(
	monitorService monitor.Service,
	maintenanceService maintenance.Service,
	heartbeatService heartbeat.Service,
	eventBus *events.EventBus,
	execRegistry *executor.ExecutorRegistry,
	logger *zap.SugaredLogger,
	proxyService proxy.Service,
	maxJitterSeconds int64,
) *HealthCheckSupervisor {
	return &HealthCheckSupervisor{
		active:           make(map[string]*task),
		monitorSvc:       monitorService,
		maintenanceSvc:   maintenanceService,
		execRegistry:     execRegistry,
		heartbeatService: heartbeatService,
		eventBus:         eventBus,
		logger:           logger.With("service", "[healthcheck]"),
		proxyService:     proxyService,
		maxJitterSeconds: maxJitterSeconds,
	}
}

func (s *HealthCheckSupervisor) StartAll(ctx context.Context) error {
	s.logger.Info("Start health check module")
	// Get all active monitors
	monitors, err := s.monitorSvc.FindActive(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active monitors: %w", err)
	}
	s.logger.Infof("Found active monitors: %d", len(monitors))

	// Start monitoring for each active monitor
	for _, m := range monitors {
		if err := s.StartMonitor(ctx, m, true); err != nil {
			log.Printf("Failed to start monitor %s: %v", m.ID, err)
		}
	}

	return nil
}

func (s *HealthCheckSupervisor) StartMonitor(
	ctx context.Context,
	m *Monitor,
	withJitter bool,
) error {
	s.logger.Infof("StartMonitor health check module for monitor: %s", m.ID)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop previous copy if it exists
	if t, ok := s.active[m.ID]; ok {
		t.cancel()
		<-t.done
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	intervalUpdate := make(chan time.Duration, 1)

	// Fetch proxy once here
	var proxyModel *proxy.Model = nil
	if m.ProxyId != "" {
		p, err := s.proxyService.FindByID(ctx, m.ProxyId)
		if err != nil {
			s.logger.Errorf("Failed to fetch proxy for monitor %s: %v", m.ID, err)
		} else if p != nil {
			proxyModel = p
		}
	}

	go func() {
		defer close(done)
		interval := time.Duration(m.Interval) * time.Second

		// Add random jitter before starting the loop
		if withJitter && s.maxJitterSeconds > 0 {
			jitter := time.Duration(rand.Int63n(int64(s.maxJitterSeconds))) * time.Second
			time.Sleep(jitter)
		}

		// Get the appropriate executor for this monitor type
		s.logger.Infof("Getting executor for monitor type: %s", m.Type)
		executor, ok := s.execRegistry.GetExecutor(m.Type)
		if !ok {
			s.logger.Errorf("executor not found for monitor type: %s", m.Type)
			return
		}

		// Run once immediately
		go s.handleMonitorTick(ctx, m, executor, proxyModel, func(newInterval time.Duration) {
			intervalUpdate <- newInterval
		})

		for {
			select {
			case <-time.After(interval):
				go s.handleMonitorTick(ctx, m, executor, proxyModel, func(newInterval time.Duration) {
					intervalUpdate <- newInterval
				})
			case newInterval := <-intervalUpdate:
				interval = newInterval
			case <-ctx.Done():
				return
			}
		}
	}()

	s.active[m.ID] = &task{cancel: cancel, done: done, intervalUpdate: intervalUpdate}
	return nil
}

func (s *HealthCheckSupervisor) DeleteMonitor(monitorId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.active[monitorId]; ok {
		t.cancel()
		<-t.done
		delete(s.active, monitorId)
	}
}

func (s *HealthCheckSupervisor) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.active {
		t.cancel()
		<-t.done
		delete(s.active, id)
	}
}

// isUnderMaintenance checks if a monitor is under maintenance
func (s *HealthCheckSupervisor) isUnderMaintenance(ctx context.Context, monitorID string) (bool, error) {
	maintenances, err := s.maintenanceSvc.GetMaintenancesByMonitorID(ctx, monitorID)
	if err != nil {
		return false, err
	}

	s.logger.Infof("Found %d maintenances for monitor %s", len(maintenances), monitorID)

	for _, m := range maintenances {
		underMaintenance, err := s.maintenanceSvc.IsUnderMaintenance(ctx, m)
		if err != nil {
			s.logger.Warnf("Failed to get maintenance status for maintenance %s: %v", m.ID, err)
			continue
		}

		// If any maintenance is under-maintenance, the monitor is under maintenance
		if underMaintenance {
			return true, nil
		}
	}

	return false, nil
}
