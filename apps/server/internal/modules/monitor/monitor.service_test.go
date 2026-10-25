package monitor

import (
	"context"
	"errors"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func (m *MockMonitorRepository) FindByID(ctx context.Context, id string) (*Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindByIDs(ctx context.Context, ids []string) ([]*Model, error) {
	args := m.Called(ctx, ids)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*Model, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds)
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

func (m *MockMonitorRepository) FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*Model, error) {
	args := m.Called(ctx, probeID, page, limit)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockMonitorRepository) RecordLocation(ctx context.Context, monitorID string, state shared.LocationState, _ func(shared.LocationJudgement) *shared.MonitorStatus) error {
	args := m.Called(ctx, monitorID, state)
	return args.Error(0)
}

func (m *MockMonitorRepository) ResetLocations(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockMonitorRepository) UpdateFull(ctx context.Context, id string, monitor *Model) error {
	args := m.Called(ctx, id, monitor)
	return args.Error(0)
}

func (m *MockMonitorRepository) UpdatePartial(ctx context.Context, id string, monitor *UpdateModel) error {
	args := m.Called(ctx, id, monitor)
	return args.Error(0)
}

func (m *MockMonitorRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
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

func (m *MockHeartbeatService) HasAny(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDPaginated(ctx context.Context, monitorID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) FindByMonitorIDAndProbeIDPaginated(ctx context.Context, monitorID, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, monitorID, probeID, limit, page, important, reverse)
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockHeartbeatService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
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

func (m *MockMonitorNotificationService) FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*monitor_notification.Model, error) {
	args := m.Called(ctx, monitorIDs)
	return args.Get(0).(map[string][]*monitor_notification.Model), args.Error(1)
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

func (m *MockMonitorTagService) FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*monitor_tag.Model, error) {
	args := m.Called(ctx, monitorIDs)
	return args.Get(0).(map[string][]*monitor_tag.Model), args.Error(1)
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

func (m *MockStatsService) DeleteExpired(ctx context.Context, now time.Time) (map[stats.StatPeriod]int64, error) {
	args := m.Called(ctx, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[stats.StatPeriod]int64), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRangeWithInterval(ctx context.Context, monitorID, probeID string, since, until time.Time, period stats.StatPeriod, interval int) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, probeID, since, until, period, interval)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) FindStatsByMonitorIDAndTimeRange(ctx context.Context, monitorID, probeID string, since, until time.Time, period stats.StatPeriod) ([]*stats.Stat, error) {
	args := m.Called(ctx, monitorID, probeID, since, until, period)
	return args.Get(0).([]*stats.Stat), args.Error(1)
}

func (m *MockStatsService) StatPointsSummary(statsList []*stats.Stat) *stats.Stats {
	args := m.Called(statsList)
	return args.Get(0).(*stats.Stats)
}

func (m *MockStatsService) Summarize(ctx context.Context, monitorID, probeID string, since, until time.Time) (*stats.Stats, error) {
	args := m.Called(ctx, monitorID, probeID, since, until)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*stats.Stats), args.Error(1)
}

// sinceAgo matches a window start about d before now.
func sinceAgo(d time.Duration) any {
	return mock.MatchedBy(func(since time.Time) bool { return time.Since(since).Round(time.Hour) == d })
}

type txMarker struct{}

// fakeTransactor is a dbtx.Transactor for tests that have no database. It runs fn with a ctx
// that is marked as the transaction's, and counts how fn ended: a nil return is a commit, and an
// error is a rollback of everything fn wrote. A repository mock that expects inTx only accepts
// the calls made with that ctx, so a write made outside WithTx fails the test.
type fakeTransactor struct {
	bus *eventstest.Bus

	committed  int
	rolledBack int
	// published is how many events were on the bus when the last transaction committed
	published int
}

var _ dbtx.Transactor = (*fakeTransactor)(nil)

func (f *fakeTransactor) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(txMarker{}) != nil {
		return fn(ctx) // joins the transaction, as dbtx does
	}
	if err := fn(context.WithValue(ctx, txMarker{}, f)); err != nil {
		f.rolledBack++
		return err
	}
	f.committed++
	f.published = len(f.bus.Topics())
	return nil
}

// inTx matches the ctx of a call made inside WithTx.
var inTx = mock.MatchedBy(func(ctx context.Context) bool { return ctx.Value(txMarker{}) != nil })

// transactionOf returns the fake transactor the service was built with.
func transactionOf(service *MonitorServiceImpl) *fakeTransactor {
	return service.transactor.(*fakeTransactor)
}

// Test setup helper
func setupMonitorService() (*MonitorServiceImpl, *MockMonitorRepository, *MockHeartbeatService, *eventstest.Bus, *MockMonitorNotificationService, *MockMonitorTagService, *MockExecutorRegistry, *MockStatsService) {
	mockRepo := &MockMonitorRepository{}
	mockHeartbeatService := &MockHeartbeatService{}
	eventBus := eventstest.NewBus()
	mockNotificationService := &MockMonitorNotificationService{}
	mockTagService := &MockMonitorTagService{}
	mockExecutorRegistry := &MockExecutorRegistry{}
	mockStatsService := &MockStatsService{}
	logger := zap.NewNop().Sugar()

	// Create a real ExecutorRegistry since the service expects a pointer to ExecutorRegistry
	realExecutorRegistry := executor.NewExecutorRegistry(logger)

	service := NewMonitorService(
		mockRepo,
		&fakeTransactor{bus: eventBus},
		mockHeartbeatService,
		eventBus,
		mockNotificationService,
		mockTagService,
		realExecutorRegistry,
		mockStatsService,
		nil,
		logger,
	).(*MonitorServiceImpl)

	return service, mockRepo, mockHeartbeatService, eventBus, mockNotificationService, mockTagService, mockExecutorRegistry, mockStatsService
}

func TestMonitorService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
		createDto := &CreateUpdateDto{
			Type:            "http",
			Name:            "Test Monitor",
			Interval:        60,
			Timeout:         16,
			MaxRetries:      3,
			RetryInterval:   45,
			ResendInterval:  10,
			Active:          true,
			NotificationIds: []string{"channel-a", "channel-b"},
			TagIds:          []string{"tag-a"},
			Config:          "{}",
			ProxyId:         "proxy123",
			PushToken:       "token123",
		}

		var saved *Model
		created := &Model{ID: "monitor123"}
		mockRepo.On("Create", inTx, mock.Anything).Run(func(args mock.Arguments) {
			saved = args.Get(1).(*Model)
		}).Return(created, nil).Once()
		notifications.On("Create", inTx, "monitor123", "channel-a").Return(&monitor_notification.Model{}, nil).Once()
		notifications.On("Create", inTx, "monitor123", "channel-b").Return(&monitor_notification.Model{}, nil).Once()
		tags.On("Create", inTx, "monitor123", "tag-a").Return(&monitor_tag.Model{}, nil).Once()

		before := time.Now().UTC()
		result, err := service.Create(ctx, createDto)

		require.NoError(t, err)
		assert.Same(t, created, result)
		assert.Equal(t, []shared.Monitor{*created}, eventstest.Published(bus, events.MonitorCreated))
		tx := transactionOf(service)
		assert.Equal(t, 1, tx.committed, "the monitor and its links are one transaction")
		assert.Equal(t, 0, tx.published, "the event is published once the transaction has committed, not before")
		require.NotNil(t, saved)
		assert.WithinRange(t, saved.CreatedAt, before, time.Now().UTC())
		assert.Equal(t, time.UTC, saved.CreatedAt.Location())
		saved.CreatedAt = time.Time{}
		assert.Equal(t, &Model{
			Type:           "http",
			Name:           "Test Monitor",
			Interval:       60,
			Timeout:        16,
			MaxRetries:     3,
			RetryInterval:  45,
			ResendInterval: 10,
			LocationRule:   shared.LocationRuleAny,
			Active:         true,
			Status:         shared.MonitorStatusPending,
			Config:         "{}",
			ProxyId:        "proxy123",
			PushToken:      "token123",
		}, saved, "the repository gets every field of the request, and the monitor starts Pending")
		mockRepo.AssertExpectations(t)
		notifications.AssertExpectations(t)
		tags.AssertExpectations(t)
	})

	t.Run("links a repeated ID once", func(t *testing.T) {
		service, mockRepo, _, _, notifications, tags, _, _ := setupMonitorService()
		mockRepo.On("Create", inTx, mock.Anything).Return(&Model{ID: "monitor123"}, nil)
		var linked []string
		notifications.On("Create", inTx, "monitor123", mock.Anything).Run(func(args mock.Arguments) {
			linked = append(linked, "channel "+args.String(2))
		}).Return(&monitor_notification.Model{}, nil)
		tags.On("Create", inTx, "monitor123", mock.Anything).Run(func(args mock.Arguments) {
			linked = append(linked, "tag "+args.String(2))
		}).Return(&monitor_tag.Model{}, nil)

		_, err := service.Create(ctx, &CreateUpdateDto{
			Type:            "http",
			Name:            "Test Monitor",
			NotificationIds: []string{"channel-b", "channel-a", "channel-b"},
			TagIds:          []string{"tag-a", "tag-a"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"channel channel-b", "channel channel-a", "tag tag-a"}, linked, "each ID once, in the order sent")
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
		createDto := &CreateUpdateDto{
			Type: "http",
			Name: "Test Monitor",
		}

		mockRepo.On("Create", inTx, mock.Anything).Return((*Model)(nil), errors.New("repository error"))

		result, err := service.Create(ctx, createDto)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "repository error")
		assert.Equal(t, 1, transactionOf(service).rolledBack)
		assert.Empty(t, bus.Topics(), "nothing is published")
		mockRepo.AssertExpectations(t)
	})

	for _, tc := range []struct {
		name  string
		fails func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService)
	}{
		{"a notification channel link", func(notifications *MockMonitorNotificationService, _ *MockMonitorTagService) {
			notifications.On("Create", inTx, "monitor123", "channel-a").Return((*monitor_notification.Model)(nil), errors.New("link failed"))
		}},
		{"a tag link", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("Create", inTx, "monitor123", "channel-a").Return(&monitor_notification.Model{}, nil)
			tags.On("Create", inTx, "monitor123", "tag-a").Return((*monitor_tag.Model)(nil), errors.New("link failed"))
		}},
	} {
		t.Run("rolls back and publishes nothing when "+tc.name+" fails", func(t *testing.T) {
			service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
			mockRepo.On("Create", inTx, mock.Anything).Return(&Model{ID: "monitor123"}, nil)
			tc.fails(notifications, tags)

			result, err := service.Create(ctx, &CreateUpdateDto{
				Type:            "http",
				Name:            "Test Monitor",
				NotificationIds: []string{"channel-a"},
				TagIds:          []string{"tag-a"},
			})

			require.ErrorContains(t, err, "link failed")
			assert.Nil(t, result)
			tx := transactionOf(service)
			assert.Equal(t, 1, tx.rolledBack, "the monitor, written in the transaction, is rolled back with the links")
			assert.Zero(t, tx.committed)
			assert.Empty(t, bus.Topics(), "no MonitorCreated")
		})
	}
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

		mockRepo.On("FindByID", ctx, monitorID).Return(expectedModel, nil)

		result, err := service.FindByID(ctx, monitorID)

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		monitorID := "nonexistent"

		mockRepo.On("FindByID", ctx, monitorID).Return((*Model)(nil), errors.New("not found"))

		result, err := service.FindByID(ctx, monitorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})
}

func TestMonitorService_UpdateFull(t *testing.T) {
	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
		monitorID := "monitor123"
		updateDto := &CreateUpdateDto{
			Type:            "tcp",
			Name:            "Updated Monitor",
			Interval:        120,
			Timeout:         60,
			MaxRetries:      5,
			RetryInterval:   60,
			ResendInterval:  20,
			Active:          false,
			NotificationIds: []string{"channel-a", "channel-b"},
			TagIds:          []string{"tag-a"},
			Config:          "{\"host\":\"example.com\"}",
			ProxyId:         "proxy456",
			PushToken:       "token456",
		}

		stored := &Model{
			ID:        monitorID,
			Type:      updateDto.Type,
			Name:      updateDto.Name,
			Status:    shared.MonitorStatusDown,
			CreatedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		}
		mockRepo.On("UpdateFull", inTx, monitorID, mock.MatchedBy(func(m *Model) bool {
			return m.ID == monitorID &&
				m.Type == updateDto.Type &&
				m.Name == updateDto.Name &&
				m.Interval == updateDto.Interval
		})).Return(nil)
		notifications.On("DeleteByMonitorID", inTx, monitorID).Return(nil).Once()
		tags.On("DeleteByMonitorID", inTx, monitorID).Return(nil).Once()
		notifications.On("Create", inTx, monitorID, "channel-a").Return(&monitor_notification.Model{}, nil).Once()
		notifications.On("Create", inTx, monitorID, "channel-b").Return(&monitor_notification.Model{}, nil).Once()
		tags.On("Create", inTx, monitorID, "tag-a").Return(&monitor_tag.Model{}, nil).Once()
		mockRepo.On("FindByID", inTx, monitorID).Return(stored, nil)

		result, err := service.UpdateFull(ctx, monitorID, updateDto)

		assert.NoError(t, err)
		assert.Same(t, stored, result, "the response is the monitor as stored, with its status and created_at")
		assert.Equal(t, []shared.Monitor{*stored}, eventstest.Published(bus, events.MonitorUpdated), "the event carries it, too")
		tx := transactionOf(service)
		assert.Equal(t, 1, tx.committed, "the monitor and its links are one transaction")
		assert.Equal(t, 0, tx.published, "the event is published once the transaction has committed, not before")
		mockRepo.AssertExpectations(t)
		notifications.AssertExpectations(t)
		tags.AssertExpectations(t)
	})

	t.Run("links a repeated ID once", func(t *testing.T) {
		service, mockRepo, _, _, notifications, tags, _, _ := setupMonitorService()
		mockRepo.On("UpdateFull", inTx, "monitor123", mock.Anything).Return(nil)
		mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123"}, nil)
		notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
		tags.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
		var linked []string
		notifications.On("Create", inTx, "monitor123", mock.Anything).Run(func(args mock.Arguments) {
			linked = append(linked, "channel "+args.String(2))
		}).Return(&monitor_notification.Model{}, nil)
		tags.On("Create", inTx, "monitor123", mock.Anything).Run(func(args mock.Arguments) {
			linked = append(linked, "tag "+args.String(2))
		}).Return(&monitor_tag.Model{}, nil)

		_, err := service.UpdateFull(ctx, "monitor123", &CreateUpdateDto{
			Type:            "http",
			Name:            "Test Monitor",
			NotificationIds: []string{"channel-b", "channel-a", "channel-b"},
			TagIds:          []string{"tag-a", "tag-a"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"channel channel-b", "channel channel-a", "tag tag-a"}, linked, "each ID once, in the order sent")
	})

	t.Run("monitor removed before it is read back", func(t *testing.T) {
		service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
		monitorID := "removed123"
		mockRepo.On("FindByID", inTx, monitorID).Return(&Model{ID: monitorID, Type: "tcp"}, nil).Once()
		mockRepo.On("UpdateFull", inTx, monitorID, mock.Anything).Return(nil)
		notifications.On("DeleteByMonitorID", inTx, monitorID).Return(nil)
		tags.On("DeleteByMonitorID", inTx, monitorID).Return(nil)
		mockRepo.On("FindByID", inTx, monitorID).Return(nil, nil).Once()

		result, err := service.UpdateFull(ctx, monitorID, &CreateUpdateDto{Type: "tcp", Name: "Updated Monitor"})

		assert.ErrorIs(t, err, ErrMonitorNotFound)
		assert.Nil(t, result)
		assert.Equal(t, 1, transactionOf(service).rolledBack)
		assert.Empty(t, bus.Topics())
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor gone before the update", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindByID", inTx, "gone123").Return(nil, nil)
		mockRepo.On("UpdateFull", mock.Anything, "gone123", mock.Anything).Return(nil).Maybe() // asserted below to be uncalled

		result, err := service.UpdateFull(ctx, "gone123", &CreateUpdateDto{Type: "tcp", Name: "Updated Monitor"})

		assert.ErrorIs(t, err, ErrMonitorNotFound)
		assert.Nil(t, result)
		assert.Empty(t, bus.Topics())
		mockRepo.AssertNotCalled(t, "UpdateFull", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		updateDto := &CreateUpdateDto{
			Type: "tcp",
			Name: "Updated Monitor",
		}

		mockRepo.On("FindByID", inTx, monitorID).Return(&Model{ID: monitorID, Type: "tcp"}, nil)
		mockRepo.On("UpdateFull", inTx, monitorID, mock.Anything).Return(errors.New("update failed"))

		result, err := service.UpdateFull(ctx, monitorID, updateDto)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update failed")
		assert.Empty(t, bus.Topics(), "nothing is published")
		mockRepo.AssertExpectations(t)
	})

	for _, tc := range []struct {
		name  string
		fails func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService)
	}{
		{"unlinking the notification channels", func(notifications *MockMonitorNotificationService, _ *MockMonitorTagService) {
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("link failed"))
		}},
		{"unlinking the tags", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			tags.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("link failed"))
		}},
		{"linking a notification channel", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			tags.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			notifications.On("Create", inTx, "monitor123", "channel-a").Return((*monitor_notification.Model)(nil), errors.New("link failed"))
		}},
		{"linking a tag", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			tags.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			notifications.On("Create", inTx, "monitor123", "channel-a").Return(&monitor_notification.Model{}, nil)
			tags.On("Create", inTx, "monitor123", "tag-a").Return((*monitor_tag.Model)(nil), errors.New("link failed"))
		}},
	} {
		t.Run("rolls back and publishes nothing when "+tc.name+" fails", func(t *testing.T) {
			service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
			mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123", Type: "tcp"}, nil)
			mockRepo.On("UpdateFull", inTx, "monitor123", mock.Anything).Return(nil)
			tc.fails(notifications, tags)

			result, err := service.UpdateFull(ctx, "monitor123", &CreateUpdateDto{
				Type:            "tcp",
				Name:            "Updated Monitor",
				NotificationIds: []string{"channel-a"},
				TagIds:          []string{"tag-a"},
			})

			require.ErrorContains(t, err, "link failed")
			assert.Nil(t, result)
			tx := transactionOf(service)
			assert.Equal(t, 1, tx.rolledBack, "the monitor row and the links are rolled back together")
			assert.Zero(t, tx.committed)
			assert.Empty(t, bus.Topics(), "no MonitorUpdated")
		})
	}
}

func TestMonitorService_UpdatePartial(t *testing.T) {
	ctx := context.Background()

	t.Run("successful partial update with publish", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
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

		mockRepo.On("UpdatePartial", inTx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID &&
				*m.Active == active &&
				*m.Status == status
		})).Return(nil)

		mockRepo.On("FindByID", inTx, monitorID).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false)

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		assert.Equal(t, []shared.Monitor{*expectedModel}, eventstest.Published(bus, events.MonitorUpdated))
		tx := transactionOf(service)
		assert.Equal(t, 1, tx.committed)
		assert.Equal(t, 0, tx.published, "the event is published once the transaction has committed, not before")
		mockRepo.AssertExpectations(t)
	})

	t.Run("successful partial update without publish", func(t *testing.T) {
		// The listener's status updates send no link IDs: the link mocks have no calls set up, so a
		// call to them fails the test
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
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

		mockRepo.On("UpdatePartial", inTx, monitorID, mock.Anything).Return(nil)
		mockRepo.On("FindByID", inTx, monitorID).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, true)

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
		assert.Empty(t, bus.Topics(), "nothing is published when noPublish is true")
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
			Type:   "tcp",
			Name:   "Test Monitor",
			Config: config,
		}

		mockRepo.On("UpdatePartial", inTx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID &&
				m.Config != nil &&
				*m.Config == config
		})).Return(nil)

		mockRepo.On("FindByID", inTx, monitorID).Return(expectedModel, nil)

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false)

		assert.NoError(t, err)
		assert.Equal(t, expectedModel, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("update error", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		updateDto := &PartialUpdateDto{}

		mockRepo.On("FindByID", inTx, monitorID).Return(&Model{ID: monitorID}, nil)
		mockRepo.On("UpdatePartial", inTx, monitorID, mock.Anything).Return(errors.New("update failed"))

		result, err := service.UpdatePartial(ctx, monitorID, updateDto, false)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update failed")
		assert.Empty(t, bus.Topics())
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindByID", inTx, "gone123").Return(nil, nil)
		mockRepo.On("UpdatePartial", mock.Anything, "gone123", mock.Anything).Return(nil).Maybe() // asserted below to be uncalled
		name := "Renamed"

		result, err := service.UpdatePartial(ctx, "gone123", &PartialUpdateDto{Name: &name}, false)

		assert.ErrorIs(t, err, ErrMonitorNotFound)
		assert.Nil(t, result)
		mockRepo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything)
		assert.Equal(t, 1, transactionOf(service).rolledBack)
		assert.Empty(t, bus.Topics(), "nothing is published")
	})

	t.Run("monitor lookup error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindByID", inTx, "monitor123").Return(nil, errors.New("lookup failed"))
		mockRepo.On("UpdatePartial", mock.Anything, "monitor123", mock.Anything).Return(nil).Maybe() // asserted below to be uncalled

		result, err := service.UpdatePartial(ctx, "monitor123", &PartialUpdateDto{}, false)

		require.ErrorContains(t, err, "lookup failed")
		assert.NotErrorIs(t, err, ErrMonitorNotFound)
		assert.Nil(t, result)
		mockRepo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything)
	})

	const validHTTPConfig = `{"url":"https://example.com","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`
	const validTCPConfig = `{"host":"example.com","port":443}`
	const invalidConfig = `{"invalid":"config"}`
	invalid := func(s string) *string { return &s }

	for _, tc := range []struct {
		name   string
		stored Model
		dto    PartialUpdateDto
	}{
		{"a config that does not fit the stored type", Model{Type: "http", Config: validHTTPConfig}, PartialUpdateDto{Config: invalid(invalidConfig)}},
		{"a type that does not fit the stored config", Model{Type: "http", Config: validHTTPConfig}, PartialUpdateDto{Type: invalid("tcp")}},
		{"a type and config that do not fit together", Model{Type: "http", Config: validHTTPConfig}, PartialUpdateDto{Type: invalid("tcp"), Config: invalid(validHTTPConfig)}},
	} {
		t.Run("rejects "+tc.name+" and writes nothing", func(t *testing.T) {
			service, mockRepo, _, bus, _, _, _, _ := setupMonitorService()
			stored := tc.stored
			stored.ID = "monitor123"
			mockRepo.On("FindByID", inTx, "monitor123").Return(&stored, nil)
			mockRepo.On("UpdatePartial", mock.Anything, "monitor123", mock.Anything).Return(nil).Maybe() // asserted below to be uncalled
			dto := tc.dto

			result, err := service.UpdatePartial(ctx, "monitor123", &dto, false)

			var invalidConfig *InvalidConfigError
			require.ErrorAs(t, err, &invalidConfig)
			assert.Error(t, invalidConfig.Err, "it carries why the config is invalid")
			assert.Nil(t, result)
			mockRepo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything)
			assert.Equal(t, 1, transactionOf(service).rolledBack)
			assert.Empty(t, bus.Topics(), "nothing is published")
		})
	}

	for _, tc := range []struct {
		name   string
		stored Model
		dto    PartialUpdateDto
	}{
		{"a config that fits the stored type", Model{Type: "http", Config: validHTTPConfig}, PartialUpdateDto{Config: invalid(validHTTPConfig)}},
		{"a type that fits the stored config", Model{Type: "http", Config: validTCPConfig}, PartialUpdateDto{Type: invalid("tcp")}},
		{"a type and config that fit together", Model{Type: "http", Config: validHTTPConfig}, PartialUpdateDto{Type: invalid("tcp"), Config: invalid(validTCPConfig)}},
		// A status update, like the listener's, or a rename does not check a config it leaves alone
		{"a change that leaves the type and config alone", Model{Type: "http", Config: invalidConfig}, PartialUpdateDto{Name: invalid("Renamed")}},
	} {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
			stored := tc.stored
			stored.ID = "monitor123"
			mockRepo.On("FindByID", inTx, "monitor123").Return(&stored, nil)
			mockRepo.On("UpdatePartial", inTx, "monitor123", mock.Anything).Return(nil)
			dto := tc.dto

			result, err := service.UpdatePartial(ctx, "monitor123", &dto, false)

			require.NoError(t, err)
			assert.Same(t, &stored, result)
			mockRepo.AssertCalled(t, "UpdatePartial", inTx, "monitor123", mock.Anything)
		})
	}

	t.Run("links change to exactly the IDs sent", func(t *testing.T) {
		service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
		monitorID := "monitor123"
		stored := &Model{ID: monitorID, Name: "Test Monitor"}
		mockRepo.On("UpdatePartial", inTx, monitorID, mock.Anything).Return(nil)
		mockRepo.On("FindByID", inTx, monitorID).Return(stored, nil)
		notifications.On("FindByMonitorID", inTx, monitorID).Return([]*monitor_notification.Model{
			{ID: "link-keep", MonitorID: monitorID, NotificationID: "channel-keep"},
			{ID: "link-drop", MonitorID: monitorID, NotificationID: "channel-drop"},
		}, nil)
		notifications.On("Delete", inTx, "link-drop").Return(nil).Once()
		notifications.On("Create", inTx, monitorID, "channel-new").Return(&monitor_notification.Model{}, nil).Once()
		tags.On("FindByMonitorID", inTx, monitorID).Return([]*monitor_tag.Model{
			{ID: "link-tag-drop", MonitorID: monitorID, TagID: "tag-drop"},
		}, nil)
		tags.On("Delete", inTx, "link-tag-drop").Return(nil).Once()
		tags.On("Create", inTx, monitorID, "tag-new").Return(&monitor_tag.Model{}, nil).Once()

		result, err := service.UpdatePartial(ctx, monitorID, &PartialUpdateDto{
			NotificationIds: []string{"channel-keep", "channel-new", "channel-new"},
			TagIds:          []string{"tag-new"},
		}, false)

		require.NoError(t, err)
		assert.Same(t, stored, result)
		assert.Equal(t, 1, transactionOf(service).committed)
		assert.Equal(t, []shared.Monitor{*stored}, eventstest.Published(bus, events.MonitorUpdated))
		notifications.AssertExpectations(t)
		tags.AssertExpectations(t)
		notifications.AssertNotCalled(t, "Create", mock.Anything, monitorID, "channel-keep")
	})

	for _, tc := range []struct {
		name  string
		fails func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService)
	}{
		{"finding the notification channel links", func(notifications *MockMonitorNotificationService, _ *MockMonitorTagService) {
			notifications.On("FindByMonitorID", inTx, "monitor123").Return(([]*monitor_notification.Model)(nil), errors.New("link failed"))
		}},
		{"unlinking a notification channel", func(notifications *MockMonitorNotificationService, _ *MockMonitorTagService) {
			notifications.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_notification.Model{{ID: "link-old", NotificationID: "channel-old"}}, nil)
			notifications.On("Delete", inTx, "link-old").Return(errors.New("link failed"))
		}},
		{"linking a notification channel", func(notifications *MockMonitorNotificationService, _ *MockMonitorTagService) {
			notifications.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_notification.Model{}, nil)
			notifications.On("Create", inTx, "monitor123", "channel-a").Return((*monitor_notification.Model)(nil), errors.New("link failed"))
		}},
		{"unlinking a tag", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_notification.Model{{ID: "link-a", NotificationID: "channel-a"}}, nil)
			tags.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_tag.Model{{ID: "link-old", TagID: "tag-old"}}, nil)
			tags.On("Delete", inTx, "link-old").Return(errors.New("link failed"))
		}},
		{"linking a tag", func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService) {
			notifications.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_notification.Model{{ID: "link-a", NotificationID: "channel-a"}}, nil)
			tags.On("FindByMonitorID", inTx, "monitor123").Return([]*monitor_tag.Model{}, nil)
			tags.On("Create", inTx, "monitor123", "tag-a").Return((*monitor_tag.Model)(nil), errors.New("link failed"))
		}},
	} {
		t.Run("rolls back and publishes nothing when "+tc.name+" fails", func(t *testing.T) {
			service, mockRepo, _, bus, notifications, tags, _, _ := setupMonitorService()
			mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123"}, nil)
			mockRepo.On("UpdatePartial", inTx, "monitor123", mock.Anything).Return(nil)
			tc.fails(notifications, tags)

			result, err := service.UpdatePartial(ctx, "monitor123", &PartialUpdateDto{
				NotificationIds: []string{"channel-a"},
				TagIds:          []string{"tag-a"},
			}, false)

			require.ErrorContains(t, err, "link failed")
			assert.Nil(t, result)
			tx := transactionOf(service)
			assert.Equal(t, 1, tx.rolledBack, "the monitor row and the links are rolled back together")
			assert.Zero(t, tx.committed)
			assert.Empty(t, bus.Topics(), "no MonitorUpdated")
		})
	}
}

func TestMonitorService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("successful deletion", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, bus, mockNotificationService, mockTagService, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		var order []string
		record := func(step string) func(mock.Arguments) {
			return func(mock.Arguments) { order = append(order, step) }
		}
		mockRepo.On("FindByID", inTx, monitorID).Run(record("lookup")).Return(&Model{ID: monitorID}, nil)
		mockNotificationService.On("DeleteByMonitorID", inTx, monitorID).Run(record("notification links")).Return(nil)
		mockTagService.On("DeleteByMonitorID", inTx, monitorID).Run(record("tag links")).Return(nil)
		mockHeartbeatService.On("DeleteByMonitorID", inTx, monitorID).Run(record("heartbeats")).Return(nil)
		mockStatsService.On("DeleteByMonitorID", inTx, monitorID).Run(record("stats")).Return(nil)
		mockRepo.On("Delete", inTx, monitorID).Run(record("monitor")).Return(nil)

		err := service.Delete(ctx, monitorID)

		assert.NoError(t, err)
		assert.Equal(t, []string{"lookup", "heartbeats", "stats", "monitor", "notification links", "tag links"}, order, "the monitor is looked up first, and its links go only after its row, so a live monitor never loses its notification channels")
		assert.Equal(t, []string{monitorID}, eventstest.Published(bus, events.MonitorDeleted))
		tx := transactionOf(service)
		assert.Equal(t, 1, tx.committed, "everything is deleted in one transaction")
		assert.Equal(t, 0, tx.published, "the event is published once the transaction has committed, not before")
		mockRepo.AssertExpectations(t)
		mockNotificationService.AssertExpectations(t)
		mockTagService.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	for _, tc := range []struct {
		name    string
		found   *Model
		lookup  error
		wantErr func(t *testing.T, err error)
	}{
		{"monitor not found", nil, nil, func(t *testing.T, err error) { assert.ErrorIs(t, err, ErrMonitorNotFound) }},
		{"monitor lookup error", nil, errors.New("lookup failed"), func(t *testing.T, err error) {
			require.ErrorContains(t, err, "lookup failed")
			assert.NotErrorIs(t, err, ErrMonitorNotFound)
		}},
	} {
		t.Run(tc.name+" deletes nothing", func(t *testing.T) {
			service, mockRepo, mockHeartbeatService, bus, mockNotificationService, mockTagService, _, mockStatsService := setupMonitorService()
			mockRepo.On("FindByID", inTx, "gone123").Return(tc.found, tc.lookup)
			// asserted below to be uncalled
			mockNotificationService.On("DeleteByMonitorID", mock.Anything, "gone123").Return(nil).Maybe()
			mockTagService.On("DeleteByMonitorID", mock.Anything, "gone123").Return(nil).Maybe()
			mockHeartbeatService.On("DeleteByMonitorID", mock.Anything, "gone123").Return(nil).Maybe()
			mockStatsService.On("DeleteByMonitorID", mock.Anything, "gone123").Return(nil).Maybe()
			mockRepo.On("Delete", mock.Anything, "gone123").Return(nil).Maybe()

			err := service.Delete(ctx, "gone123")

			tc.wantErr(t, err)
			mockNotificationService.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			mockTagService.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			mockHeartbeatService.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			mockStatsService.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			mockRepo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
			assert.Equal(t, 1, transactionOf(service).rolledBack)
			assert.Empty(t, bus.Topics(), "no MonitorDeleted")
		})
	}

	// Each case lets the deletes before the one that fails succeed, and sets up no call after it:
	// a call the test didn't expect fails it, so nothing after the failing step runs.
	for _, tc := range []struct {
		name  string
		setUp func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService)
	}{
		{"the heartbeats", func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService) {
			heartbeats.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("delete failed"))
		}},
		{"the stats", func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService) {
			heartbeats.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			statsService.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("delete failed"))
		}},
		{"the monitor", func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService) {
			heartbeats.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			statsService.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			repo.On("Delete", inTx, "monitor123").Return(errors.New("delete failed"))
		}},
		{"the notification channel links", func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService) {
			heartbeats.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			statsService.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			repo.On("Delete", inTx, "monitor123").Return(nil)
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("delete failed"))
		}},
		{"the tag links", func(repo *MockMonitorRepository, heartbeats *MockHeartbeatService, notifications *MockMonitorNotificationService, tags *MockMonitorTagService, statsService *MockStatsService) {
			heartbeats.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			statsService.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			repo.On("Delete", inTx, "monitor123").Return(nil)
			notifications.On("DeleteByMonitorID", inTx, "monitor123").Return(nil)
			tags.On("DeleteByMonitorID", inTx, "monitor123").Return(errors.New("delete failed"))
		}},
	} {
		t.Run("returns the error, rolls back and publishes nothing when deleting "+tc.name+" fails", func(t *testing.T) {
			service, mockRepo, mockHeartbeatService, bus, mockNotificationService, mockTagService, _, mockStatsService := setupMonitorService()
			mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123"}, nil)
			tc.setUp(mockRepo, mockHeartbeatService, mockNotificationService, mockTagService, mockStatsService)

			err := service.Delete(ctx, "monitor123")

			require.ErrorContains(t, err, "delete failed")
			tx := transactionOf(service)
			assert.Equal(t, 1, tx.rolledBack, "what was deleted before the failure is rolled back")
			assert.Zero(t, tx.committed)
			assert.Empty(t, bus.Topics(), "no MonitorDeleted")
		})
	}
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
		service, _, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		limit := 10
		page := 1
		important := true
		reverse := false

		expectedHeartbeats := []*heartbeat.Model{
			{ID: "hb1", MonitorID: monitorID},
			{ID: "hb2", MonitorID: monitorID},
		}

		mockHeartbeatService.On("FindByMonitorIDAndProbeIDPaginated", ctx, monitorID, "probe-frankfurt", limit, page, &important, reverse).Return(expectedHeartbeats, nil)

		result, err := service.GetHeartbeats(ctx, monitorID, "probe-frankfurt", limit, page, &important, reverse)

		assert.NoError(t, err)
		assert.Equal(t, expectedHeartbeats, result)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("heartbeat service error", func(t *testing.T) {
		service, _, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		limit := 10
		page := 1

		mockHeartbeatService.On("FindByMonitorIDAndProbeIDPaginated", ctx, monitorID, "", limit, page, (*bool)(nil), false).Return(([]*heartbeat.Model)(nil), errors.New("service error"))

		result, err := service.GetHeartbeats(ctx, monitorID, "", limit, page, nil, false)

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

		monitor := &Model{
			ID:       monitorID,
			Interval: 60,
		}

		statsList := []*stats.Stat{
			{
				Up:          10,
				Down:        2,
				Maintenance: 0,
				Ping:        50.5,
				PingMin:     30.0,
				PingMax:     70.0,
				Timestamp:   since,
			},
		}

		summary := &stats.Stats{
			MaxPing: &[]float64{70.0}[0],
			MinPing: &[]float64{30.0}[0],
			AvgPing: &[]float64{50.5}[0],
			Uptime:  &[]float64{83.33}[0],
		}

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRangeWithInterval", ctx, monitorID, "probe-frankfurt", since, until, stats.StatHourly, monitor.Interval).Return(statsList, nil)
		mockStatsService.On("StatPointsSummary", statsList).Return(summary)

		result, err := service.GetStatPoints(ctx, monitorID, "probe-frankfurt", since, until, granularity)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "probe-frankfurt", result.ProbeID)
		assert.Len(t, result.Points, 1)
		assert.Equal(t, 10, result.Points[0].Up)
		assert.Equal(t, 2, result.Points[0].Down)
		assert.Equal(t, 0, result.Points[0].Maintenance)
		assert.Equal(t, 50.5, result.Points[0].Ping)
		assert.Equal(t, 30.0, result.Points[0].PingMin)
		assert.Equal(t, 70.0, result.Points[0].PingMax)
		assert.Equal(t, since.Unix()*1000, result.Points[0].Timestamp)
		assert.Equal(t, summary.MaxPing, result.MaxPing)
		assert.Equal(t, summary.MinPing, result.MinPing)
		assert.Equal(t, summary.AvgPing, result.AvgPing)
		assert.Equal(t, summary.Uptime, result.Uptime)

		mockRepo.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("invalid granularity", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now()
		granularity := "invalid"

		result, err := service.GetStatPoints(ctx, monitorID, "", since, until, granularity)

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

		mockRepo.On("FindByID", ctx, monitorID).Return((*Model)(nil), errors.New("not found"))

		result, err := service.GetStatPoints(ctx, monitorID, "", since, until, granularity)

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

		mockRepo.On("FindByID", ctx, monitorID).Return((*Model)(nil), nil)

		result, err := service.GetStatPoints(ctx, monitorID, "", since, until, granularity)

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

		monitor := &Model{
			ID:       monitorID,
			Interval: 60,
		}

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockStatsService.On("FindStatsByMonitorIDAndTimeRangeWithInterval", ctx, monitorID, "", since, until, stats.StatMinutely, monitor.Interval).Return(([]*stats.Stat)(nil), errors.New("stats error"))

		result, err := service.GetStatPoints(ctx, monitorID, "", since, until, granularity)

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
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		// Mock calls for each time period
		for window, uptime := range map[time.Duration]float64{
			24 * time.Hour:       99,
			7 * 24 * time.Hour:   98,
			30 * 24 * time.Hour:  97,
			365 * 24 * time.Hour: 96,
		} {
			mockStatsService.On("Summarize", ctx, monitorID, "probe-frankfurt", sinceAgo(window), mock.AnythingOfType("time.Time")).Return(&stats.Stats{Uptime: &uptime}, nil).Once()
		}

		result, err := service.GetUptimeStats(ctx, monitorID, "probe-frankfurt")

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "probe-frankfurt", result.ProbeID)
		assert.Equal(t, 99.0, result.Uptime24h)
		assert.Equal(t, 98.0, result.Uptime7d)
		assert.Equal(t, 97.0, result.Uptime30d)
		assert.Equal(t, 96.0, result.Uptime365d)

		mockStatsService.AssertExpectations(t)
	})

	t.Run("nil uptime in summary", func(t *testing.T) {
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		summary := &stats.Stats{
			Uptime: nil,
		}

		mockStatsService.On("Summarize", ctx, monitorID, "", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(summary, nil).Times(4)

		result, err := service.GetUptimeStats(ctx, monitorID, "")

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 0.0, result.Uptime24h)
		assert.Equal(t, 0.0, result.Uptime7d)
		assert.Equal(t, 0.0, result.Uptime30d)
		assert.Equal(t, 0.0, result.Uptime365d)

		mockStatsService.AssertExpectations(t)
	})

	t.Run("stats service error", func(t *testing.T) {
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		monitorID := "monitor123"

		mockStatsService.On("Summarize", ctx, monitorID, "", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil, errors.New("stats error"))

		result, err := service.GetUptimeStats(ctx, monitorID, "")

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "stats error")
		mockStatsService.AssertExpectations(t)
	})
}

func TestMonitorService_GetUptime24h(t *testing.T) {
	ctx := context.Background()

	t.Run("reads the last 24 hours of one probe with one stats read", func(t *testing.T) {
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		uptime := 87.5
		mockStatsService.On("Summarize", ctx, "monitor123", "probe-frankfurt", sinceAgo(24*time.Hour), mock.AnythingOfType("time.Time")).Return(&stats.Stats{Uptime: &uptime}, nil).Once()

		result, err := service.GetUptime24h(ctx, "monitor123", "probe-frankfurt")

		assert.NoError(t, err)
		assert.Equal(t, 87.5, result)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("no checks in the period is 0", func(t *testing.T) {
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		mockStatsService.On("Summarize", ctx, "monitor123", "", sinceAgo(24*time.Hour), mock.AnythingOfType("time.Time")).Return(&stats.Stats{}, nil).Once()

		result, err := service.GetUptime24h(ctx, "monitor123", "")

		assert.NoError(t, err)
		assert.Equal(t, 0.0, result)
	})

	t.Run("a stats error is returned", func(t *testing.T) {
		service, _, _, _, _, _, _, mockStatsService := setupMonitorService()
		mockStatsService.On("Summarize", ctx, "monitor123", "p", sinceAgo(24*time.Hour), mock.AnythingOfType("time.Time")).Return(nil, errors.New("stats error")).Once()

		_, err := service.GetUptime24h(ctx, "monitor123", "p")

		assert.Error(t, err)
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

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil).Once()
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockRepo.On("ResetLocations", ctx, monitorID).Return(nil)
		mockRepo.On("UpdatePartial", ctx, monitorID, mock.MatchedBy(func(m *UpdateModel) bool {
			return *m.ID == monitorID && *m.Status == shared.MonitorStatusPending
		})).Return(nil)
		mockRepo.On("FindByID", ctx, monitorID).Return(updatedMonitor, nil).Once()

		err := service.ResetMonitorData(ctx, monitorID)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
		mockStatsService.AssertExpectations(t)
	})

	t.Run("monitor not found", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "nonexistent"

		mockRepo.On("FindByID", ctx, monitorID).Return((*Model)(nil), errors.New("not found"))

		err := service.ResetMonitorData(ctx, monitorID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		mockRepo.AssertExpectations(t)
	})

	t.Run("monitor is nil", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		monitorID := "monitor123"

		mockRepo.On("FindByID", ctx, monitorID).Return((*Model)(nil), nil)

		err := service.ResetMonitorData(ctx, monitorID)

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

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("heartbeat delete failed"))

		err := service.ResetMonitorData(ctx, monitorID)

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

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(errors.New("stats delete failed"))

		err := service.ResetMonitorData(ctx, monitorID)

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

		mockRepo.On("FindByID", ctx, monitorID).Return(monitor, nil)
		mockHeartbeatService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockStatsService.On("DeleteByMonitorID", ctx, monitorID).Return(nil)
		mockRepo.On("ResetLocations", ctx, monitorID).Return(nil)
		mockRepo.On("UpdatePartial", ctx, monitorID, mock.Anything).Return(errors.New("update failed"))

		err := service.ResetMonitorData(ctx, monitorID)

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

		mockRepo.On("FindAll", ctx, page, limit, q, &active, &status, tagIds).Return(expectedMonitors, nil)

		result, err := service.FindAll(ctx, page, limit, q, &active, &status, tagIds)

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindAll", ctx, 1, 10, "", (*bool)(nil), (*int)(nil), []string(nil)).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindAll(ctx, 1, 10, "", nil, nil, nil)

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

		mockRepo.On("FindByIDs", ctx, ids).Return(expectedMonitors, nil)

		result, err := service.FindByIDs(ctx, ids)

		assert.NoError(t, err)
		assert.Equal(t, expectedMonitors, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("repository error", func(t *testing.T) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		ids := []string{"monitor1"}

		mockRepo.On("FindByIDs", ctx, ids).Return(([]*Model)(nil), errors.New("repository error"))

		result, err := service.FindByIDs(ctx, ids)

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

	eventBus := eventstest.NewBus()
	transactor := &fakeTransactor{bus: eventBus}

	// Create real instances for dependencies that expect concrete types
	realExecutorRegistry := executor.NewExecutorRegistry(logger)

	service := NewMonitorService(
		mockRepo,
		transactor,
		mockHeartbeatService,
		eventBus,
		mockNotificationService,
		mockTagService,
		realExecutorRegistry,
		mockStatsService,
		nil,
		logger,
	)

	assert.NotNil(t, service)
	assert.IsType(t, &MonitorServiceImpl{}, service)

	serviceImpl := service.(*MonitorServiceImpl)
	assert.Equal(t, mockRepo, serviceImpl.monitorRepository)
	assert.Equal(t, transactor, serviceImpl.transactor)
	assert.Equal(t, mockHeartbeatService, serviceImpl.heartbeatService)
	assert.Equal(t, eventBus, serviceImpl.eventBus)
	assert.Equal(t, mockNotificationService, serviceImpl.monitorNotificationService)
	assert.Equal(t, mockTagService, serviceImpl.monitorTagService)
	assert.Equal(t, realExecutorRegistry, serviceImpl.executorRegistry)
	assert.Equal(t, mockStatsService, serviceImpl.statPointsService)
	assert.NotNil(t, serviceImpl.logger)
}

func TestMonitorService_RepairStoredStatuses(t *testing.T) {
	ctx := context.Background()
	unset, noStatus, noTags := (*bool)(nil), (*int)(nil), []string(nil)

	t.Run("a monitor that fails does not stop the others", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		mockRepo.On("FindAll", ctx, 0, StatusRepairPageSize, "", unset, noStatus, noTags).Return([]*Model{
			{ID: "unreadable", Status: shared.MonitorStatusUp},
			{ID: "unwritable", Status: shared.MonitorStatusUp},
			{ID: "stale", Status: shared.MonitorStatusUp},
		}, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, "unreadable", 1, 0, unset, false).Return([]*heartbeat.Model(nil), errors.New("heartbeats unavailable"))
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, "unwritable", 1, 0, unset, false).Return([]*heartbeat.Model{{Status: shared.MonitorStatusDown}}, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, "stale", 1, 0, unset, false).Return([]*heartbeat.Model{{Status: shared.MonitorStatusDown}}, nil)
		isDown := mock.MatchedBy(func(m *UpdateModel) bool { return *m.Status == shared.MonitorStatusDown })
		mockRepo.On("UpdatePartial", ctx, "unwritable", isDown).Return(errors.New("database is read-only"))
		mockRepo.On("UpdatePartial", ctx, "stale", isDown).Return(nil)

		repaired, err := service.RepairStoredStatuses(ctx)

		assert.Equal(t, 1, repaired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "heartbeats unavailable")
		assert.ErrorContains(t, err, "database is read-only")
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("a failed listing ends the walk with the count so far", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		fullPage := []*Model{{ID: "stale", Status: shared.MonitorStatusUp}}
		for len(fullPage) < StatusRepairPageSize {
			fullPage = append(fullPage, &Model{ID: "silent", Status: shared.MonitorStatusUp})
		}
		mockRepo.On("FindAll", ctx, 0, StatusRepairPageSize, "", unset, noStatus, noTags).Return(fullPage, nil)
		mockRepo.On("FindAll", ctx, 1, StatusRepairPageSize, "", unset, noStatus, noTags).Return([]*Model(nil), errors.New("connection lost"))
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, "stale", 1, 0, unset, false).Return([]*heartbeat.Model{{Status: shared.MonitorStatusDown}}, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", ctx, "silent", 1, 0, unset, false).Return([]*heartbeat.Model(nil), nil)
		mockRepo.On("UpdatePartial", ctx, "stale", mock.Anything).Return(nil)

		repaired, err := service.RepairStoredStatuses(ctx)

		assert.Equal(t, 1, repaired)
		assert.ErrorContains(t, err, "connection lost")
		mockRepo.AssertExpectations(t)
	})

	t.Run("a cancelled context ends the sweep with the count so far", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		mockRepo.Test(t)
		mockHeartbeatService.Test(t)
		cancelled, cancel := context.WithCancel(ctx)
		defer cancel()
		// "after" has no expectations, so reading or writing it fails the test
		mockRepo.On("FindAll", cancelled, 0, StatusRepairPageSize, "", unset, noStatus, noTags).Return([]*Model{
			{ID: "stale", Status: shared.MonitorStatusUp},
			{ID: "after", Status: shared.MonitorStatusUp},
		}, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", cancelled, "stale", 1, 0, unset, false).Return([]*heartbeat.Model{{Status: shared.MonitorStatusDown}}, nil)
		mockRepo.On("UpdatePartial", cancelled, "stale", mock.Anything).Run(func(mock.Arguments) { cancel() }).Return(nil)

		repaired, err := service.RepairStoredStatuses(cancelled)

		assert.Equal(t, 1, repaired)
		assert.ErrorIs(t, err, context.Canceled)
		mockRepo.AssertExpectations(t)
		mockHeartbeatService.AssertExpectations(t)
	})

	t.Run("a cancelled context does not list the next page", func(t *testing.T) {
		service, mockRepo, mockHeartbeatService, _, _, _, _, _ := setupMonitorService()
		mockRepo.Test(t)
		mockHeartbeatService.Test(t)
		cancelled, cancel := context.WithCancel(ctx)
		defer cancel()
		fullPage := []*Model{}
		for len(fullPage) < StatusRepairPageSize-1 {
			fullPage = append(fullPage, &Model{ID: "silent", Status: shared.MonitorStatusUp})
		}
		fullPage = append(fullPage, &Model{ID: "stale", Status: shared.MonitorStatusUp})
		mockRepo.On("FindAll", cancelled, 0, StatusRepairPageSize, "", unset, noStatus, noTags).Return(fullPage, nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", cancelled, "silent", 1, 0, unset, false).Return([]*heartbeat.Model(nil), nil)
		mockHeartbeatService.On("FindByMonitorIDPaginated", cancelled, "stale", 1, 0, unset, false).Return([]*heartbeat.Model{{Status: shared.MonitorStatusDown}}, nil)
		mockRepo.On("UpdatePartial", cancelled, "stale", mock.Anything).Run(func(mock.Arguments) { cancel() }).Return(nil)

		repaired, err := service.RepairStoredStatuses(cancelled)

		assert.Equal(t, 1, repaired)
		assert.ErrorIs(t, err, context.Canceled)
		mockRepo.AssertNotCalled(t, "FindAll", cancelled, 1, StatusRepairPageSize, "", unset, noStatus, noTags)
	})
}

// fakeProbes is a ProbeLookup over the probes it holds, by id
type fakeProbes struct {
	probes map[string]*ProbeInfo
	err    error
}

func (f fakeProbes) FindProbe(_ context.Context, id string) (*ProbeInfo, error) {
	return f.probes[id], f.err
}

// The types a probe lists are the ones it has an executor for. A push monitor needs none: its pushes
// arrive at the api, so it runs on the local probe whatever that probe lists, and peekaping-probe
// lists every type but push. The local probe of a cluster is its instances, which report exactly that.
func TestMonitorService_APushMonitorIsNeverRefusedForTheTypesAProbeRuns(t *testing.T) {
	ctx := context.Background()
	probes := fakeProbes{probes: map[string]*ProbeInfo{
		"local": {Name: "Local", SupportedTypes: []string{"http", "tcp", "dns"}},
	}}
	newService := func() (*MonitorServiceImpl, *MockMonitorRepository) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		service.probes = probes
		return service, mockRepo
	}

	t.Run("create", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("Create", inTx, mock.Anything).Return(&Model{ID: "monitor123"}, nil)

		_, err := service.Create(ctx, &CreateUpdateDto{Type: "push", Name: "Nightly cron", Config: "{}", ProbeIDs: []string{"local"}})

		require.NoError(t, err)
	})

	t.Run("an http monitor still needs a probe that runs http", func(t *testing.T) {
		service, _ := newService()

		_, err := service.Create(ctx, &CreateUpdateDto{Type: "docker", Name: "Containers", Config: "{}", ProbeIDs: []string{"local"}})

		require.EqualError(t, err, `invalid probe_ids: probe "Local" doesn't run docker monitors`)
	})

	t.Run("a monitor becomes a push monitor", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123", Type: "http", ProbeIDs: []string{"local"}}, nil)
		mockRepo.On("UpdatePartial", mock.Anything, "monitor123", mock.Anything).Return(nil).Maybe()
		mockRepo.On("FindByID", mock.Anything, "monitor123").Return(&Model{ID: "monitor123", Type: "push", ProbeIDs: []string{"local"}}, nil).Maybe()
		pushType := "push"

		_, err := service.UpdatePartial(ctx, "monitor123", &PartialUpdateDto{Type: &pushType}, false)

		var invalid *InvalidProbesError
		assert.False(t, errors.As(err, &invalid), "the local probe lists no push, and push needs no executor: %v", err)
	})
}

// A graph without probes: the probe side says so, and only the local probe, which is all there is,
// is accepted.
func TestMonitorService_WithoutProbesOnlyAcceptsLocal(t *testing.T) {
	ctx := context.Background()
	newService := func() (*MonitorServiceImpl, *MockMonitorRepository) {
		service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
		service.probes = fakeProbes{err: ErrNoProbes}
		return service, mockRepo
	}
	dto := func(probeIDs ...string) *CreateUpdateDto {
		return &CreateUpdateDto{Type: "http", Name: "Checkout", Config: "{}", ProbeIDs: probeIDs}
	}

	t.Run("local is accepted", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("Create", inTx, mock.MatchedBy(func(m *Model) bool { return slices.Equal(m.ProbeIDs, []string{"local"}) })).Return(&Model{ID: "monitor123"}, nil)

		_, err := service.Create(ctx, dto("local"))

		require.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("a monitor without probes is accepted", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("Create", inTx, mock.MatchedBy(func(m *Model) bool { return m.ProbeIDs == nil })).Return(&Model{ID: "monitor123"}, nil)

		_, err := service.Create(ctx, &CreateUpdateDto{Type: "http", Name: "Checkout", Config: "{}"})

		require.NoError(t, err)
	})

	t.Run("any other probe is refused", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("Create", mock.Anything, mock.Anything).Return(&Model{ID: "monitor123"}, nil).Maybe() // asserted below to be uncalled

		_, err := service.Create(ctx, dto("frankfurt"))

		var invalid *InvalidProbesError
		require.ErrorAs(t, err, &invalid)
		assert.ErrorIs(t, err, ErrNoProbes)
		assert.EqualError(t, err, `invalid probe_ids: probe "frankfurt" can't be used: probes aren't available here`)
		mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	})

	t.Run("a partial update to another probe is refused", func(t *testing.T) {
		service, mockRepo := newService()
		mockRepo.On("FindByID", inTx, "monitor123").Return(&Model{ID: "monitor123", Type: "http", ProbeIDs: []string{"local"}}, nil)
		mockRepo.On("UpdatePartial", mock.Anything, "monitor123", mock.Anything).Return(nil).Maybe() // asserted below to be uncalled

		_, err := service.UpdatePartial(ctx, "monitor123", &PartialUpdateDto{ProbeIDs: []string{"frankfurt"}}, false)

		var invalid *InvalidProbesError
		require.ErrorAs(t, err, &invalid)
		mockRepo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestMonitorService_ChecksTheProbeOfAMonitor(t *testing.T) {
	ctx := context.Background()
	probes := fakeProbes{probes: map[string]*ProbeInfo{
		"local":     {Name: "Local"},
		"frankfurt": {Name: "Frankfurt", SupportedTypes: []string{"http", "tcp"}},
		"paris":     {Name: "Paris", SupportedTypes: []string{"dns"}},
	}}

	for _, tc := range []struct {
		name     string
		typ      string
		probeIDs []string
		want     string // the reason, empty when the monitor is accepted
	}{
		{"one probe that runs the type", "http", []string{"frankfurt"}, ""},
		{"a probe with no types runs every type", "docker", []string{"local"}, ""},
		{"no probes", "http", []string{}, "a monitor runs on at least one probe"},
		{"two probes that run the type", "http", []string{"local", "frankfurt"}, ""},
		{"the same probe twice", "http", []string{"frankfurt", "frankfurt"}, `probe "frankfurt" is listed twice`},
		{"one of two probes runs other types", "http", []string{"frankfurt", "paris"}, `probe "Paris" doesn't run http monitors`},
		{"a push monitor on local and a remote probe", "push", []string{"local", "frankfurt"}, "a push monitor runs on the local probe only"},
		{"an unknown probe", "http", []string{"berlin"}, `probe "berlin" doesn't exist`},
		{"a probe that runs other types", "http", []string{"paris"}, `probe "Paris" doesn't run http monitors`},
		{"a push monitor on a remote probe", "push", []string{"frankfurt"}, "a push monitor runs on the local probe only"},
		{"a push monitor on the local probe", "push", []string{"local"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, mockRepo, _, _, _, _, _, _ := setupMonitorService()
			service.probes = probes
			mockRepo.On("Create", inTx, mock.Anything).Return(&Model{ID: "monitor123"}, nil).Maybe()

			_, err := service.Create(ctx, &CreateUpdateDto{Type: tc.typ, Name: "Checkout", Config: "{}", ProbeIDs: tc.probeIDs})

			if tc.want == "" {
				require.NoError(t, err)
				mockRepo.AssertCalled(t, "Create", inTx, mock.Anything)
				return
			}
			require.EqualError(t, err, "invalid probe_ids: "+tc.want)
			mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
		})
	}

	t.Run("a failure to look the probe up is not the client's", func(t *testing.T) {
		service, _, _, _, _, _, _, _ := setupMonitorService()
		service.probes = fakeProbes{err: errors.New("database is down")}

		_, err := service.Create(ctx, &CreateUpdateDto{Type: "http", Name: "Checkout", ProbeIDs: []string{"local"}})

		require.EqualError(t, err, "database is down")
		var invalid *InvalidProbesError
		assert.False(t, errors.As(err, &invalid))
	})
}
