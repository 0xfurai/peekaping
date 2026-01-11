package monitor

import (
	"context"
	"errors"
	"testing"
	"time"
	"vigi/internal/infra"
	"vigi/internal/modules/events"
	"vigi/internal/modules/healthcheck/executor"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor_notification"
	"vigi/internal/modules/monitor_tag"
	"vigi/internal/modules/shared"
	"vigi/internal/modules/stats"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// Mock implementations
type MockMonitorRepository struct {
	mock.Mock
}

func (m *MockMonitorRepository) Create(ctx context.Context, monitor *Model) (*Model, error) {
	args := m.Called(ctx, monitor)
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*Model, error) {
	args := m.Called(ctx, ids, orgID)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*Model, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds, orgID)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindActive(ctx context.Context) ([]*Model, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) UpdateFull(ctx context.Context, id string, monitor *Model, orgID string) error {
	args := m.Called(ctx, id, monitor, orgID)
	return args.Error(0)
}

func (m *MockMonitorRepository) UpdatePartial(ctx context.Context, id string, monitor *UpdateModel, orgID string) error {
	args := m.Called(ctx, id, monitor, orgID)
	return args.Error(0)
}

func (m *MockMonitorRepository) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

func (m *MockMonitorRepository) RemoveProxyReference(ctx context.Context, proxyId string) error {
	args := m.Called(ctx, proxyId)
	return args.Error(0)
}

func (m *MockMonitorRepository) FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error) {
	args := m.Called(ctx, proxyId)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error) {
	args := m.Called(ctx, pushToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

type MockHeartbeatService struct {
	mock.Mock
}

func (m *MockHeartbeatService) Create(ctx context.Context, entity *heartbeat.CreateUpdateDto) (*heartbeat.Model, error) {
	args := m.Called(ctx, entity)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindByID(ctx context.Context, id string) (*heartbeat.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindAll(ctx context.Context, page int, limit int) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockHeartbeatService) FindUptimeStatsByMonitorID(ctx context.Context, monitorID string, periods map[string]time.Duration, now time.Time) (map[string]float64, error) {
	args := m.Called(ctx, monitorID, periods, now)
	return args.Get(0).(map[string]float64), args.Error(1)
}

func (m *MockHeartbeatService) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	args := m.Called(ctx, cutoff)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

type MockEventBus struct {
	mock.Mock
}

func (m *MockEventBus) Publish(event events.Event) {
	m.Called(event)
}

type MockMonitorNotificationService struct {
	mock.Mock
}

func (m *MockMonitorNotificationService) Create(ctx context.Context, monitorID string, notificationID string) (*monitor_notification.Model, error) {
	args := m.Called(ctx, monitorID, notificationID)
	return args.Get(0).(*monitor_notification.Model), args.Error(1)
}

func (m *MockMonitorNotificationService) FindByID(ctx context.Context, id string) (*monitor_notification.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*monitor_notification.Model), args.Error(1)
}

func (m *MockMonitorNotificationService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorNotificationService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_notification.Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).([]*monitor_notification.Model), args.Error(1)
}

func (m *MockMonitorNotificationService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockMonitorNotificationService) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	args := m.Called(ctx, notificationID)
	return args.Error(0)
}

type MockMonitorTagService struct {
	mock.Mock
}

func (m *MockMonitorTagService) Create(ctx context.Context, monitorID string, tagID string) (*monitor_tag.Model, error) {
	args := m.Called(ctx, monitorID, tagID)
	return args.Get(0).(*monitor_tag.Model), args.Error(1)
}

func (m *MockMonitorTagService) FindByID(ctx context.Context, id string) (*monitor_tag.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*monitor_tag.Model), args.Error(1)
}

func (m *MockMonitorTagService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorTagService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_tag.Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).([]*monitor_tag.Model), args.Error(1)
}

func (m *MockMonitorTagService) FindByTagID(ctx context.Context, tagID string) ([]*monitor_tag.Model, error) {
	args := m.Called(ctx, tagID)
	return args.Get(0).([]*monitor_tag.Model), args.Error(1)
}

func (m *MockMonitorTagService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockMonitorTagService) DeleteByTagID(ctx context.Context, tagID string) error {
	args := m.Called(ctx, tagID)
	return args.Error(0)
}

func (m *MockMonitorTagService) DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error {
	args := m.Called(ctx, monitorID, tagID)
	return args.Error(0)
}

type MockExecutorRegistry struct {
	mock.Mock
}

func (m *MockExecutorRegistry) ValidateConfig(monitorType string, configJSON string) error {
	args := m.Called(monitorType, configJSON)
	return args.Error(0)
}

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

func (m *MockStatsService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod, interval int) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period, interval)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID string, since, until time.Time, period stats.StatPeriod) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, since, until, period)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) StatPointsSummary(statsList []*stats.Stat) *stats.Stats {
	args := m.Called(statsList)
	return args.Get(0).(*stats.Stats)
}

// Test setup helper
func setupMonitorService() (*MonitorServiceImpl, *MockMonitorRepository, *MockHeartbeatService, *MockEventBus, *MockMonitorNotificationService, *MockMonitorTagService, *MockExecutorRegistry, *MockStatsService) {
	mockRepo := &MockMonitorRepository{}
	mockHeartbeatService := &MockHeartbeatService{}
	mockEventBus := &MockEventBus{}
	mockNotificationService := &MockMonitorNotificationService{}
	mockTagService := &MockMonitorTagService{}
	mockExecutorRegistry := &MockExecutorRegistry{}
	mockStatsService := &MockStatsService{}
	logger := zap.NewNop().Sugar()

	// Create a redis client for EventBus (will fail to connect but that's ok for unit tests)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Create a real EventBus for the service since it expects pointer to EventBus
	realEventBus := infra.NewRedisEventBus(redisClient, logger)

	// Create a real ExecutorRegistry since the service expects a pointer to ExecutorRegistry
	realExecutorRegistry := executor.NewExecutorRegistry(logger)

	service := NewMonitorService(
		mockRepo,
		mockHeartbeatService,
		realEventBus,
		mockNotificationService,
		mockTagService,
		realExecutorRegistry,
		mockStatsService,
		logger,
	).(*MonitorServiceImpl)

	return service, mockRepo, mockHeartbeatService, mockEventBus, mockNotificationService, mockTagService, mockExecutorRegistry, mockStatsService
}

func TestMonitorService_Create(t *testing.T) {
	service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		createDto := &CreateUpdateDto{
			Type:           "http",
			Name:           "Test Monitor",
			Interval:       60,
			Timeout:        30,
			MaxRetries:     3,
			RetryInterval:  30,
			ResendInterval: 10,
			Active:         true,
			Config:         "{}",
			ProxyId:        "proxy123",
			PushToken:      "token123",
		}

		expectedModel := &Model{
			ID:             "monitor123",
			Type:           createDto.Type,
			Name:           createDto.Name,
			Interval:       createDto.Interval,
			Timeout:        createDto.Timeout,
			MaxRetries:     createDto.MaxRetries,
			RetryInterval:  createDto.RetryInterval,
			ResendInterval: createDto.ResendInterval,
			Active:         createDto.Active,
			Status:         shared.MonitorStatusUp,
			Config:         createDto.Config,
			ProxyId:        createDto.ProxyId,
			PushToken:      createDto.PushToken,
			CreatedAt:      time.Now().UTC(),
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(m *Model) bool {
			return m.Type == createDto.Type &&
				m.Name == createDto.Name &&
				m.Interval == createDto.Interval &&
				m.Status == shared.MonitorStatusUp
		})).Return(expectedModel, nil)

		result, err := service.Create(ctx, createDto)

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		createDto := &CreateUpdateDto{
			Type: "http",
			Name: "Test Monitor",
		}

		mockRepo.On("Create", ctx, mock.Anything).Return((*Model)(nil), errors.New("repository error"))

		result, err := service.Create(ctx, createDto)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_FindByID(t *testing.T) {
	service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
	ctx := context.Background()

	t.Run("successful find", func(t *testing.T) {
		monitorID := "monitor123"
		expectedModel := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil)

		result, err := service.FindByID(ctx, monitorID, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		monitorID := "nonexistent"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return((*Model)(nil), errors.New("not found"))

		result, err := service.FindByID(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_UpdateFull(t *testing.T) {
	service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		monitorID := "monitor123"
		updateDto := &CreateUpdateDto{
			Type:           "tcp",
			Name:           "Updated Monitor",
			Interval:       120,
			Timeout:        60,
			MaxRetries:     5,
			RetryInterval:  60,
			ResendInterval: 20,
			Active:         false,
			Config:         "{\"host\":\"example.com\"}",
			ProxyId:        "proxy456",
			PushToken:      "token456",
		}

		mockRepo.On("UpdateFull", ctx, monitorID, mock.MatchedBy(func(m *Model) bool {
			return m.ID == monitorID &&
				m.Type == updateDto.Type &&
				m.Name == updateDto.Name &&
				m.Interval == updateDto.Interval &&
				m.Status == shared.MonitorStatusUp
		}), mock.Anything).Return(nil)

		result, err := service.UpdateFull(ctx, monitorID, updateDto)

		assert.NoError(t, err)
		assert.Equal(t, monitorID, result.ID)
		assert.Equal(t, updateDto.Name, result.Name)
		assert.Equal(t, updateDto.Type, result.Type)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		monitorID := "monitor123"
		updateDto := &CreateUpdateDto{
			Type: "tcp",
			Name: "Updated Monitor",
		}

		mockRepo.On("UpdateFull", ctx, monitorID, mock.Anything, mock.Anything).Return(errors.New("update failed"))

		result, err := service.UpdateFull(ctx, monitorID, updateDto)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update failed")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_UpdatePartial(t *testing.T) {
	ctx := context.Background()

	t.Run("successful partial update with publish", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		active := true
		status := shared.MonitorStatusDown
		updateDto := &PartialUpdateDto{
			Active: &active,
			Status: &status,
		}

		expectedModel := &Model{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: active,
			Status: status,
		}

		mockRepo.On("UpdatePartial", ctx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID &&
				m.Status != nil &&
				*m.Status == status
		}), mock.Anything).Return(nil)

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("successful partial update without publish", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		active := false
		updateDto := &PartialUpdateDto{
			Active: &active,
		}

		expectedModel := &Model{
			ID:     monitorID,
			Name:   "Test Monitor",
			Active: active,
		}

		mockRepo.On("UpdatePartial", ctx, monitorID, mock.Anything, mock.Anything).Return(nil)
		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, true, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
		// EventBus should not be called when noPublish is true
	})

	t.Run("successful partial update with config field", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		config := "{\"host\":\"example.com\",\"port\":8080}"
		updateDto := &PartialUpdateDto{
			Config: &config,
		}

		expectedModel := &Model{
			ID:     monitorID,
			Name:   "Test Monitor",
			Config: config,
		}

		mockRepo.On("UpdatePartial", ctx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID &&
				m.Config != nil &&
				*m.Config == config
		}), mock.Anything).Return(nil)

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("update error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		updateDto := &PartialUpdateDto{}

		mockRepo.On("UpdatePartial", ctx, monitorID, mock.Anything, mock.Anything).Return(errors.New("update failed"))

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update failed")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("successful deletion", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, mockNotificationService, mockTagService, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("Delete", ctx, monitorID, mock.Anything).Return(nil)
		mockNotificationService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockTagService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)

		err := service.Delete(ctx, monitorID, "org1")

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
		mockNotificationService.AssertExpectations(t)
		mockTagService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("repository delete error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("Delete", ctx, monitorID, mock.Anything).Return(errors.New("delete failed"))

		err := service.Delete(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete failed")
		mockRepo.AssertExpectations(t)
	})

	t.Run("cleanup errors are ignored", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, mockNotificationService, mockTagService, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("Delete", ctx, monitorID, mock.Anything).Return(nil)
		mockNotificationService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("cleanup error"))
		mockTagService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("cleanup error"))
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("cleanup error"))
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("cleanup error"))

		err := service.Delete(ctx, monitorID, "org1")

		assert.NoError(t, err) // Cleanup errors are ignored
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_ValidateMonitorConfig(t *testing.T) {
	t.Run("successful validation", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		monitorType := "http"
		configJSON := `{
			"url": "https://example.com",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none"
		}`

		err := service.ValidateMonitorConfig(monitorType, configJSON)

		assert.NoError(t, err)
	})

	t.Run("validation error", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		monitorType := "http"
		configJSON := `{"invalid": "config"}`

		err := service.ValidateMonitorConfig(monitorType, configJSON)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
	})

	t.Run("nil executor registry", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		service.executorRegistry = nil

		err := service.ValidateMonitorConfig("http", "{}")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "executor registry not available")
	})
}

func TestMonitorService_GetHeartbeats(t *testing.T) {
	ctx := context.Background()

	t.Run("successful retrieval", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		limit := 10
		page := 1
		important := true
		reverse := false

		expectedModel := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		expectedHeartbeats := []*heartbeat.Model{
			{ID: "hb1", MonitorID: monitorID},
			{ID: "hb2", MonitorID: monitorID},
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, limit, page, &important, reverse).Return(expectedHeartbeats, nil)

		result, err := service.GetHeartbeats(ctx, monitorID, limit, page, &important, reverse, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedHeartbeats, result)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("heartbeat service error", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		limit := 10
		page := 1

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(&Model{ID: monitorID}, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, monitorID, limit, page, (*bool)(nil), false).Return(([]*heartbeat.Model)(nil), errors.New("service error"))

		result, err := service.GetHeartbeats(ctx, monitorID, limit, page, nil, false, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "service error")
		mockHeartbeatService.AssertExpectations(t)
	})
}

func TestMonitorService_GetStatPoints(t *testing.T) {
	ctx := context.Background()

	t.Run("successful stat points retrieval", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "hour"
		interval := 60

		monitor := &Model{
			ID:       monitorID,
			Interval: interval,
		}

		expectedUptime := 83.33
		expectedAvgPing := 50.5
		expectedMaxPing := 70.0
		expectedMinPing := 30.0

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRangeWithInterval", ctx, monitorID, since, until, stats.StatHourly, interval).Return([]*stats.Stat{
			{
				Up:          10,
				Down:        2,
				Maintenance: 0,
				Ping:        50.5,
				PingMin:     30.0,
				PingMax:     70.0,
				Timestamp:   since,
			},
		}, nil)
		mockStatsService.On("StatPointsSummary", mock.Anything).Return(&stats.Stats{
			MaxPing: &expectedMaxPing,
			MinPing: &expectedMinPing,
			AvgPing: &expectedAvgPing,
			Uptime:  &expectedUptime,
		})

		result, err := service.GetStatPoints(ctx, monitorID, since, until, granularity, "org1")

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Points, 1)
		assert.Equal(t, 10, result.Points[0].Up)
		assert.Equal(t, 2, result.Points[0].Down)
		assert.Equal(t, 0, result.Points[0].Maintenance)
		assert.Equal(t, 50.5, result.Points[0].Ping)
		assert.Equal(t, 30.0, result.Points[0].PingMin)
		assert.Equal(t, 70.0, result.Points[0].PingMax)
		assert.Equal(t, since.Unix()*1000, result.Points[0].Timestamp)
		assert.Equal(t, &expectedMaxPing, result.MaxPing)
		assert.Equal(t, &expectedMinPing, result.MinPing)
		assert.Equal(t, &expectedAvgPing, result.AvgPing)
		assert.Equal(t, &expectedUptime, result.Uptime)

		mockRepo.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("invalid granularity", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "invalid"

		result, err := service.GetStatPoints(ctx, monitorID, since, until, granularity, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid granularity")
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "nonexistent"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "minute"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return((*Model)(nil), errors.New("not found"))

		result, err := service.GetStatPoints(ctx, monitorID, since, until, granularity, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "day"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return((*Model)(nil), nil)

		result, err := service.GetStatPoints(ctx, monitorID, since, until, granularity, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "monitor not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("stats service error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "minute"
		interval := 60

		monitor := &Model{
			ID:       monitorID,
			Interval: interval,
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRangeWithInterval", ctx, monitorID, since, until, stats.StatMinutely, monitor.Interval).Return(([]*stats.Stat)(nil), errors.New("stats error"))

		result, err := service.GetStatPoints(ctx, monitorID, since, until, granularity, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "stats error")
		mockRepo.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})
}

func TestMonitorService_GetUptimeStats(t *testing.T) {
	ctx := context.Background()

	t.Run("successful uptime stats retrieval", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		expectedModel := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		statsList := []*stats.Stat{
			{Up: 80, Down: 20},
		}

		uptime := 80.0
		summary := &stats.Stats{
			Uptime: &uptime,
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil).Times(4)
		// Mock calls for each time period
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(statsList, nil).Times(4)
		mockStatsService.On("StatPointsSummary", statsList).Return(summary).Times(4)

		result, err := service.GetUptimeStats(ctx, monitorID, "org1")

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 80.0, result.Uptime24h)
		assert.Equal(t, 80.0, result.Uptime7d)
		assert.Equal(t, 80.0, result.Uptime30d)
		assert.Equal(t, 80.0, result.Uptime365d)

		mockStatsService.AssertExpectations(t)
	})

	t.Run("nil uptime in summary", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		expectedModel := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		statsList := []*stats.Stat{}
		summary := &stats.Stats{
			Uptime: nil,
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(expectedModel, nil).Times(4)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(statsList, nil).Times(4)
		mockStatsService.On("StatPointsSummary", statsList).Return(summary).Times(4)

		result, err := service.GetUptimeStats(ctx, monitorID, "org1")

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 0.0, result.Uptime24h)
		assert.Equal(t, 0.0, result.Uptime7d)
		assert.Equal(t, 0.0, result.Uptime30d)
		assert.Equal(t, 0.0, result.Uptime365d)

		mockStatsService.AssertExpectations(t)
	})

	t.Run("stats service error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(&Model{ID: monitorID}, nil).Once()
		mockStatsService.On("FindStatsByMonitorIDAndTimeRange", ctx, monitorID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"), stats.StatDaily).Return(([]*stats.Stat)(nil), errors.New("stats error")).Once()

		result, err := service.GetUptimeStats(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "stats error")
		mockStatsService.AssertExpectations(t)
	})
}

func TestMonitorService_ResetMonitorData(t *testing.T) {
	ctx := context.Background()

	t.Run("successful reset", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"
		monitor := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		updatedMonitor := &Model{
			ID:     monitorID,
			Name:   "Test Monitor",
			Status: shared.MonitorStatusPending,
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil).Once()
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockRepo.On("UpdatePartial", ctx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID && *m.Status == shared.MonitorStatusPending
		}), mock.Anything).Return(nil)
		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(updatedMonitor, nil).Once()

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "nonexistent"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return((*Model)(nil), errors.New("not found"))

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return((*Model)(nil), nil)

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "monitor not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("heartbeat deletion error", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		monitor := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("heartbeat delete failed"))

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete heartbeats")
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("stats deletion error", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"
		monitor := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("stats delete failed"))

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete stats")
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("status update error", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"
		monitor := &Model{
			ID:   monitorID,
			Name: "Test Monitor",
		}

		mockRepo.On("FindByID", ctx, monitorID, mock.Anything).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockRepo.On("UpdatePartial", ctx, monitorID, mock.Anything, mock.Anything).Return(errors.New("update failed"))

		err := service.ResetMonitorData(ctx, monitorID, "org1")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to reset monitor status")
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})
}

func TestMonitorService_FindAll(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find all", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		page := 1
		limit := 10
		q := "test"
		active := true
		status := 1
		tagIds := []string{"tag1", "tag2"}

		expectedMonitors := []*Model{
			{ID: "monitor1", Name: "Monitor 1"},
			{ID: "monitor2", Name: "Monitor 2"},
		}

		mockRepo.On("FindAll", ctx, page, limit, q, &active, &status, tagIds, mock.Anything).Return(expectedMonitors, nil)

		result, err := service.FindAll(ctx, page, limit, q, &active, &status, tagIds, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindAll", ctx, 1, 10, "", (*bool)(nil), (*int)(nil), []string(nil), mock.Anything).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindAll(ctx, 1, 10, "", nil, nil, nil, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_FindActive(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find active", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		expectedMonitors := []*Model{
			{ID: "monitor1", Name: "Monitor 1", Active: true},
			{ID: "monitor2", Name: "Monitor 2", Active: true},
		}

		mockRepo.On("FindActive", ctx).Return(expectedMonitors, nil)

		result, err := service.FindActive(ctx)

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindActive", ctx).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindActive(ctx)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_FindByIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find by IDs", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		ids := []string{"monitor1", "monitor2"}
		expectedMonitors := []*Model{
			{ID: "monitor1", Name: "Monitor 1"},
			{ID: "monitor2", Name: "Monitor 2"},
		}

		mockRepo.On("FindByIDs", ctx, ids, mock.Anything).Return(expectedMonitors, nil)

		result, err := service.FindByIDs(ctx, ids, "org1")

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		ids := []string{"monitor1"}

		mockRepo.On("FindByIDs", ctx, ids, mock.Anything).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindByIDs(ctx, ids, "org1")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_RemoveProxyReference(t *testing.T) {
	ctx := context.Background()

	t.Run("successful proxy reference removal", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		proxyId := "proxy123"

		mockRepo.On("RemoveProxyReference", ctx, proxyId).Return(nil)

		err := service.RemoveProxyReference(ctx, proxyId)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		proxyId := "proxy123"

		mockRepo.On("RemoveProxyReference", ctx, proxyId).Return(errors.New("repository error"))

		err := service.RemoveProxyReference(ctx, proxyId)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_FindByProxyId(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find by proxy ID", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		proxyId := "proxy123"
		expectedMonitors := []*Model{
			{ID: "monitor1", ProxyId: proxyId},
			{ID: "monitor2", ProxyId: proxyId},
		}

		mockRepo.On("FindByProxyId", ctx, proxyId).Return(expectedMonitors, nil)

		result, err := service.FindByProxyId(ctx, proxyId)

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		proxyId := "proxy123"

		mockRepo.On("FindByProxyId", ctx, proxyId).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindByProxyId(ctx, proxyId)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_FindOneByPushToken(t *testing.T) {
	ctx := context.Background()

	t.Run("successful find by push token", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		pushToken := "token123"
		expectedMonitor := &Model{
			ID:        "monitor1",
			PushToken: pushToken,
		}

		mockRepo.On("FindOneByPushToken", ctx, pushToken).Return(expectedMonitor, nil)

		result, err := service.FindOneByPushToken(ctx, pushToken)

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitor, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		pushToken := "nonexistent"

		mockRepo.On("FindOneByPushToken", ctx, pushToken).Return((*Model)(nil), errors.New("not found"))

		result, err := service.FindOneByPushToken(ctx, pushToken)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not found")
		mockRepo.AssertExpectations(t)
	})
}

func TestNewMonitorService(t *testing.T) {
	mockRepo := &MockMonitorRepository{}
	mockHeartbeatService := &MockHeartbeatService{}
	mockNotificationService := &MockMonitorNotificationService{}
	mockTagService := &MockMonitorTagService{}
	mockStatsService := &MockStatsService{}
	logger := zap.NewNop().Sugar()

	// Create a redis client for EventBus (will fail to connect but that's ok for unit tests)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Create real instances for dependencies that expect concrete types
	realEventBus := infra.NewRedisEventBus(redisClient, logger)
	realExecutorRegistry := executor.NewExecutorRegistry(logger)

	service := NewMonitorService(
		mockRepo,
		mockHeartbeatService,
		realEventBus,
		mockNotificationService,
		mockTagService,
		realExecutorRegistry,
		mockStatsService,
		logger,
	)

	assert.NotNil(t, service)
	assert.IsType(t, &MonitorServiceImpl{}, service)

	serviceImpl := service.(*MonitorServiceImpl)
	assert.Equal(t, mockRepo, serviceImpl.monitorRepository)
	assert.Equal(t, mockHeartbeatService, serviceImpl.heartbeatService)
	assert.Equal(t, realEventBus, serviceImpl.eventBus)
	assert.Equal(t, mockNotificationService, serviceImpl.monitorNotificationService)
	assert.Equal(t, mockTagService, serviceImpl.monitorTagService)
	assert.Equal(t, realExecutorRegistry, serviceImpl.executorRegistry)
	assert.Equal(t, mockStatsService, serviceImpl.statPointsService)
	assert.NotNil(t, serviceImpl.logger)
}
