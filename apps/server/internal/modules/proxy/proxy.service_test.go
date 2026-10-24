package proxy

import (
	"context"
	"errors"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository implements Repository interface for testing
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

func (m *MockRepository) FindByID(ctx context.Context, id string) (*Model, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	args := m.Called(ctx, page, limit, q)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error) {
	args := m.Called(ctx, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error) {
	args := m.Called(ctx, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// MockMonitorService implements monitor.Service interface for testing
type MockMonitorService struct {
	mock.Mock
}

func (m *MockMonitorService) Create(ctx context.Context, monitor *monitor.CreateUpdateDto) (*shared.Monitor, error) {
	args := m.Called(ctx, monitor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByID(ctx context.Context, id string) (*shared.Monitor, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByIDs(ctx context.Context, ids []string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindActive(ctx context.Context) ([]*shared.Monitor, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) UpdateFull(ctx context.Context, id string, monitor *monitor.CreateUpdateDto) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) UpdatePartial(ctx context.Context, id string, monitor *monitor.PartialUpdateDto, noPublish bool) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor, noPublish)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorService) ValidateMonitorConfig(monitorType string, configJSON string) error {
	args := m.Called(monitorType, configJSON)
	return args.Error(0)
}

func (m *MockMonitorService) GetHeartbeats(ctx context.Context, id, probeID string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	args := m.Called(ctx, id, limit, page, important, reverse)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*heartbeat.Model), args.Error(1)
}

func (m *MockMonitorService) RemoveProxyReference(ctx context.Context, proxyID string) error {
	args := m.Called(ctx, proxyID)
	return args.Error(0)
}

func (m *MockMonitorService) FindByProxyId(ctx context.Context, proxyId string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, proxyId)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) GetStatPoints(ctx context.Context, id, probeID string, since, until time.Time, granularity string) (*monitor.StatPointsSummaryDto, error) {
	args := m.Called(ctx, id, since, until, granularity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.StatPointsSummaryDto), args.Error(1)
}

func (m *MockMonitorService) GetUptimeStats(ctx context.Context, id, probeID string) (*monitor.CustomUptimeStatsDto, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor.CustomUptimeStatsDto), args.Error(1)
}

func (m *MockMonitorService) FindOneByPushToken(ctx context.Context, pushToken string) (*shared.Monitor, error) {
	args := m.Called(ctx, pushToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) GetUptime24h(ctx context.Context, id, probeID string) (float64, error) {
	args := m.Called(ctx, id, probeID)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockMonitorService) ResetMonitorData(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorService) RepairStoredStatuses(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockMonitorService) FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*shared.Monitor, error) {
	args := m.Called(ctx, probeID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func TestNewService(t *testing.T) {
	// Setup
	mockRepo := new(MockRepository)
	mockMonitorService := new(MockMonitorService)
	logger := zap.NewNop().Sugar()

	params := NewServiceParams{
		Repository:     mockRepo,
		MonitorService: mockMonitorService,
		Logger:         logger,
	}

	// Execute
	service := NewService(params)

	// Assert
	assert.NotNil(t, service)
	serviceImpl, ok := service.(*ServiceImpl)
	assert.True(t, ok)
	assert.Equal(t, mockRepo, serviceImpl.repository)
	assert.Equal(t, mockMonitorService, serviceImpl.monitorService)
	assert.NotNil(t, serviceImpl.logger)
}

func TestServiceImpl_Create(t *testing.T) {
	tests := []struct {
		name          string
		entity        *CreateUpdateDto
		repoResponse  *Model
		repoError     error
		expectedError bool
	}{
		{
			name: "successful creation",
			entity: &CreateUpdateDto{
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
				Auth:     true,
				Username: "user",
				Password: "pass",
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
				Auth:     true,
				Username: "user",
				Password: "pass",
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name: "creation without auth",
			entity: &CreateUpdateDto{
				Protocol: "https",
				Host:     "secure-proxy.example.com",
				Port:     443,
				Auth:     false,
			},
			repoResponse: &Model{
				ID:       "proxy2",
				Protocol: "https",
				Host:     "secure-proxy.example.com",
				Port:     443,
				Auth:     false,
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name: "repository error",
			entity: &CreateUpdateDto{
				Protocol: "socks5",
				Host:     "socks-proxy.example.com",
				Port:     1080,
				Auth:     false,
			},
			repoResponse:  nil,
			repoError:     errors.New("database error"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			expectedModel := &Model{
				Protocol: tt.entity.Protocol,
				Host:     tt.entity.Host,
				Port:     tt.entity.Port,
				Auth:     tt.entity.Auth,
				Username: tt.entity.Username,
				Password: tt.entity.Password,
			}

			mockRepo.On("Create", mock.Anything, mock.MatchedBy(func(model *Model) bool {
				return model.Protocol == expectedModel.Protocol &&
					model.Host == expectedModel.Host &&
					model.Port == expectedModel.Port &&
					model.Auth == expectedModel.Auth &&
					model.Username == expectedModel.Username &&
					model.Password == expectedModel.Password
			})).Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.Create(context.Background(), tt.entity)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.repoResponse, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindByID(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		repoResponse  *Model
		repoError     error
		expectedError bool
	}{
		{
			name: "successful find",
			id:   "proxy1",
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name:          "proxy not found",
			id:            "nonexistent",
			repoResponse:  nil,
			repoError:     errors.New("not found"),
			expectedError: true,
		},
		{
			name:          "empty id",
			id:            "",
			repoResponse:  nil,
			repoError:     errors.New("invalid id"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			mockRepo.On("FindByID", mock.Anything, tt.id).Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.FindByID(context.Background(), tt.id)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.repoResponse, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindAll(t *testing.T) {
	tests := []struct {
		name          string
		page          int
		limit         int
		query         string
		repoResponse  []*Model
		repoError     error
		expectedError bool
	}{
		{
			name:  "successful find all",
			page:  1,
			limit: 10,
			query: "",
			repoResponse: []*Model{
				{
					ID:       "proxy1",
					Protocol: "http",
					Host:     "proxy1.example.com",
					Port:     8080,
				},
				{
					ID:       "proxy2",
					Protocol: "https",
					Host:     "proxy2.example.com",
					Port:     443,
				},
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name:  "find with search query",
			page:  1,
			limit: 5,
			query: "example",
			repoResponse: []*Model{
				{
					ID:       "proxy1",
					Protocol: "http",
					Host:     "proxy1.example.com",
					Port:     8080,
				},
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name:          "repository error",
			page:          1,
			limit:         10,
			query:         "",
			repoResponse:  nil,
			repoError:     errors.New("database error"),
			expectedError: true,
		},
		{
			name:          "empty result",
			page:          1,
			limit:         10,
			query:         "nonexistent",
			repoResponse:  []*Model{},
			repoError:     nil,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			mockRepo.On("FindAll", mock.Anything, tt.page, tt.limit, tt.query).Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.FindAll(context.Background(), tt.page, tt.limit, tt.query)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.repoResponse, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_UpdateFull(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		entity        *CreateUpdateDto
		repoResponse  *Model
		repoError     error
		expectedError bool
	}{
		{
			name: "successful full update",
			id:   "proxy1",
			entity: &CreateUpdateDto{
				Protocol: "https",
				Host:     "updated-proxy.example.com",
				Port:     443,
				Auth:     true,
				Username: "newuser",
				Password: "newpass",
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "https",
				Host:     "updated-proxy.example.com",
				Port:     443,
				Auth:     true,
				Username: "newuser",
				Password: "newpass",
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name: "repository error",
			id:   "proxy1",
			entity: &CreateUpdateDto{
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
			},
			repoResponse:  nil,
			repoError:     errors.New("update failed"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			expectedModel := &Model{
				Protocol: tt.entity.Protocol,
				Host:     tt.entity.Host,
				Port:     tt.entity.Port,
				Auth:     tt.entity.Auth,
				Username: tt.entity.Username,
				Password: tt.entity.Password,
			}

			mockRepo.On("UpdateFull", mock.Anything, tt.id, mock.MatchedBy(func(model *Model) bool {
				return model.Protocol == expectedModel.Protocol &&
					model.Host == expectedModel.Host &&
					model.Port == expectedModel.Port &&
					model.Auth == expectedModel.Auth &&
					model.Username == expectedModel.Username &&
					model.Password == expectedModel.Password
			})).Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.UpdateFull(context.Background(), tt.id, tt.entity)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.repoResponse, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_UpdatePartial(t *testing.T) {
	protocol := "https"
	host := "updated-host.com"
	port := 443
	auth := true
	username := "newuser"
	password := "newpass"

	tests := []struct {
		name          string
		id            string
		entity        *PartialUpdateDto
		repoResponse  *Model
		repoError     error
		expectedError bool
	}{
		{
			name: "successful partial update",
			id:   "proxy1",
			entity: &PartialUpdateDto{
				Protocol: &protocol,
				Host:     &host,
				Port:     &port,
				Auth:     &auth,
				Username: &username,
				Password: &password,
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "https",
				Host:     "updated-host.com",
				Port:     443,
				Auth:     true,
				Username: "newuser",
				Password: "newpass",
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name: "partial update with only host",
			id:   "proxy1",
			entity: &PartialUpdateDto{
				Host: &host,
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "http",
				Host:     "updated-host.com",
				Port:     8080,
			},
			repoError:     nil,
			expectedError: false,
		},
		{
			name: "repository error",
			id:   "proxy1",
			entity: &PartialUpdateDto{
				Host: &host,
			},
			repoResponse:  nil,
			repoError:     errors.New("update failed"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			expectedUpdateModel := &UpdateModel{
				Protocol: tt.entity.Protocol,
				Host:     tt.entity.Host,
				Port:     tt.entity.Port,
				Auth:     tt.entity.Auth,
				Username: tt.entity.Username,
				Password: tt.entity.Password,
			}

			mockRepo.On("UpdatePartial", mock.Anything, tt.id, mock.MatchedBy(func(model *UpdateModel) bool {
				return ((model.Protocol == nil && expectedUpdateModel.Protocol == nil) ||
					(model.Protocol != nil && expectedUpdateModel.Protocol != nil && *model.Protocol == *expectedUpdateModel.Protocol)) &&
					((model.Host == nil && expectedUpdateModel.Host == nil) ||
						(model.Host != nil && expectedUpdateModel.Host != nil && *model.Host == *expectedUpdateModel.Host)) &&
					((model.Port == nil && expectedUpdateModel.Port == nil) ||
						(model.Port != nil && expectedUpdateModel.Port != nil && *model.Port == *expectedUpdateModel.Port)) &&
					((model.Auth == nil && expectedUpdateModel.Auth == nil) ||
						(model.Auth != nil && expectedUpdateModel.Auth != nil && *model.Auth == *expectedUpdateModel.Auth)) &&
					((model.Username == nil && expectedUpdateModel.Username == nil) ||
						(model.Username != nil && expectedUpdateModel.Username != nil && *model.Username == *expectedUpdateModel.Username)) &&
					((model.Password == nil && expectedUpdateModel.Password == nil) ||
						(model.Password != nil && expectedUpdateModel.Password != nil && *model.Password == *expectedUpdateModel.Password))
			})).Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.UpdatePartial(context.Background(), tt.id, tt.entity)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.repoResponse, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Delete(t *testing.T) {
	tests := []struct {
		name                     string
		id                       string
		monitorServiceError      error
		repoError                error
		expectedError            bool
		expectMonitorServiceCall bool
	}{
		{
			name:                     "successful delete",
			id:                       "proxy1",
			monitorServiceError:      nil,
			repoError:                nil,
			expectedError:            false,
			expectMonitorServiceCall: true,
		},
		{
			name:                     "repository error",
			id:                       "proxy1",
			monitorServiceError:      nil,
			repoError:                errors.New("delete failed"),
			expectedError:            true,
			expectMonitorServiceCall: true,
		},
		{
			name:                     "monitor service error ignored",
			id:                       "proxy1",
			monitorServiceError:      errors.New("monitor service error"),
			repoError:                nil,
			expectedError:            false,
			expectMonitorServiceCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				logger:         logger,
			}

			if tt.expectMonitorServiceCall {
				mockMonitorService.On("RemoveProxyReference", mock.Anything, tt.id).Return(tt.monitorServiceError)
			}

			mockRepo.On("Delete", mock.Anything, tt.id).Return(tt.repoError)

			// Execute
			err := service.Delete(context.Background(), tt.id)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockRepo.AssertExpectations(t)
			mockMonitorService.AssertExpectations(t)
		})
	}
}
