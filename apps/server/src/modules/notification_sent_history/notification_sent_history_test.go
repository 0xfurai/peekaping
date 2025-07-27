package notification_sent_history

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository is a mock implementation of Repository for testing
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) CheckIfSent(ctx context.Context, notificationType string, monitorID string, days int) (bool, error) {
	args := m.Called(ctx, notificationType, monitorID, days)
	return args.Bool(0), args.Error(1)
}

func (m *MockRepository) RecordSent(ctx context.Context, dto *CreateDto) error {
	args := m.Called(ctx, dto)
	return args.Error(0)
}

func (m *MockRepository) ClearByMonitorAndType(ctx context.Context, monitorID string, notificationType string) error {
	args := m.Called(ctx, monitorID, notificationType)
	return args.Error(0)
}

func (m *MockRepository) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	args := m.Called(ctx, olderThanDays)
	return args.Error(0)
}

func (m *MockRepository) GetByMonitorAndType(ctx context.Context, monitorID string, notificationType string) ([]*Model, error) {
	args := m.Called(ctx, monitorID, notificationType)
	return args.Get(0).([]*Model), args.Error(1)
}

func TestNotificationSentHistoryService(t *testing.T) {
	logger := zap.NewNop().Sugar()
	mockRepo := new(MockRepository)
	service := NewService(mockRepo, logger)

	ctx := context.Background()
	monitorID := "test-monitor-123"
	notificationType := "certificate"
	targetDays := 7

	t.Run("CheckIfNotificationSent - not sent yet", func(t *testing.T) {
		mockRepo.On("CheckIfSent", ctx, notificationType, monitorID, targetDays).Return(false, nil)

		sent, err := service.CheckIfNotificationSent(ctx, notificationType, monitorID, targetDays)

		assert.NoError(t, err)
		assert.False(t, sent)
		mockRepo.AssertExpectations(t)
	})

	t.Run("CheckIfNotificationSent - already sent", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		mockRepo.On("CheckIfSent", ctx, notificationType, monitorID, targetDays).Return(true, nil)

		sent, err := service.CheckIfNotificationSent(ctx, notificationType, monitorID, targetDays)

		assert.NoError(t, err)
		assert.True(t, sent)
		mockRepo.AssertExpectations(t)
	})

	t.Run("RecordNotificationSent", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		expectedDto := &CreateDto{
			Type:      notificationType,
			MonitorID: monitorID,
			Days:      targetDays,
		}
		mockRepo.On("RecordSent", ctx, mock.MatchedBy(func(dto *CreateDto) bool {
			return dto.Type == expectedDto.Type &&
				dto.MonitorID == expectedDto.MonitorID &&
				dto.Days == expectedDto.Days
		})).Return(nil)

		err := service.RecordNotificationSent(ctx, notificationType, monitorID, targetDays)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("ClearNotificationHistory", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		mockRepo.On("ClearByMonitorAndType", ctx, monitorID, notificationType).Return(nil)

		err := service.ClearNotificationHistory(ctx, monitorID, notificationType)

		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("GetNotificationHistory", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		expectedHistory := []*Model{
			{
				ID:        "test-id-1",
				Type:      notificationType,
				MonitorID: monitorID,
				Days:      7,
				CreatedAt: time.Now(),
			},
			{
				ID:        "test-id-2",
				Type:      notificationType,
				MonitorID: monitorID,
				Days:      14,
				CreatedAt: time.Now(),
			},
		}
		mockRepo.On("GetByMonitorAndType", ctx, monitorID, notificationType).Return(expectedHistory, nil)

		history, err := service.GetNotificationHistory(ctx, monitorID, notificationType)

		assert.NoError(t, err)
		assert.Len(t, history, 2)
		assert.Equal(t, 7, history[0].Days)
		assert.Equal(t, 14, history[1].Days)
		mockRepo.AssertExpectations(t)
	})
}

func TestNotificationDeduplicationFlow(t *testing.T) {
	logger := zap.NewNop().Sugar()
	mockRepo := new(MockRepository)
	service := NewService(mockRepo, logger)

	ctx := context.Background()
	monitorID := "test-monitor-456"
	notificationType := "certificate"

	// Simulate the flow from uptime-kuma:
	// 1. Check if 21-day notification was sent
	// 2. If not sent, record it
	// 3. Check if 14-day notification was sent
	// 4. Should not send 14-day if 21-day was already sent

	t.Run("Certificate expiry notification flow", func(t *testing.T) {
		// Certificate expires in 15 days, check thresholds [7, 14, 21]

		// Check 21-day threshold (15 <= 21, so should notify)
		mockRepo.On("CheckIfSent", ctx, notificationType, monitorID, 21).Return(false, nil)
		mockRepo.On("RecordSent", ctx, mock.AnythingOfType("*notification_sent_history.CreateDto")).Return(nil)

		// Check 14-day threshold (15 <= 14 is false, so won't even check)
		// Check 7-day threshold (15 <= 7 is false, so won't even check)

		// Test 21-day notification
		sent21, err := service.CheckIfNotificationSent(ctx, notificationType, monitorID, 21)
		assert.NoError(t, err)
		assert.False(t, sent21) // Should send notification

		err = service.RecordNotificationSent(ctx, notificationType, monitorID, 21)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})
}
