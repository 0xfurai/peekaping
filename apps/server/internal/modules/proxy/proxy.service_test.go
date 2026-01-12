package proxy

import (
	"context"
	"errors"
	"testing"
	"time"
	"vigi/internal/infra"
	"vigi/internal/modules/events"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/shared"

	"github.com/redis/go-redis/v9"
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

func (m *MockRepository) UpdateFull(ctx context.Context, id string, entity *Model, orgID string) (*Model, error) {
	args := m.Called(ctx, id, entity, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) UpdatePartial(ctx context.Context, id string, entity *UpdateModel, orgID string) (*Model, error) {
	args := m.Called(ctx, id, entity, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
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

func (m *MockMonitorService) FindByID(ctx context.Context, id string, orgID string) (*shared.Monitor, error) {
	args := m.Called(ctx, id, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, ids, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) FindAll(ctx context.Context, page int, limit int, q string, active *bool, status *int, tagIds []string, orgID string) ([]*shared.Monitor, error) {
	args := m.Called(ctx, page, limit, q, active, status, tagIds, orgID)
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

func (m *MockMonitorService) UpdatePartial(ctx context.Context, id string, monitor *monitor.PartialUpdateDto, noPublish bool, orgID string) (*shared.Monitor, error) {
	args := m.Called(ctx, id, monitor, noPublish, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
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

func (m *MockMonitorService) FindOneByPushToken(ctx context.Context, pushToken string) (*shared.Monitor, error) {
	args := m.Called(ctx, pushToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.Monitor), args.Error(1)
}

func (m *MockMonitorService) ResetMonitorData(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
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
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	eventBus := infra.NewRedisEventBus(redisClient, logger)

	params := NewServiceParams{
		Repository:     mockRepo,
		MonitorService: mockMonitorService,
		EventBus:       eventBus,
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
	assert.Equal(t, eventBus, serviceImpl.eventBus)
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
			redisClient := redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			})
			eventBus := infra.NewRedisEventBus(redisClient, logger)

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
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
			redisClient := redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			})
			eventBus := infra.NewRedisEventBus(redisClient, logger)

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
				logger:         logger,
			}

			mockRepo.On("FindByID", mock.Anything, tt.id, "test-org").Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.FindByID(context.Background(), tt.id, "test-org")

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
			redisClient := redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			})
			eventBus := infra.NewRedisEventBus(redisClient, logger)

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
				logger:         logger,
			}

			mockRepo.On("FindAll", mock.Anything, tt.page, tt.limit, tt.query, "test-org").Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.FindAll(context.Background(), tt.page, tt.limit, tt.query, "test-org")

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
		name           string
		id             string
		entity         *CreateUpdateDto
		repoResponse   *Model
		repoError      error
		expectedError  bool
		expectEventBus bool
		eventBusNil    bool
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
			repoError:      nil,
			expectedError:  false,
			expectEventBus: true,
			eventBusNil:    false,
		},
		{
			name: "repository error",
			id:   "proxy1",
			entity: &CreateUpdateDto{
				Protocol: "http",
				Host:     "proxy.example.com",
				Port:     8080,
			},
			repoResponse:   nil,
			repoError:      errors.New("update failed"),
			expectedError:  true,
			expectEventBus: false,
			eventBusNil:    false,
		},
		{
			name: "successful update with nil event bus",
			id:   "proxy1",
			entity: &CreateUpdateDto{
				Protocol: "socks5",
				Host:     "socks-proxy.example.com",
				Port:     1080,
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "socks5",
				Host:     "socks-proxy.example.com",
				Port:     1080,
			},
			repoError:      nil,
			expectedError:  false,
			expectEventBus: false,
			eventBusNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			var eventBus events.EventBus
			if !tt.eventBusNil {
				redisClient := redis.NewClient(&redis.Options{
					Addr: "localhost:6379",
				})
				eventBus = infra.NewRedisEventBus(redisClient, logger)
			}

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
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
			}), "test-org").Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.UpdateFull(context.Background(), tt.id, tt.entity, "test-org")

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
		name           string
		id             string
		entity         *PartialUpdateDto
		repoResponse   *Model
		repoError      error
		expectedError  bool
		expectEventBus bool
		eventBusNil    bool
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
			repoError:      nil,
			expectedError:  false,
			expectEventBus: true,
			eventBusNil:    false,
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
			repoError:      nil,
			expectedError:  false,
			expectEventBus: true,
			eventBusNil:    false,
		},
		{
			name: "repository error",
			id:   "proxy1",
			entity: &PartialUpdateDto{
				Host: &host,
			},
			repoResponse:   nil,
			repoError:      errors.New("update failed"),
			expectedError:  true,
			expectEventBus: false,
			eventBusNil:    false,
		},
		{
			name: "successful update with nil event bus",
			id:   "proxy1",
			entity: &PartialUpdateDto{
				Protocol: &protocol,
			},
			repoResponse: &Model{
				ID:       "proxy1",
				Protocol: "https",
				Host:     "proxy.example.com",
				Port:     8080,
			},
			repoError:      nil,
			expectedError:  false,
			expectEventBus: false,
			eventBusNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			var eventBus events.EventBus
			if !tt.eventBusNil {
				redisClient := redis.NewClient(&redis.Options{
					Addr: "localhost:6379",
				})
				eventBus = infra.NewRedisEventBus(redisClient, logger)
			}

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
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
			}), "test-org").Return(tt.repoResponse, tt.repoError)

			// Execute
			result, err := service.UpdatePartial(context.Background(), tt.id, tt.entity, "test-org")

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
		expectEventBus           bool
		eventBusNil              bool
		expectMonitorServiceCall bool
	}{
		{
			name:                     "successful delete",
			id:                       "proxy1",
			monitorServiceError:      nil,
			repoError:                nil,
			expectedError:            false,
			expectEventBus:           true,
			eventBusNil:              false,
			expectMonitorServiceCall: true,
		},
		{
			name:                     "repository error",
			id:                       "proxy1",
			monitorServiceError:      nil,
			repoError:                errors.New("delete failed"),
			expectedError:            true,
			expectEventBus:           false,
			eventBusNil:              false,
			expectMonitorServiceCall: true,
		},
		{
			name:                     "monitor service error ignored",
			id:                       "proxy1",
			monitorServiceError:      errors.New("monitor service error"),
			repoError:                nil,
			expectedError:            false,
			expectEventBus:           true,
			eventBusNil:              false,
			expectMonitorServiceCall: true,
		},
		{
			name:                     "successful delete with nil event bus",
			id:                       "proxy1",
			monitorServiceError:      nil,
			repoError:                nil,
			expectedError:            false,
			expectEventBus:           false,
			eventBusNil:              true,
			expectMonitorServiceCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockRepository)
			mockMonitorService := new(MockMonitorService)
			logger := zap.NewNop().Sugar()
			var eventBus events.EventBus
			if !tt.eventBusNil {
				redisClient := redis.NewClient(&redis.Options{
					Addr: "localhost:6379",
				})
				eventBus = infra.NewRedisEventBus(redisClient, logger)
			}

			service := &ServiceImpl{
				repository:     mockRepo,
				monitorService: mockMonitorService,
				eventBus:       eventBus,
				logger:         logger,
			}

			if tt.expectMonitorServiceCall {
				mockMonitorService.On("RemoveProxyReference", mock.Anything, tt.id).Return(tt.monitorServiceError)
			}

			mockRepo.On("Delete", mock.Anything, tt.id, "test-org").Return(tt.repoError)

			// Execute
			err := service.Delete(context.Background(), tt.id, "test-org")

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
