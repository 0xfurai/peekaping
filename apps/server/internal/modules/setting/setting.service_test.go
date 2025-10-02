package setting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository is a mock implementation of the Repository interface
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) GetByKey(ctx context.Context, key string) (*Model, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) SetByKey(ctx context.Context, key string, entity *CreateUpdateDto) (*Model, error) {
	args := m.Called(ctx, key, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) DeleteByKey(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func TestNewService(t *testing.T) {
	mockRepo := &MockRepository{}
	logger := zap.NewNop().Sugar()

	service := NewService(mockRepo, logger)

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	impl := service.(*ServiceImpl)
	assert.Equal(t, mockRepo, impl.repository)
	assert.NotNil(t, impl.logger)
}

func TestServiceImpl_GetByKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		mockReturn    *Model
		mockError     error
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful get",
			key:  "test_key",
			mockReturn: &Model{
				ID:        "1",
				Key:       "test_key",
				Value:     "test_value",
				Type:      "string",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			mockError: nil,
			expectedModel: &Model{
				ID:        "1",
				Key:       "test_key",
				Value:     "test_value",
				Type:      "string",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			expectedError: nil,
		},
		{
			name:          "not found",
			key:           "non_existent_key",
			mockReturn:    nil,
			mockError:     nil,
			expectedModel: nil,
			expectedError: nil,
		},
		{
			name:          "repository error",
			key:           "test_key",
			mockReturn:    nil,
			mockError:     errors.New("database error"),
			expectedModel: nil,
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			mockRepo.On("GetByKey", mock.Anything, tt.key).Return(tt.mockReturn, tt.mockError)

			result, err := service.GetByKey(context.Background(), tt.key)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			if tt.expectedModel != nil {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedModel.Key, result.Key)
				assert.Equal(t, tt.expectedModel.Value, result.Value)
				assert.Equal(t, tt.expectedModel.Type, result.Type)
			} else {
				assert.Nil(t, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_SetByKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		dto           *CreateUpdateDto
		mockReturn    *Model
		mockError     error
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful set",
			key:  "test_key",
			dto: &CreateUpdateDto{
				Value: "new_value",
				Type:  "string",
			},
			mockReturn: &Model{
				ID:        "1",
				Key:       "test_key",
				Value:     "new_value",
				Type:      "string",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			mockError: nil,
			expectedModel: &Model{
				ID:        "1",
				Key:       "test_key",
				Value:     "new_value",
				Type:      "string",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			expectedError: nil,
		},
		{
			name: "repository error",
			key:  "test_key",
			dto: &CreateUpdateDto{
				Value: "new_value",
				Type:  "string",
			},
			mockReturn:    nil,
			mockError:     errors.New("database error"),
			expectedModel: nil,
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			mockRepo.On("SetByKey", mock.Anything, tt.key, tt.dto).Return(tt.mockReturn, tt.mockError)

			result, err := service.SetByKey(context.Background(), tt.key, tt.dto)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			if tt.expectedModel != nil {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedModel.Key, result.Key)
				assert.Equal(t, tt.expectedModel.Value, result.Value)
				assert.Equal(t, tt.expectedModel.Type, result.Type)
			} else {
				assert.Nil(t, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_DeleteByKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		mockError     error
		expectedError error
	}{
		{
			name:          "successful delete",
			key:           "test_key",
			mockError:     nil,
			expectedError: nil,
		},
		{
			name:          "repository error",
			key:           "test_key",
			mockError:     errors.New("database error"),
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			mockRepo.On("DeleteByKey", mock.Anything, tt.key).Return(tt.mockError)

			err := service.DeleteByKey(context.Background(), tt.key)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_InitializeSettings(t *testing.T) {
	tests := []struct {
		name          string
		mockSetup     func(*MockRepository)
		expectedError error
	}{
		{
			name: "successful initialization - all settings missing",
			mockSetup: func(repo *MockRepository) {
				// ACCESS_TOKEN_EXPIRED_IN - not exists
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "15m" && dto.Type == "string"
				})).Return(&Model{Key: "ACCESS_TOKEN_EXPIRED_IN", Value: "15m", Type: "string"}, nil)

				// REFRESH_TOKEN_EXPIRED_IN - not exists
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "720h" && dto.Type == "string"
				})).Return(&Model{Key: "REFRESH_TOKEN_EXPIRED_IN", Value: "720h", Type: "string"}, nil)

				// ACCESS_TOKEN_SECRET_KEY - not exists
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return len(dto.Value) == 64 && dto.Type == "string" // 32 bytes = 64 hex chars
				})).Return(&Model{Key: "ACCESS_TOKEN_SECRET_KEY", Value: "test_secret", Type: "string"}, nil)

				// REFRESH_TOKEN_SECRET_KEY - not exists
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return len(dto.Value) == 64 && dto.Type == "string"
				})).Return(&Model{Key: "REFRESH_TOKEN_SECRET_KEY", Value: "test_secret", Type: "string"}, nil)

				// cert_expiry_notify_days - not exists
				repo.On("GetByKey", mock.Anything, "cert_expiry_notify_days").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "cert_expiry_notify_days", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "[7,14,21]" && dto.Type == "json"
				})).Return(&Model{Key: "cert_expiry_notify_days", Value: "[7,14,21]", Type: "json"}, nil)
			},
			expectedError: nil,
		},
		{
			name: "successful initialization - some settings exist",
			mockSetup: func(repo *MockRepository) {
				// ACCESS_TOKEN_EXPIRED_IN - exists
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(&Model{Key: "ACCESS_TOKEN_EXPIRED_IN", Value: "30m"}, nil)

				// REFRESH_TOKEN_EXPIRED_IN - not exists
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "720h" && dto.Type == "string"
				})).Return(&Model{Key: "REFRESH_TOKEN_EXPIRED_IN", Value: "720h", Type: "string"}, nil)

				// ACCESS_TOKEN_SECRET_KEY - exists but empty
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(&Model{Key: "ACCESS_TOKEN_SECRET_KEY", Value: ""}, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return len(dto.Value) == 64 && dto.Type == "string"
				})).Return(&Model{Key: "ACCESS_TOKEN_SECRET_KEY", Value: "test_secret", Type: "string"}, nil)

				// REFRESH_TOKEN_SECRET_KEY - exists
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY").Return(&Model{Key: "REFRESH_TOKEN_SECRET_KEY", Value: "existing_secret"}, nil)

				// cert_expiry_notify_days - not exists
				repo.On("GetByKey", mock.Anything, "cert_expiry_notify_days").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "cert_expiry_notify_days", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "[7,14,21]" && dto.Type == "json"
				})).Return(&Model{Key: "cert_expiry_notify_days", Value: "[7,14,21]", Type: "json"}, nil)
			},
			expectedError: nil,
		},
		{
			name: "error getting access token expiration",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize access token expiration: database error"),
		},
		{
			name: "error setting access token expiration",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize access token expiration: database error"),
		},
		{
			name: "error getting refresh token expiration",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize refresh token expiration: database error"),
		},
		{
			name: "error getting access token secret key",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize access token secret key: database error"),
		},
		{
			name: "error setting access token secret key",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize access token secret key: database error"),
		},
		{
			name: "error getting refresh token secret key",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize refresh token secret key: database error"),
		},
		{
			name: "error setting refresh token secret key",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_EXPIRED_IN", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "ACCESS_TOKEN_SECRET_KEY", mock.Anything).Return(&Model{}, nil)
				repo.On("GetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "REFRESH_TOKEN_SECRET_KEY", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("failed to initialize refresh token secret key: database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			tt.mockSetup(mockRepo)

			err := service.InitializeSettings(context.Background())

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_initializeDefaultSetting(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		defaultValue  string
		settingType   string
		mockSetup     func(*MockRepository)
		expectedError error
	}{
		{
			name:         "setting does not exist - creates new",
			key:          "TEST_KEY",
			defaultValue: "default_value",
			settingType:  "string",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "TEST_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "TEST_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return dto.Value == "default_value" && dto.Type == "string"
				})).Return(&Model{Key: "TEST_KEY", Value: "default_value", Type: "string"}, nil)
			},
			expectedError: nil,
		},
		{
			name:         "setting exists - does nothing",
			key:          "TEST_KEY",
			defaultValue: "default_value",
			settingType:  "string",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "TEST_KEY").Return(&Model{Key: "TEST_KEY", Value: "existing_value"}, nil)
			},
			expectedError: nil,
		},
		{
			name:         "error getting setting",
			key:          "TEST_KEY",
			defaultValue: "default_value",
			settingType:  "string",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "TEST_KEY").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("database error"),
		},
		{
			name:         "error setting new value",
			key:          "TEST_KEY",
			defaultValue: "default_value",
			settingType:  "string",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "TEST_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "TEST_KEY", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			tt.mockSetup(mockRepo)

			// Cast to implementation to access private method
			impl := service.(*ServiceImpl)
			err := impl.initializeDefaultSetting(context.Background(), tt.key, tt.defaultValue, tt.settingType)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_initializeSecretKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		mockSetup     func(*MockRepository)
		expectedError error
	}{
		{
			name: "secret key does not exist - generates new",
			key:  "SECRET_KEY",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "SECRET_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return len(dto.Value) == 64 && dto.Type == "string" // 32 bytes = 64 hex chars
				})).Return(&Model{Key: "SECRET_KEY", Value: "generated_secret", Type: "string"}, nil)
			},
			expectedError: nil,
		},
		{
			name: "secret key exists but empty - generates new",
			key:  "SECRET_KEY",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "SECRET_KEY").Return(&Model{Key: "SECRET_KEY", Value: ""}, nil)
				repo.On("SetByKey", mock.Anything, "SECRET_KEY", mock.MatchedBy(func(dto *CreateUpdateDto) bool {
					return len(dto.Value) == 64 && dto.Type == "string"
				})).Return(&Model{Key: "SECRET_KEY", Value: "generated_secret", Type: "string"}, nil)
			},
			expectedError: nil,
		},
		{
			name: "secret key exists and not empty - does nothing",
			key:  "SECRET_KEY",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "SECRET_KEY").Return(&Model{Key: "SECRET_KEY", Value: "existing_secret"}, nil)
			},
			expectedError: nil,
		},
		{
			name: "error getting secret key",
			key:  "SECRET_KEY",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "SECRET_KEY").Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("database error"),
		},
		{
			name: "error setting secret key",
			key:  "SECRET_KEY",
			mockSetup: func(repo *MockRepository) {
				repo.On("GetByKey", mock.Anything, "SECRET_KEY").Return(nil, nil)
				repo.On("SetByKey", mock.Anything, "SECRET_KEY", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			logger := zap.NewNop().Sugar()
			service := NewService(mockRepo, logger)

			tt.mockSetup(mockRepo)

			// Cast to implementation to access private method
			impl := service.(*ServiceImpl)
			err := impl.initializeSecretKey(context.Background(), tt.key)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_generateSecretKey(t *testing.T) {
	service := &ServiceImpl{}

	// Test multiple generations to ensure randomness
	secret1, err1 := service.generateSecretKey()
	assert.NoError(t, err1)
	assert.Len(t, secret1, 64) // 32 bytes = 64 hex characters

	secret2, err2 := service.generateSecretKey()
	assert.NoError(t, err2)
	assert.Len(t, secret2, 64)

	// Ensure secrets are different (cryptographically random)
	assert.NotEqual(t, secret1, secret2)

	// Verify hex format
	for _, char := range secret1 {
		assert.Contains(t, "0123456789abcdef", string(char))
	}
}
