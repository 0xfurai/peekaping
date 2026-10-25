package producer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/healthcheck/supervisor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

var errTaskIDConflict = fmt.Errorf("%w: healthcheck:mon-1", queue.ErrTaskIDConflict)

// scheduledAt is the time processMonitor gets as 1234567890 ms.
var scheduledAt = time.Date(1970, 1, 15, 6, 56, 7, 890_000_000, time.UTC)

// mockHeartbeatService stubs the heartbeat lookup that push monitors make.
type mockHeartbeatService struct {
	heartbeat.Service
	mock.Mock
}

func (m *mockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

// healthCheckOpts matches the enqueue options of a monitor's health check
func healthCheckOpts(monitorID string) interface{} {
	return mock.MatchedBy(func(opts *queue.EnqueueOptions) bool {
		return opts.Queue == "healthcheck" && opts.TaskID == "healthcheck:"+monitorID
	})
}

func TestProcessMonitor(t *testing.T) {
	t.Run("successfully process active monitor", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, worker.HealthCheckTaskPayload{
			MonitorID:      "mon-1",
			MonitorName:    "Test Monitor",
			MonitorType:    "http",
			Interval:       60,
			Timeout:        30,
			MaxRetries:     3,
			RetryInterval:  10,
			ResendInterval: 300,
			ScheduledAt:    scheduledAt,
		}, mock.MatchedBy(func(opts *queue.EnqueueOptions) bool {
			// The task must outlive the check, or asynq fails it before the DOWN result is delivered
			return opts.TaskID == "healthcheck:mon-1" &&
				opts.Queue == "healthcheck" &&
				opts.MaxRetry == 0 &&
				opts.Retention == 0 &&
				opts.Timeout > 30*time.Second+supervisor.ExecutorGrace
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, nil, nil, logger),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, nil, nil, logger),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, mockProxySvc, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
			return assert.ObjectsAreEqual(&worker.ProxyData{
				ID:       "proxy-1",
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
				Auth:     true,
				Username: "user",
				Password: "pass",
			}, payload.Proxy)
		}), healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)

		mockMonitorSvc.AssertExpectations(t)
		mockMaintenanceSvc.AssertExpectations(t)
		mockProxySvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("checks without the proxy when it can't be loaded", func(t *testing.T) {
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockProxySvc := new(MockProxyService)
		mockQueueSvc := new(MockQueueService)
		producer := &Producer{
			logger: zap.NewNop().Sugar(),
			loader: check.NewLoader(mockMonitorSvc, mockProxySvc, mockMaintenanceSvc, nil, zap.NewNop().Sugar()),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
		}

		ctx := context.Background()
		mon := &monitor.Model{ID: "mon-1", Name: "Behind a proxy", Type: "http", Active: true, Interval: 60, Timeout: 30, ProxyId: "proxy-1"}
		mockMonitorSvc.On("FindByID", ctx, "mon-1").Return(mon, nil)
		mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-1").Return([]*maintenance.Model{}, nil)
		mockProxySvc.On("FindByID", ctx, "proxy-1").Return(nil, errors.New("proxy not found"))
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, worker.HealthCheckTaskPayload{
			MonitorID:   "mon-1",
			MonitorName: "Behind a proxy",
			MonitorType: "http",
			Interval:    60,
			Timeout:     30,
			ScheduledAt: scheduledAt,
		}, healthCheckOpts("mon-1")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

		interval, err := producer.processMonitor(ctx, "mon-1", 1234567890)
		assert.NoError(t, err)
		assert.Equal(t, 60, interval)
		mockProxySvc.AssertExpectations(t)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("push monitor", func(t *testing.T) {
		mon := &monitor.Model{ID: "mon-push", Name: "Nightly backup", Type: "push", Active: true, Interval: 3600, Timeout: 30}
		latest := &heartbeat.Model{
			ID:        "hb-9",
			MonitorID: "mon-push",
			Status:    shared.MonitorStatusUp,
			Msg:       "pushed",
			Time:      time.Date(1970, 1, 15, 6, 0, 0, 0, time.UTC),
		}

		for name, tc := range map[string]struct {
			latest []*heartbeat.Model
			err    error
			want   *heartbeat.Model
		}{
			"carries its latest heartbeat":             {latest: []*heartbeat.Model{latest}, want: latest},
			"checks without one when none exists":      {latest: []*heartbeat.Model{}},
			"checks without one when it can't be read": {err: errors.New("database is locked")},
		} {
			t.Run(name, func(t *testing.T) {
				mockMonitorSvc := new(MockMonitorService)
				mockMaintenanceSvc := new(MockMaintenanceService)
				mockHeartbeatSvc := new(mockHeartbeatService)
				mockQueueSvc := new(MockQueueService)
				producer := &Producer{
					logger: zap.NewNop().Sugar(),
					loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, mockHeartbeatSvc, zap.NewNop().Sugar()),
					runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
				}

				ctx := context.Background()
				mockMonitorSvc.On("FindByID", ctx, "mon-push").Return(mon, nil)
				mockMaintenanceSvc.On("GetMaintenancesByMonitorID", ctx, "mon-push").Return([]*maintenance.Model{}, nil)
				// The newest heartbeat: one per page, first page, newest first
				mockHeartbeatSvc.On("FindByMonitorIDPaginated", ctx, "mon-push", 1, 0, (*bool)(nil), false).Return(tc.latest, tc.err)
				mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, worker.HealthCheckTaskPayload{
					MonitorID:     "mon-push",
					MonitorName:   "Nightly backup",
					MonitorType:   "push",
					Interval:      3600,
					Timeout:       30,
					LastHeartbeat: tc.want,
					ScheduledAt:   scheduledAt,
				}, healthCheckOpts("mon-push")).Return(&queue.TaskInfo{ID: "task-123"}, nil)

				interval, err := producer.processMonitor(ctx, "mon-push", 1234567890)
				assert.NoError(t, err)
				assert.Equal(t, 3600, interval)
				mockHeartbeatSvc.AssertExpectations(t)
				mockQueueSvc.AssertExpectations(t)
			})
		}
	})

	t.Run("process monitor under maintenance", func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		mockMonitorSvc := new(MockMonitorService)
		mockMaintenanceSvc := new(MockMaintenanceService)
		mockQueueSvc := new(MockQueueService)

		producer := &Producer{
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, worker.HealthCheckTaskPayload{
			MonitorID:   "mon-1",
			MonitorName: "Test Monitor",
			MonitorType: "http",
			Interval:    60,
			ScheduledAt: scheduledAt,
		}, healthCheckOpts("mon-1")).Return(nil, errTaskIDConflict)
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, nil, nil, logger),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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
			logger: logger,
			loader: check.NewLoader(mockMonitorSvc, nil, mockMaintenanceSvc, nil, logger),
			runner: NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()),
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

func TestQueueRunner_Run(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	job := &check.Job{Monitor: &monitor.Model{ID: "mon-1"}}
	payload := worker.HealthCheckTaskPayload{MonitorID: "mon-1", ScheduledAt: at}
	opts := &queue.EnqueueOptions{Queue: "healthcheck", TaskID: "healthcheck:mon-1", Timeout: worker.HealthCheckTaskTimeout(0)}

	newRunner := func() (check.Runner, *MockQueueService) {
		mockQueueSvc := new(MockQueueService)
		return NewQueueRunner(mockQueueSvc, zap.NewNop().Sugar()), mockQueueSvc
	}

	t.Run("enqueues when no task holds the ID", func(t *testing.T) {
		runner, mockQueueSvc := newRunner()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(&queue.TaskInfo{}, nil)

		enqueued, err := runner.Run(ctx, job, at)
		assert.NoError(t, err)
		assert.True(t, enqueued)
		mockQueueSvc.AssertExpectations(t)
	})

	t.Run("returns enqueue errors other than a conflict", func(t *testing.T) {
		runner, mockQueueSvc := newRunner()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errors.New("redis unavailable"))

		enqueued, err := runner.Run(ctx, job, at)
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
			runner, mockQueueSvc := newRunner()
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
			mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(existing, nil)

			enqueued, err := runner.Run(ctx, job, at)
			assert.NoError(t, err)
			assert.False(t, enqueued)
			mockQueueSvc.AssertNotCalled(t, "DeleteTask", mock.Anything, mock.Anything, mock.Anything)
			mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 1)
		})
	}

	t.Run("skips when the task finished in the meantime", func(t *testing.T) {
		runner, mockQueueSvc := newRunner()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(nil, fmt.Errorf("%w: healthcheck:mon-1", queue.ErrTaskNotFound))

		enqueued, err := runner.Run(ctx, job, at)
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
			runner, mockQueueSvc := newRunner()
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict).Once()
			mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(existing, nil)
			mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(nil)
			mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(&queue.TaskInfo{}, nil).Once()

			enqueued, err := runner.Run(ctx, job, at)
			assert.NoError(t, err)
			assert.True(t, enqueued)
			mockQueueSvc.AssertExpectations(t)
		})
	}

	t.Run("skips when the leftover task can't be deleted", func(t *testing.T) {
		runner, mockQueueSvc := newRunner()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(&queue.TaskInfo{ID: "healthcheck:mon-1", State: "archived"}, nil)
		mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(errors.New("redis unavailable"))

		enqueued, err := runner.Run(ctx, job, at)
		assert.NoError(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 1)
	})

	t.Run("skips when another producer enqueued first", func(t *testing.T) {
		runner, mockQueueSvc := newRunner()
		mockQueueSvc.On("Enqueue", ctx, worker.TaskTypeHealthCheck, payload, opts).Return(nil, errTaskIDConflict)
		mockQueueSvc.On("GetTaskInfo", ctx, "healthcheck", "healthcheck:mon-1").Return(&queue.TaskInfo{ID: "healthcheck:mon-1", State: "archived"}, nil)
		mockQueueSvc.On("DeleteTask", ctx, "healthcheck", "healthcheck:mon-1").Return(nil)

		enqueued, err := runner.Run(ctx, job, at)
		assert.NoError(t, err)
		assert.False(t, enqueued)
		mockQueueSvc.AssertNumberOfCalls(t, "Enqueue", 2)
	})
}
