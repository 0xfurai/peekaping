package producer

import (
	"context"
	"testing"

	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/queue"

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

// MockMonitorService mocks the monitor service's methods the producer and the check loader read
type MockMonitorService struct {
	mock.Mock
}

func (m *MockMonitorService) FindByID(ctx context.Context, id string) (*monitor.Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.Model), args.Error(1)
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

func (m *MockMonitorService) FindActiveByProbe(ctx context.Context, probeID string, page int, pageSize int) ([]*monitor.Model, error) {
	args := m.Called(ctx, probeID, page, pageSize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	switch v := args.Get(0).(type) {
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

// MockMaintenanceService mocks the maintenance service's methods the check loader reads
type MockMaintenanceService struct {
	mock.Mock
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

// MockProxyService mocks the proxy service's method the check loader reads
type MockProxyService struct {
	mock.Mock
}

func (m *MockProxyService) FindByID(ctx context.Context, id string) (*proxy.Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proxy.Model), args.Error(1)
}

// MockQueueService mocks the queue service's methods the queue runner uses
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
