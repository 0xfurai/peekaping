package monitor_tls_info

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository is a mock implementation for testing
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) GetByMonitorID(ctx context.Context, monitorID string) (*Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Create(ctx context.Context, dto *CreateDto) (*Model, error) {
	args := m.Called(ctx, dto)
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, monitorID string, dto *UpdateDto) (*Model, error) {
	args := m.Called(ctx, monitorID, dto)
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Upsert(ctx context.Context, monitorID string, infoJSON string) (*Model, error) {
	args := m.Called(ctx, monitorID, infoJSON)
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockRepository) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	args := m.Called(ctx, olderThanDays)
	return args.Error(0)
}

type TestTLSInfo struct {
	Valid    bool   `json:"valid"`
	CertInfo string `json:"cert_info"`
}

func TestMonitorTLSInfoService(t *testing.T) {
	logger := zap.NewNop().Sugar()
	mockRepo := new(MockRepository)
	service := NewService(mockRepo, logger)

	ctx := context.Background()
	monitorID := "test-monitor-123"

	t.Run("StoreTLSInfoObject and GetTLSInfoObject", func(t *testing.T) {
		testInfo := TestTLSInfo{
			Valid:    true,
			CertInfo: "test-certificate-info",
		}

		expectedJSON := `{"valid":true,"cert_info":"test-certificate-info"}`
		mockRepo.On("Upsert", ctx, monitorID, expectedJSON).Return(&Model{
			ID:        "test-id-1",
			MonitorID: monitorID,
			InfoJSON:  expectedJSON,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil)

		// Store TLS info object
		err := service.StoreTLSInfoObject(ctx, monitorID, testInfo)
		assert.NoError(t, err)

		// Get TLS info object
		mockRepo.ExpectedCalls = nil // Reset expectations
		mockRepo.On("GetByMonitorID", ctx, monitorID).Return(&Model{
			ID:        "test-id-1",
			MonitorID: monitorID,
			InfoJSON:  expectedJSON,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil)

		var retrievedInfo TestTLSInfo
		err = service.GetTLSInfoObject(ctx, monitorID, &retrievedInfo)
		assert.NoError(t, err)
		assert.Equal(t, testInfo.Valid, retrievedInfo.Valid)
		assert.Equal(t, testInfo.CertInfo, retrievedInfo.CertInfo)

		mockRepo.AssertExpectations(t)
	})

	t.Run("GetTLSInfoObject - no data found", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		mockRepo.On("GetByMonitorID", ctx, monitorID).Return((*Model)(nil), nil)

		var retrievedInfo TestTLSInfo
		err := service.GetTLSInfoObject(ctx, monitorID, &retrievedInfo)
		assert.NoError(t, err)
		// Should return zero values when no data found
		assert.False(t, retrievedInfo.Valid)
		assert.Empty(t, retrievedInfo.CertInfo)

		mockRepo.AssertExpectations(t)
	})

	t.Run("DeleteTLSInfo", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		mockRepo.On("Delete", ctx, monitorID).Return(nil)

		err := service.DeleteTLSInfo(ctx, monitorID)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})

	t.Run("CleanupOldRecords", func(t *testing.T) {
		mockRepo.ExpectedCalls = nil // Reset expectations
		olderThanDays := 30
		mockRepo.On("CleanupOldRecords", ctx, olderThanDays).Return(nil)

		err := service.CleanupOldRecords(ctx, olderThanDays)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})
}
