package notification_channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"vigi/internal/modules/monitor_notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository implements the Repository interface for testing
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, entity *Model) (*Model, error) {
	args := m.Called(ctx, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	args := m.Called(ctx, page, limit, q, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) UpdateFull(ctx context.Context, id string, entity *Model, orgID string) error {
	args := m.Called(ctx, id, entity, orgID)
	return args.Error(0)
}

func (m *MockRepository) UpdatePartial(ctx context.Context, id string, entity *UpdateModel, orgID string) error {
	args := m.Called(ctx, id, entity, orgID)
	return args.Error(0)
}

func (m *MockRepository) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

// MockMonitorNotificationService implements the monitor_notification.Service interface for testing
type MockMonitorNotificationService struct {
	mock.Mock
}

func (m *MockMonitorNotificationService) Create(ctx context.Context, monitorID string, notificationID string) (*monitor_notification.Model, error) {
	args := m.Called(ctx, monitorID, notificationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_notification.Model), args.Error(1)
}

func (m *MockMonitorNotificationService) FindByID(ctx context.Context, id string) (*monitor_notification.Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_notification.Model), args.Error(1)
}

func (m *MockMonitorNotificationService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorNotificationService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_notification.Model, error) {
	args := m.Called(ctx, monitorID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
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

// Helper function to create a test service
func createTestService(mockRepo *MockRepository, mockMonitorNotificationService *MockMonitorNotificationService) Service {
	logger, _ := zap.NewDevelopment()
	return NewService(mockRepo, mockMonitorNotificationService, logger.Sugar())
}

func TestServiceImpl_Create(t *testing.T) {
	tests := []struct {
		name          string
		input         *CreateUpdateDto
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful creation",
			input: &CreateUpdateDto{
				Name:      "Test Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    "config-string",
			},
			mockSetup: func(mr *MockRepository) {
				expectedModel := &Model{
					ID:        "test-id",
					Name:      "Test Channel",
					Type:      "email",
					Active:    true,
					IsDefault: false,
					Config:    stringPtr("config-string"),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				mr.On("Create", mock.Anything, mock.MatchedBy(func(model *Model) bool {
					return model.Name == "Test Channel" &&
						model.Type == "email" &&
						model.Active == true &&
						model.IsDefault == false &&
						model.OrgID == "org-1" &&
						*model.Config == "config-string"
				})).Return(expectedModel, nil)
			},
			expectedModel: &Model{
				ID:        "test-id",
				Name:      "Test Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    stringPtr("config-string"),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			expectedError: nil,
		},
		{
			name: "repository error",
			input: &CreateUpdateDto{
				Name:      "Test Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    "config-string",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("Create", mock.Anything, mock.Anything).Return(nil, errors.New("repository error"))
			},
			expectedModel: nil,
			expectedError: errors.New("repository error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.Create(ctx, tt.input, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedModel.Name, result.Name)
				assert.Equal(t, tt.expectedModel.Type, result.Type)
				assert.Equal(t, tt.expectedModel.Active, result.Active)
				assert.Equal(t, tt.expectedModel.IsDefault, result.IsDefault)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindByID(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful find",
			id:   "test-id",
			mockSetup: func(mr *MockRepository) {
				expectedModel := &Model{
					ID:        "test-id",
					Name:      "Test Channel",
					Type:      "email",
					Active:    true,
					IsDefault: false,
					Config:    stringPtr("config-string"),
				}
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(expectedModel, nil)
			},
			expectedModel: &Model{
				ID:        "test-id",
				Name:      "Test Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    stringPtr("config-string"),
			},
			expectedError: nil,
		},
		{
			name: "repository error",
			id:   "test-id",
			mockSetup: func(mr *MockRepository) {
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(nil, errors.New("not found"))
			},
			expectedModel: nil,
			expectedError: errors.New("not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.FindByID(ctx, tt.id, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModel, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindAll(t *testing.T) {
	tests := []struct {
		name           string
		page           int
		limit          int
		query          string
		mockSetup      func(*MockRepository)
		expectedModels []*Model
		expectedError  error
	}{
		{
			name:  "successful find all",
			page:  1,
			limit: 10,
			query: "test",
			mockSetup: func(mr *MockRepository) {
				expectedModels := []*Model{
					{
						ID:        "test-id-1",
						Name:      "Test Channel 1",
						Type:      "email",
						Active:    true,
						IsDefault: false,
						Config:    stringPtr("config-1"),
					},
					{
						ID:        "test-id-2",
						Name:      "Test Channel 2",
						Type:      "slack",
						Active:    true,
						IsDefault: true,
						Config:    stringPtr("config-2"),
					},
				}
				mr.On("FindAll", mock.Anything, 1, 10, "test", "org-1").Return(expectedModels, nil)
			},
			expectedModels: []*Model{
				{
					ID:        "test-id-1",
					Name:      "Test Channel 1",
					Type:      "email",
					Active:    true,
					IsDefault: false,
					Config:    stringPtr("config-1"),
				},
				{
					ID:        "test-id-2",
					Name:      "Test Channel 2",
					Type:      "slack",
					Active:    true,
					IsDefault: true,
					Config:    stringPtr("config-2"),
				},
			},
			expectedError: nil,
		},
		{
			name:  "repository error",
			page:  1,
			limit: 10,
			query: "test",
			mockSetup: func(mr *MockRepository) {
				mr.On("FindAll", mock.Anything, 1, 10, "test", "org-1").Return(nil, errors.New("repository error"))
			},
			expectedModels: nil,
			expectedError:  errors.New("repository error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.FindAll(ctx, tt.page, tt.limit, tt.query, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModels, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_UpdateFull(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		input         *CreateUpdateDto
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful update",
			id:   "test-id",
			input: &CreateUpdateDto{
				Name:      "Updated Channel",
				Type:      "webhook",
				Active:    false,
				IsDefault: true,
				Config:    "updated-config",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("UpdateFull", mock.Anything, "test-id", mock.MatchedBy(func(model *Model) bool {
					return model.ID == "test-id" &&
						model.Name == "Updated Channel" &&
						model.Type == "webhook" &&
						model.Active == false &&
						model.IsDefault == true &&
						*model.Config == "updated-config" &&
						model.OrgID == "org-1"
				}), "org-1").Return(nil)
			},
			expectedModel: &Model{
				ID:        "test-id",
				Name:      "Updated Channel",
				Type:      "webhook",
				Active:    false,
				IsDefault: true,
				Config:    stringPtr("updated-config"),
			},
			expectedError: nil,
		},
		{
			name: "repository error",
			id:   "test-id",
			input: &CreateUpdateDto{
				Name:      "Updated Channel",
				Type:      "webhook",
				Active:    false,
				IsDefault: true,
				Config:    "updated-config",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("UpdateFull", mock.Anything, "test-id", mock.Anything, "org-1").Return(errors.New("update failed"))
			},
			expectedModel: nil,
			expectedError: errors.New("update failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.UpdateFull(ctx, tt.id, tt.input, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModel.ID, result.ID)
				assert.Equal(t, tt.expectedModel.Name, result.Name)
				assert.Equal(t, tt.expectedModel.Type, result.Type)
				assert.Equal(t, tt.expectedModel.Active, result.Active)
				assert.Equal(t, tt.expectedModel.IsDefault, result.IsDefault)
				assert.Equal(t, *tt.expectedModel.Config, *result.Config)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_UpdatePartial(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		input         *PartialUpdateDto
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful partial update",
			id:   "test-id",
			input: &PartialUpdateDto{
				Name:      "Partially Updated Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    "partial-config",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("UpdatePartial", mock.Anything, "test-id", mock.MatchedBy(func(model *UpdateModel) bool {
					return *model.ID == "test-id" &&
						*model.Name == "Partially Updated Channel" &&
						*model.Type == "email" &&
						*model.Active == true &&
						*model.IsDefault == false &&
						*model.Config == "partial-config"
				}), "org-1").Return(nil)

				updatedModel := &Model{
					ID:        "test-id",
					Name:      "Partially Updated Channel",
					Type:      "email",
					Active:    true,
					IsDefault: false,
					Config:    stringPtr("partial-config"),
				}
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(updatedModel, nil)
			},
			expectedModel: &Model{
				ID:        "test-id",
				Name:      "Partially Updated Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    stringPtr("partial-config"),
			},
			expectedError: nil,
		},
		{
			name: "update error",
			id:   "test-id",
			input: &PartialUpdateDto{
				Name:      "Partially Updated Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    "partial-config",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("UpdatePartial", mock.Anything, "test-id", mock.Anything, "org-1").Return(errors.New("update failed"))
			},
			expectedModel: nil,
			expectedError: errors.New("update failed"),
		},
		{
			name: "find after update error",
			id:   "test-id",
			input: &PartialUpdateDto{
				Name:      "Partially Updated Channel",
				Type:      "email",
				Active:    true,
				IsDefault: false,
				Config:    "partial-config",
			},
			mockSetup: func(mr *MockRepository) {
				mr.On("UpdatePartial", mock.Anything, "test-id", mock.Anything, "org-1").Return(nil)
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(nil, errors.New("find failed"))
			},
			expectedModel: nil,
			expectedError: errors.New("find failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.UpdatePartial(ctx, tt.id, tt.input, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModel, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Delete(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		mockSetup     func(*MockRepository, *MockMonitorNotificationService)
		expectedError error
	}{
		{
			name: "successful delete with cascade",
			id:   "test-id",
			mockSetup: func(mr *MockRepository, mns *MockMonitorNotificationService) {
				mr.On("Delete", mock.Anything, "test-id", "org-1").Return(nil)
				mns.On("DeleteByNotificationID", mock.Anything, "test-id").Return(nil)
			},
			expectedError: nil,
		},
		{
			name: "repository delete error",
			id:   "test-id",
			mockSetup: func(mr *MockRepository, mns *MockMonitorNotificationService) {
				mr.On("Delete", mock.Anything, "test-id", "org-1").Return(errors.New("delete failed"))
			},
			expectedError: errors.New("delete failed"),
		},
		{
			name: "successful delete with cascade error (should not fail)",
			id:   "test-id",
			mockSetup: func(mr *MockRepository, mns *MockMonitorNotificationService) {
				mr.On("Delete", mock.Anything, "test-id", "org-1").Return(nil)
				mns.On("DeleteByNotificationID", mock.Anything, "test-id").Return(errors.New("cascade delete failed"))
			},
			expectedError: nil, // Service ignores cascade delete errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockMonitorNotificationService := &MockMonitorNotificationService{}
			service := createTestService(mockRepo, mockMonitorNotificationService)

			tt.mockSetup(mockRepo, mockMonitorNotificationService)

			ctx := context.Background()
			err := service.Delete(ctx, tt.id, "org-1")

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
			mockMonitorNotificationService.AssertExpectations(t)
		})
	}
}

func TestNewService(t *testing.T) {
	mockRepo := &MockRepository{}
	mockMonitorNotificationService := &MockMonitorNotificationService{}
	logger, _ := zap.NewDevelopment()

	service := NewService(mockRepo, mockMonitorNotificationService, logger.Sugar())

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	serviceImpl := service.(*ServiceImpl)
	assert.Equal(t, mockRepo, serviceImpl.repository)
	assert.Equal(t, mockMonitorNotificationService, serviceImpl.monitorNotificationService)
	assert.NotNil(t, serviceImpl.logger)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
