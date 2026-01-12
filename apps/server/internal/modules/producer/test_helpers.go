package producer

import (
	"context"
	"testing"
	"time"

	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/maintenance"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/proxy"
	"vigi/internal/modules/queue"
	"vigi/internal/modules/stats"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupTestRedis creates a test Redis client and miniredis instance
func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, mr
}

// MockMonitorService for testing
type MockMonitorService struct {
	mock.Mock
}

func (m *MockMonitorService) Create(ctx context.Context, dto *monitor.CreateUpdateDto) (*monitor.Model, error) {
	args := m.Called(ctx, dto)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) FindByID(ctx context.Context, id string, orgID string) (*monitor.Model, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*monitor.Model, error) {
	args := m.Called(ctx, ids, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*monitor.Model, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) FindActive(ctx context.Context) ([]*monitor.Model, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) FindActivePaginated(ctx context.Context, page int, pageSize int) ([]*monitor.Model, error) {
	args := m.Called(ctx, page, pageSize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	// Convert []monitor.Model to []*monitor.Model
	models := args.Get(0)
	switch v := models.(type) {
	case []monitor.Model:
		result := make([]*monitor.Model, len(v))
		for i := range v {
			result[i] = &v[i]
		}
		return result, args.Error(1)
	case []*monitor.Model:
		return v, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockMonitorService) UpdateFull(ctx context.Context, id string, dto *monitor.CreateUpdateDto) (*monitor.Model, error) {
	args := m.Called(ctx, id, dto)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) UpdatePartial(ctx context.Context, id string, dto *monitor.PartialUpdateDto, noPublish bool, orgID string) (*monitor.Model, error) {
	args := m.Called(ctx, id, dto, noPublish, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockMonitorService) ValidateMonitorConfig(monitorType string, configJSON string) error {
	args := m.Called(monitorType, configJSON)
	return args.Error(0)
}

func (m *MockMonitorService) GetHeartbeats(ctx context.Context, id string, limit, page int, important *bool, reverse bool, orgID string) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, id, limit, page, important, reverse, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockMonitorService) RemoveProxyReference(ctx context.Context, proxyId string) error {
	args := m.Called(ctx, proxyId)
	return args.Error(0)
}

func (m *MockMonitorService) FindByProxyId(ctx context.Context, proxyId string) ([]*monitor.Model, error) {
	args := m.Called(ctx, proxyId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) GetStatPoints(ctx context.Context, id string, since, until time.Time, granularity string, orgID string) (*monitor.StatPointsSummaryDto, error) {
	args := m.Called(ctx, id, since, until, granularity, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.StatPointsSummaryDto), args.Error(1)
}

func (m *MockMonitorService) GetUptimeStats(ctx context.Context, id string, orgID string) (*monitor.CustomUptimeStatsDto, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.CustomUptimeStatsDto), args.Error(1)
}

func (m *MockMonitorService) FindOneByPushToken(ctx context.Context, pushToken string) (*monitor.Model, error) {
	args := m.Called(ctx, pushToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
}

func (m *MockMonitorService) ResetMonitorData(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

// MockMaintenanceService for testing
type MockMaintenanceService struct {
	mock.Mock
}

func (m *MockMaintenanceService) Create(ctx context.Context, dto *maintenance.CreateUpdateDto) (*maintenance.Model, error) {
	args := m.Called(ctx, dto)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*maintenance.Model, error) {
	args := m.Called(ctx, monitorID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) IsUnderMaintenance(ctx context.Context, maint *maintenance.Model) (bool, error) {
	args := m.Called(ctx, maint)
	return args.Bool(0), args.Error(1)
}

func (m *MockMaintenanceService) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockMaintenanceService) FindByID(ctx context.Context, id string, orgID string) (*maintenance.Model, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*maintenance.Model, error) {
	args := m.Called(ctx, page, limit, q, strategy, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) UpdateFull(ctx context.Context, id string, dto *maintenance.CreateUpdateDto, orgID string) (*maintenance.Model, error) {
	args := m.Called(ctx, id, dto, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) UpdatePartial(ctx context.Context, id string, dto *maintenance.PartialUpdateDto, orgID string) (*maintenance.Model, error) {
	args := m.Called(ctx, id, dto, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) SetActive(ctx context.Context, id string, active bool, orgID string) (*maintenance.Model, error) {
	args := m.Called(ctx, id, active, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*maintenance.Model), args.Error(1)
}

func (m *MockMaintenanceService) GetMonitors(ctx context.Context, id string) ([]string, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// MockProxyService for testing
type MockProxyService struct {
	mock.Mock
}

func (m *MockProxyService) Create(ctx context.Context, dto *proxy.CreateUpdateDto) (*proxy.Model, error) {
	args := m.Called(ctx, dto)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proxy.Model), args.Error(1)
}

func (m *MockProxyService) FindByID(ctx context.Context, id string, orgID string) (*proxy.Model, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proxy.Model), args.Error(1)
}

func (m *MockProxyService) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockProxyService) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*proxy.Model, error) {
	args := m.Called(ctx, page, limit, q, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*proxy.Model), args.Error(1)
}

func (m *MockProxyService) UpdateFull(ctx context.Context, id string, dto *proxy.CreateUpdateDto, orgID string) (*proxy.Model, error) {
	args := m.Called(ctx, id, dto, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proxy.Model), args.Error(1)
}

func (m *MockProxyService) UpdatePartial(ctx context.Context, id string, dto *proxy.PartialUpdateDto, orgID string) (*proxy.Model, error) {
	args := m.Called(ctx, id, dto, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proxy.Model), args.Error(1)
}

// MockQueueService for testing
type MockQueueService struct {
	mock.Mock
}

func (m *MockQueueService) Enqueue(ctx context.Context, taskType string, payload interface{}, opts *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	args := m.Called(ctx, taskType, payload, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) EnqueueUnique(ctx context.Context, taskType string, payload interface{}, uniqueKey string, ttl time.Duration, opts *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	args := m.Called(ctx, taskType, payload, uniqueKey, ttl, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) GetQueueInfo(ctx context.Context, queueName string) (*queue.QueueInfo, error) {
	args := m.Called(ctx, queueName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.QueueInfo), args.Error(1)
}

func (m *MockQueueService) ListQueues(ctx context.Context) ([]*queue.QueueInfo, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.QueueInfo), args.Error(1)
}

func (m *MockQueueService) GetTaskInfo(ctx context.Context, queueName, taskID string) (*queue.TaskInfo, error) {
	args := m.Called(ctx, queueName, taskID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) DeleteTask(ctx context.Context, queueName, taskID string) error {
	args := m.Called(ctx, queueName, taskID)
	return args.Error(0)
}

func (m *MockQueueService) CancelTask(ctx context.Context, taskID string) error {
	args := m.Called(ctx, taskID)
	return args.Error(0)
}

func (m *MockQueueService) PauseQueue(ctx context.Context, queueName string) error {
	args := m.Called(ctx, queueName)
	return args.Error(0)
}

func (m *MockQueueService) UnpauseQueue(ctx context.Context, queueName string) error {
	args := m.Called(ctx, queueName)
	return args.Error(0)
}

func (m *MockQueueService) ListPendingTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	args := m.Called(ctx, queueName, pageSize, pageNum)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) ListActiveTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	args := m.Called(ctx, queueName, pageSize, pageNum)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) ListScheduledTasks(ctx context.Context, queueName string, pageSize, pageNum int) ([]*queue.TaskInfo, error) {
	args := m.Called(ctx, queueName, pageSize, pageNum)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.TaskInfo), args.Error(1)
}

func (m *MockQueueService) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockEventBus for testing
type MockEventBus struct {
	mock.Mock
	subscribers map[events.EventType][]events.EventHandler
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		subscribers: make(map[events.EventType][]events.EventHandler),
	}
}

func (m *MockEventBus) Publish(event events.Event) {
	m.Called(event)
	// Also trigger handlers if any
	if handlers, ok := m.subscribers[event.Type]; ok {
		for _, handler := range handlers {
			handler(event)
		}
	}
}

func (m *MockEventBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	m.Called(eventType, handler)
	m.subscribers[eventType] = append(m.subscribers[eventType], handler)
}

func (m *MockEventBus) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Helper to trigger event
func (m *MockEventBus) TriggerEvent(eventType events.EventType, event events.Event) {
	if handlers, ok := m.subscribers[eventType]; ok {
		for _, handler := range handlers {
			handler(event)
		}
	}
}

// MockStatsService for testing
type MockStatsService struct {
	mock.Mock
}

func (m *MockStatsService) AggregateHeartbeat(ctx context.Context, hb *stats.HeartbeatPayload) error {
	args := m.Called(ctx, hb)
	return args.Error(0)
}

func (m *MockStatsService) RegisterEventHandlers(eventBus events.EventBus) {
	m.Called(eventBus)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod, monitorInterval int) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period, monitorInterval)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) StatPointsSummary(statsList []*stats.Stat) *stats.Stats {
	args := m.Called(statsList)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*stats.Stats)
}

func (m *MockStatsService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}
