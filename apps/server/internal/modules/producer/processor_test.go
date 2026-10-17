package producer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

var errTaskIDConflict = fmt.Errorf("%w: healthcheck:mon-1", queue.ErrTaskIDConflict)

// healthCheckOpts matches the enqueue options of a monitor's health check
func healthCheckOpts(monitorID string) interface{} {
	return mock.MatchedBy(func(opts *queue.EnqueueOptions) bool {
		return opts.Queue == "healthcheck" && opts.TaskID == "healthcheck:"+monitorID
	})
}

func TestIsUnderMaintenance(t *testing.T) {
	t.Run("monitor not under maintenance", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMaintenanceSvc := new(MockMaintenanceService)

		producer := &Producer{
			logger:             logger,
			maintenanceService: mockMaintenanceSvc,
		}

		ctx := context.Background()
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)

		result, err := producer.isUnderMaintenance(ctx, "mon-1")
		assert.NoError(t, err)
		assert.False(t, result)

		mockMaintenanceSvc.AssertExpectations(t)
	})

	t.Run("monitor under maintenance", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMaintenanceSvc := new(MockMaintenanceService)

		producer := &Producer{
			logger:             logger,
			maintenanceService: mockMaintenanceSvc,
		}

		ctx := context.Background()
		maintenances := []*maintenance.Model{
			{ID: "maint-1"},
		}

		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return(maintenances, nil)
		mockMaintenanceSvc.On("IsUnderMaintenance", ctx, maintenances[0]).Return(true, nil)

		result, err := producer.isUnderMaintenance(ctx, "mon-1")
		assert.NoError(t, err)
		assert.True(t, result)

		mockMaintenanceSvc.AssertExpectations(t)
	})

	t.Run("multiple maintenances, one active", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMaintenanceSvc := new(MockMaintenanceService)

		producer := &Producer{
			logger:             logger,
			maintenanceService: mockMaintenanceSvc,
		}

		ctx := context.Background()
		maintenances := []*maintenance.Model{
			{ID: "maint-1"},
			{ID: "maint-2"},
		}

		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return(maintenances, nil)
		mockMaintenanceSvc.On("IsUnderMaintenance", ctx, maintenances[0]).Return(false, nil)
		mockMaintenanceSvc.On("IsUnderMaintenance", ctx, maintenances[1]).Return(true, nil)

		result, err := producer.isUnderMaintenance(ctx, "mon-1")
		assert.NoError(t, err)
		assert.True(t, result)

		mockMaintenanceSvc.AssertExpectations(t)
	})

	t.Run("error getting maintenances", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMaintenanceSvc := new(MockMaintenanceService)

		producer := &Producer{
			logger:             logger,
			maintenanceService: mockMaintenanceSvc,
		}

		ctx := context.Background()
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return(nil, errors.New("database error"))

		result, err := producer.isUnderMaintenance(ctx, "mon-1")
		assert.Error(t, err)
		assert.False(t, result)

		mockMaintenanceSvc.AssertExpectations(t)
	})
}

func TestProcessMonitor(t *testing.T) {
	t.Run("successfully process active monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:             "mon-1",
			Name:           "Test Monitor",
			Type:           "http",
			Active:         true,
			Interval:       60,
			Timeout:        30,
			MaxRetries:     3,
			RetryInterval:  10,
			ResendInterval: 300,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.AnythingOfType("worker.HealthCheckTaskPayload"), mock.MatchedBy(func(opts *queue.EnqueueOptions) bool {
			// The task must outlive the check, or asynq fails it before the DOWN result is delivered
			return opts.TaskID == "healthcheck:mon-1" &&
				opts.Queue == "healthcheck" &&
				opts.MaxRetry == 0 &&
				opts.Retention == 0 &&
				opts.Timeout > 30*time.Second+healthcheck.ExecutorGrace
		})).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("skip inactive monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			logger:         logger,
			monitorService: mockMonitorSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "Inactive Monitor",
			Active:   false,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 0, interval)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			logger:         logger,
			monitorService: mockMonitorSvc,
		}

		ctx := context.Background()
		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(nil, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 0, interval)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("process monitor with proxy", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			proxyService:       mockProxySvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:             "mon-1",
			Name:           "Test Monitor with Proxy",
			Type:           "http",
			Active:         true,
			Interval:       60,
			Timeout:        30,
			MaxRetries:     3,
			RetryInterval:  10,
			ResendInterval: 300,
			ProxyId:        "proxy-1",
		}

		proxyModel := &proxy.Model{
			ID:       "proxy-1",
			Protocol: "http",
			Host:     "proxy.example.com",
			Port:     8080,
			Auth:     true,
			Username: "user",
			Password: "pass",
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockProxySvc.On("FindByID", ctx, "proxy-1").Return(proxyModel, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.Proxy != nil && payload.Proxy.ID == "proxy-1"
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockProxySvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("process monitor under maintenance", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "Monitor Under Maintenance",
			Type:     "http",
			Active:   true,
			Interval: 60,
		}

		maintenances := []*maintenance.Model{
			{ID: "maint-1"},
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return(maintenances, nil)
		mockMaintenanceSvc.On("IsUnderMaintenance", ctx, maintenances[0]).Return(true, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.IsUnderMaintenance == true
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("skip when the conflicting task can't be looked up", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "Test Monitor",
			Type:     "http",
			Active:   true,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.AnythingOfType("worker.HealthCheckTaskPayload"), healthCheckOpts("mon-1")).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(nil, errors.New("task not found"))

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err) // Duplicate errors are not considered errors
		assert.Equal(t, 60, interval)
		mockQueueSvc.AssertNotCalled(t, "DeleteTask", mock.Anything, mock.Anything, mock.Anything)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("error finding monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)

		producer := &Producer{
			logger:         logger,
			monitorService: mockMonitorSvc,
		}

		ctx := context.Background()
		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(nil, errors.New("database error"))

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.Error(t, err)
		assert.Equal(t, 0, interval)

		mockMonitorSvc.AssertExpectations(t)
	})

	t.Run("check cert expiry for http monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "HTTP Monitor",
			Type:     "http",
			Active:   true,
			Interval: 60,
			Config:   `{"check_cert_expiry": true}`,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == true
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("check cert expiry for tcp monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "TCP Monitor",
			Type:     "tcp",
			Active:   true,
			Interval: 60,
			Config:   `{"check_cert_expiry": true}`,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == true
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("check cert expiry for smtp monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "SMTP Monitor",
			Type:     "smtp",
			Active:   true,
			Interval: 60,
			Config:   `{"check_cert_expiry": true}`,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == true
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("don't check cert expiry for ping monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger:             logger,
			monitorService:     mockMonitorSvc,
			maintenanceService: mockMaintenanceSvc,
			queueService:       mockQueueSvc,
		}

		ctx := context.Background()
		mon := &monitor.Model{
			ID:       "mon-1",
			Name:     "Ping Monitor",
			Type:     "ping",
			Active:   true,
			Interval: 60,
		}

		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == false
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})
}

func TestEnqueueHealthCheck(t *testing.T) {
	ctx := context.Background()
	payload := worker.HealthCheckTaskPayload{MonitorID: "mon-1", ScheduledAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	opts := &queue.EnqueueOptions{Queue: "healthcheck", TaskID: "healthcheck:mon-1"}

	newProducer := func() (*Producer, *MockQueueService) {
		mockQueueSvc := new(MockQueueService)
		return &Producer{logger: zap.NewNop().Sugar(), queueService: mockQueueSvc}, mockQueueSvc
	}

	t.Run("enqueues when no task holds the ID", func(t *testing.T) {
		producer, mockQueueSvc := newProducer()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(&queue.TaskInfo{}, nil)

		enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
		assert.NoError(t, err)
		assert.True(t, enqueued)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("returns enqueue errors other than a conflict", func(t *testing.T) {
		producer, mockQueueSvc := newProducer()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errors.New("redis unavailable"))

		enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
		assert.Error(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNotCalled(t, "GetTaskInfo", mock.Anything, mock.Anything, mock.Anything)
	})

	inFlight := map[string]*queue.TaskInfo{
		"running check": {ID: "healthcheck:mon-1", State: "active"},
		"pending check": {ID: "healthcheck:mon-1", State: "pending"},
	}
	for name, existing := range inFlight {
		t.Run("skips a "+name, func(t *testing.T) {
			producer, mockQueueSvc := newProducer()
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
			mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(existing, nil)

			enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
			assert.NoError(t, err)
			assert.False(t, enqueued)
			mockQueueSvc.AssertNotCalled(t, "DeleteTask", mock.Anything, mock.Anything, mock.Anything)
			mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 1)
		})
	}

	t.Run("skips when the task finished in the meantime", func(t *testing.T) {
		producer, mockQueueSvc := newProducer()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(nil, fmt.Errorf("%w: healthcheck:mon-1", queue.ErrTaskNotFound))

		enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
		assert.NoError(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNotCalled(t, "DeleteTask", mock.Anything, mock.Anything, mock.Anything)
	})

	leftovers := map[string]*queue.TaskInfo{
		"archived task":  {ID: "healthcheck:mon-1", State: "archived", LastErr: "context deadline exceeded"},
		"completed task": {ID: "healthcheck:mon-1", State: "completed"},
		"retry task":     {ID: "healthcheck:mon-1", State: "retry"},
	}
	for name, existing := range leftovers {
		t.Run("replaces a "+name, func(t *testing.T) {
			producer, mockQueueSvc := newProducer()
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict).Once()
			mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(existing, nil)
			mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(nil)
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(&queue.TaskInfo{}, nil).Once()

			enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
			assert.NoError(t, err)
			assert.True(t, enqueued)
			mockQueueSvc.AssertExpectations(t)
		})
	}

	t.Run("skips when the leftover task can't be deleted", func(t *testing.T) {
		producer, mockQueueSvc := newProducer()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(&queue.TaskInfo{ID: "healthcheck:mon-1", State: "archived"}, nil)
		mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(errors.New("redis unavailable"))

		enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
		assert.NoError(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 1)
	})

	t.Run("skips when another producer enqueued first", func(t *testing.T) {
		producer, mockQueueSvc := newProducer()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(&queue.TaskInfo{ID: "healthcheck:mon-1", State: "archived"}, nil)
		mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(nil)

		enqueued, err := producer.enqueueHealthCheck(ctx, payload, opts)
		assert.NoError(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 2)
	})
}
