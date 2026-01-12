package status_page

import (
	"context"
	"errors"
	"testing"

	"vigi/internal/modules/domain_status_page"
	"vigi/internal/modules/events"
	"vigi/internal/modules/monitor_status_page"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockRepository
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, statusPage *Model) (*Model, error) {
	args := m.Called(ctx, statusPage)
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

func (m *MockRepository) FindBySlug(ctx context.Context, slug string) (*Model, error) {
	args := m.Called(ctx, slug)
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

func (m *MockRepository) Update(ctx context.Context, id string, statusPage *UpdateModel, orgID string) error {
	args := m.Called(ctx, id, statusPage, orgID)
	return args.Error(0)
}

func (m *MockRepository) Delete(ctx context.Context, id string, orgID string) error {
	args := m.Called(ctx, id, orgID)
	return args.Error(0)
}

// MockEventBus
type MockEventBus struct {
	mock.Mock
}

func (m *MockEventBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	m.Called(eventType, handler)
}

func (m *MockEventBus) Publish(event events.Event) {
	m.Called(event)
}

func (m *MockEventBus) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockMonitorStatusPageService
type MockMonitorStatusPageService struct {
	mock.Mock
}

func (m *MockMonitorStatusPageService) Create(ctx context.Context, entity *monitor_status_page.CreateUpdateDto) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, entity)
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}
func (m *MockMonitorStatusPageService) FindByID(ctx context.Context, id string) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) FindAll(ctx context.Context, page int, limit int, q string) ([]*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) UpdateFull(ctx context.Context, id string, entity *monitor_status_page.CreateUpdateDto) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) UpdatePartial(ctx context.Context, id string, entity *monitor_status_page.PartialUpdateDto) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *MockMonitorStatusPageService) AddMonitorToStatusPage(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, monitorID, order, active)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*monitor_status_page.Model), args.Error(1)
}
func (m *MockMonitorStatusPageService) RemoveMonitorFromStatusPage(ctx context.Context, statusPageID, monitorID string) error {
	args := m.Called(ctx, statusPageID, monitorID)
	return args.Error(0)
}
func (m *MockMonitorStatusPageService) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error) {
	args := m.Called(ctx, statusPageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*monitor_status_page.Model), args.Error(1)
}
func (m *MockMonitorStatusPageService) GetStatusPagesForMonitor(ctx context.Context, monitorID string) ([]*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) FindByStatusPageAndMonitor(ctx context.Context, statusPageID, monitorID string) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*monitor_status_page.Model, error) {
	return nil, nil
}
func (m *MockMonitorStatusPageService) DeleteAllMonitorsForStatusPage(ctx context.Context, statusPageID string) error {
	args := m.Called(ctx, statusPageID)
	return args.Error(0)
}

// MockDomainStatusPageService
type MockDomainStatusPageService struct {
	mock.Mock
}

func (m *MockDomainStatusPageService) Create(ctx context.Context, entity *domain_status_page.CreateUpdateDto) (*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) FindByID(ctx context.Context, id string) (*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) FindAll(ctx context.Context, page int, limit int, q string) ([]*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) UpdateFull(ctx context.Context, id string, entity *domain_status_page.CreateUpdateDto) (*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) UpdatePartial(ctx context.Context, id string, entity *domain_status_page.PartialUpdateDto) (*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) Delete(ctx context.Context, id string) error {
	return nil
}
func (m *MockDomainStatusPageService) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*domain_status_page.Model, error) {
	args := m.Called(ctx, statusPageID, domain)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain_status_page.Model), args.Error(1)
}
func (m *MockDomainStatusPageService) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	args := m.Called(ctx, statusPageID, domain)
	return args.Error(0)
}
func (m *MockDomainStatusPageService) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*domain_status_page.Model, error) {
	args := m.Called(ctx, statusPageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain_status_page.Model), args.Error(1)
}
func (m *MockDomainStatusPageService) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*domain_status_page.Model, error) {
	return nil, nil
}
func (m *MockDomainStatusPageService) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	return nil
}
func (m *MockDomainStatusPageService) FindByDomain(ctx context.Context, domain string) (*domain_status_page.Model, error) {
	args := m.Called(ctx, domain)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain_status_page.Model), args.Error(1)
}

func createTestService(
	mockRepo *MockRepository,
	mockEventBus *MockEventBus,
	mockMonitorStatusPageService *MockMonitorStatusPageService,
	mockDomainStatusPageService *MockDomainStatusPageService,
) Service {
	logger, _ := zap.NewDevelopment()
	return NewService(mockRepo, mockEventBus, mockMonitorStatusPageService, mockDomainStatusPageService, logger.Sugar())
}

func TestServiceImpl_Create(t *testing.T) {
	tests := []struct {
		name          string
		input         *CreateStatusPageDTO
		orgID         string
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name: "successful creation with orgID",
			input: &CreateStatusPageDTO{
				Slug:  "test-slug",
				Title: "Test Page",
			},
			orgID: "org-1",
			mockSetup: func(mr *MockRepository) {
				expectedModel := &Model{
					ID:    "test-id",
					Slug:  "test-slug",
					Title: "Test Page",
					OrgID: "org-1",
				}
				mr.On("Create", mock.Anything, mock.MatchedBy(func(model *Model) bool {
					return model.Slug == "test-slug" &&
						model.Title == "Test Page" &&
						model.OrgID == "org-1"
				})).Return(expectedModel, nil)
			},
			expectedModel: &Model{
				ID:    "test-id",
				Slug:  "test-slug",
				Title: "Test Page",
				OrgID: "org-1",
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockEventBus := &MockEventBus{}
			mockMSP := &MockMonitorStatusPageService{}
			mockDSP := &MockDomainStatusPageService{}
			service := createTestService(mockRepo, mockEventBus, mockMSP, mockDSP)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.Create(ctx, tt.input, tt.orgID)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModel, result)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindByID(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		orgID         string
		mockSetup     func(*MockRepository)
		expectedModel *Model
		expectedError error
	}{
		{
			name:  "successful find with orgID",
			id:    "test-id",
			orgID: "org-1",
			mockSetup: func(mr *MockRepository) {
				expectedModel := &Model{
					ID:    "test-id",
					OrgID: "org-1",
				}
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(expectedModel, nil)
			},
			expectedModel: &Model{
				ID:    "test-id",
				OrgID: "org-1",
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockEventBus := &MockEventBus{}
			mockMSP := &MockMonitorStatusPageService{}
			mockDSP := &MockDomainStatusPageService{}
			service := createTestService(mockRepo, mockEventBus, mockMSP, mockDSP)

			tt.mockSetup(mockRepo)

			ctx := context.Background()
			result, err := service.FindByID(ctx, tt.id, tt.orgID)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedModel, result)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Update(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		input         *UpdateStatusPageDTO
		orgID         string
		mockSetup     func(*MockRepository)
		expectedError error
	}{
		{
			name: "successful update with orgID",
			id:   "test-id",
			input: &UpdateStatusPageDTO{
				Title: stringPtr("Updated Title"),
			},
			orgID: "org-1",
			mockSetup: func(mr *MockRepository) {
				mr.On("Update", mock.Anything, "test-id", mock.Anything, "org-1").Return(nil)
				mr.On("FindByID", mock.Anything, "test-id", "org-1").Return(&Model{ID: "test-id", Title: "Updated Title"}, nil)
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockEventBus := &MockEventBus{}
			mockMSP := &MockMonitorStatusPageService{}
			mockDSP := &MockDomainStatusPageService{}
			service := createTestService(mockRepo, mockEventBus, mockMSP, mockDSP)

			tt.mockSetup(mockRepo)

			// Mock nested service calls for Update
			mockMSP.On("GetMonitorsForStatusPage", mock.Anything, "test-id").Return([]*monitor_status_page.Model{}, nil).Maybe()
			mockDSP.On("GetDomainsForStatusPage", mock.Anything, "test-id").Return([]*domain_status_page.Model{}, nil).Maybe()

			ctx := context.Background()
			_, err := service.Update(ctx, tt.id, tt.input, tt.orgID)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Delete(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		orgID         string
		mockSetup     func(*MockRepository, *MockMonitorStatusPageService)
		expectedError error
	}{
		{
			name:  "successful delete with orgID",
			id:    "test-id",
			orgID: "org-1",
			mockSetup: func(mr *MockRepository, mm *MockMonitorStatusPageService) {
				mr.On("Delete", mock.Anything, "test-id", "org-1").Return(nil)
				mm.On("DeleteAllMonitorsForStatusPage", mock.Anything, "test-id").Return(nil)
			},
			expectedError: nil,
		},
		{
			name:  "repository error",
			id:    "test-id",
			orgID: "org-1",
			mockSetup: func(mr *MockRepository, mm *MockMonitorStatusPageService) {
				mr.On("Delete", mock.Anything, "test-id", "org-1").Return(errors.New("delete failed"))
			},
			expectedError: errors.New("delete failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockRepository{}
			mockEventBus := &MockEventBus{}
			mockMSP := &MockMonitorStatusPageService{}
			mockDSP := &MockDomainStatusPageService{}
			service := createTestService(mockRepo, mockEventBus, mockMSP, mockDSP)

			tt.mockSetup(mockRepo, mockMSP)

			ctx := context.Background()
			err := service.Delete(ctx, tt.id, tt.orgID)

			if tt.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			mockRepo.AssertExpectations(t)
			mockMSP.AssertExpectations(t)
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
