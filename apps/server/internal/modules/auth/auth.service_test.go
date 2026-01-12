package auth

import (
	"context"
	"testing"
	"vigi/internal/config"
	"vigi/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, user *Model) (*Model, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindByEmail(ctx context.Context, email string) (*Model, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindByID(ctx context.Context, id string) (*Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindAllCount(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, id string, entity *UpdateModel) error {
	args := m.Called(ctx, id, entity)
	return args.Error(0)
}

// MockTokenMaker logic replaced by MockSettingService to support real TokenMaker
// MockAuthTestSettingService
type MockAuthTestSettingService struct {
	mock.Mock
}

func (m *MockAuthTestSettingService) GetByKey(ctx context.Context, key string) (*shared.SettingModel, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.SettingModel), args.Error(1)
}

func (m *MockAuthTestSettingService) SetByKey(ctx context.Context, key string, entity *shared.SettingCreateUpdateDto) (*shared.SettingModel, error) {
	args := m.Called(ctx, key, entity)
	return args.Get(0).(*shared.SettingModel), args.Error(1)
}

func (m *MockAuthTestSettingService) DeleteByKey(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockAuthTestSettingService) InitializeSettings(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Add other methods of SettingService if any (it likely has more, but TokenMaker only uses GetByKey)
// Checking shared/setting.go might be needed if interface has more methods.
// Assuming shared.SettingService has only GetByKey for now based on TokenMaker usage.
// Wait, NewTokenMaker needs the interface. I should verify the interface definition if I get compilation error.
// For now I'll include likely methods or generic mock.
// Actually, I'll list `shared/setting_service.go` if it fails again. But let's try to mock GetByKey first.

func createTestService(t *testing.T, repo Repository, cfg *config.Config, settingService *MockAuthTestSettingService) Service {
	logger, _ := zap.NewDevelopment()
	sugar := logger.Sugar()

	// Setup default setting mocks for TokenMaker
	settingService.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{Value: "15m"}, nil).Maybe()
	settingService.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(&shared.SettingModel{Value: "12345678901234567890123456789012"}, nil).Maybe()
	settingService.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{Value: "24h"}, nil).Maybe()
	settingService.On("GetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY").Return(&shared.SettingModel{Value: "12345678901234567890123456789012"}, nil).Maybe()

	tokenMaker := NewTokenMaker(settingService, sugar)
	return NewService(repo, tokenMaker, sugar, cfg)
}

func TestServiceImpl_Register_SingleAdminMode(t *testing.T) {
	tests := []struct {
		name              string
		enableSingleAdmin bool
		existingCount     int64
		expectError       bool
		errorMsg          string
	}{
		{
			name:              "SingleAdmin=true, Count=1 -> Error",
			enableSingleAdmin: true,
			existingCount:     1,
			expectError:       true,
			errorMsg:          "admin already exists",
		},
		{
			name:              "SingleAdmin=true, Count=0 -> Success",
			enableSingleAdmin: true,
			existingCount:     0,
			expectError:       false,
		},
		{
			name:              "SingleAdmin=false, Count=1 -> Success",
			enableSingleAdmin: false,
			existingCount:     1,
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockSettings := &MockAuthTestSettingService{}
			cfg := &config.Config{
				EnableSingleAdmin: tt.enableSingleAdmin,
			}
			service := createTestService(t, mockRepo, cfg, mockSettings)

			// Mocks
			if tt.enableSingleAdmin {
				mockRepo.On("FindAllCount", mock.Anything).Return(tt.existingCount, nil)
			}
			// If disabled, FindAllCount is skipped (unless verifying that call doesn't happen, but impl says if s.cfg.EnableSingleAdmin...)
			// Wait, if !EnableSingleAdmin, we DON'T call FindAllCount. So we don't mock it.

			if !tt.expectError {
				// Continue to creation flow
				mockRepo.On("FindByEmail", mock.Anything, "test@example.com").Return(nil, nil)
				mockRepo.On("Create", mock.Anything, mock.Anything).Return(&Model{ID: "new-user"}, nil)
			}

			dto := RegisterDto{
				Email:    "test@example.com",
				Password: "password123",
			}

			_, err := service.Register(context.Background(), dto)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.errorMsg, err.Error())
			} else {
				assert.NoError(t, err)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}
