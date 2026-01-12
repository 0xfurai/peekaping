package producer

import (
	"context"
	"errors"
	"testing"

	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/queue"
	"vigi/internal/modules/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.AnythingOfType("worker.HealthCheckTaskPayload"), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)

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
		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(nil, nil)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockProxySvc.On("FindByID", ctx, "proxy-1", "").Return(proxyModel, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.Proxy != nil && payload.Proxy.ID == "proxy-1"
		}), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return(maintenances, nil)
		mockMaintenanceSvc.On("IsUnderMaintenance", ctx, maintenances[0]).Return(true, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.IsUnderMaintenance == true
		}), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("handle duplicate task error", func(t *testing.T) {
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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.AnythingOfType("worker.HealthCheckTaskPayload"), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(nil, errors.New("task ID conflicts with existing task"))

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err) // Duplicate errors are not considered errors
		assert.Equal(t, 60, interval)

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
		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(nil, errors.New("database error"))

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == true
		}), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == true
		}), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

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

		mockMonitorSvc.On("FindByID", ctx, "mon-1", "").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockQueueSvc.On("EnqueueUnique", ctx, worker.TaskTypeHealthCheck, mock.MatchedBy(func(payload worker.HealthCheckTaskPayload) bool {
			return payload.CheckCertExpiry == false
		}), "healthcheck:mon-1", mock.AnythingOfType("time.Duration"), mock.AnythingOfType("*queue.EnqueueOptions")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})
}
