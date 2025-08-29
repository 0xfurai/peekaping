package incident

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

func (m *MockRepository) Create(ctx context.Context, incident *Model) (*Model, error) {
	args := m.Called(ctx, incident)
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

func (m *MockRepository) FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error) {
	args := m.Called(ctx, statusPageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) Update(ctx context.Context, id string, incident *UpdateModel) (*Model, error) {
	args := m.Called(ctx, id, incident)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// Helper function to create a test service
func createTestService() (*ServiceImpl, *MockRepository) {
	mockRepo := &MockRepository{}
	logger := zap.NewNop().Sugar()
	service := NewService(mockRepo, logger).(*ServiceImpl)
	return service, mockRepo
}

// Helper function to assert model equality without time fields
func assertModelEqual(t *testing.T, expected, actual *Model) {
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Title, actual.Title)
	assert.Equal(t, expected.Content, actual.Content)
	assert.Equal(t, expected.Style, actual.Style)
	assert.Equal(t, expected.Pin, actual.Pin)
	assert.Equal(t, expected.Active, actual.Active)
	assert.Equal(t, expected.StatusPageID, actual.StatusPageID)
	// Just verify that time fields are set, don't compare exact values
	assert.NotZero(t, actual.CreatedAt)
	assert.NotZero(t, actual.UpdatedAt)
}

// Helper function to assert model slice equality without time fields
func assertModelSliceEqual(t *testing.T, expected, actual []*Model) {
	assert.Len(t, actual, len(expected))
	for i, expectedModel := range expected {
		assertModelEqual(t, expectedModel, actual[i])
	}
}

// Helper function to create test models
func createTestIncident() *Model {
	statusPageID := "status-page-123"
	fixedTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	return &Model{
		ID:           "incident-123",
		Title:        "Test Incident",
		Content:      "This is a test incident",
		Style:        "warning",
		Pin:          true,
		Active:       true,
		StatusPageID: &statusPageID,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}
}

func createTestCreateDTO() *CreateIncidentDTO {
	statusPageID := "status-page-123"
	pin := false
	active := true
	return &CreateIncidentDTO{
		Title:        "Test Incident",
		Content:      "This is a test incident",
		Style:        "warning",
		Pin:          &pin,
		Active:       &active,
		StatusPageID: &statusPageID,
	}
}

func createTestUpdateDTO() *UpdateIncidentDTO {
	title := "Updated Incident"
	content := "This is an updated incident"
	style := "error"
	pin := true
	active := false
	statusPageID := "status-page-456"
	return &UpdateIncidentDTO{
		Title:        &title,
		Content:      &content,
		Style:        &style,
		Pin:          &pin,
		Active:       &active,
		StatusPageID: &statusPageID,
	}
}

func TestServiceImpl_Create(t *testing.T) {
	tests := []struct {
		name           string
		dto            *CreateIncidentDTO
		mockSetup      func(*MockRepository)
		expectedResult *Model
		expectedError  error
	}{
		{
			name: "successful creation with default values",
			dto: &CreateIncidentDTO{
				Title:   "Test Incident",
				Content: "Test content",
				Style:   "warning",
			},
			mockSetup: func(m *MockRepository) {
				expectedIncident := &Model{
					Title:   "Test Incident",
					Content: "Test content",
					Style:   "warning",
					Pin:     true,
					Active:  true,
				}
				returnedIncident := createTestIncident()
				m.On("Create", mock.Anything, mock.MatchedBy(func(incident *Model) bool {
					return incident.Title == expectedIncident.Title &&
						incident.Content == expectedIncident.Content &&
						incident.Style == expectedIncident.Style &&
						incident.Pin == expectedIncident.Pin &&
						incident.Active == expectedIncident.Active &&
						incident.StatusPageID == nil
				})).Return(returnedIncident, nil)
			},
			expectedResult: createTestIncident(),
			expectedError:  nil,
		},
		{
			name: "successful creation with custom values",
			dto:  createTestCreateDTO(),
			mockSetup: func(m *MockRepository) {
				statusPageID := "status-page-123"
				expectedIncident := &Model{
					Title:        "Test Incident",
					Content:      "This is a test incident",
					Style:        "warning",
					Pin:          false,
					Active:       true,
					StatusPageID: &statusPageID,
				}
				returnedIncident := createTestIncident()
				m.On("Create", mock.Anything, mock.MatchedBy(func(incident *Model) bool {
					return incident.Title == expectedIncident.Title &&
						incident.Content == expectedIncident.Content &&
						incident.Style == expectedIncident.Style &&
						incident.Pin == expectedIncident.Pin &&
						incident.Active == expectedIncident.Active &&
						incident.StatusPageID != nil &&
						*incident.StatusPageID == *expectedIncident.StatusPageID
				})).Return(returnedIncident, nil)
			},
			expectedResult: createTestIncident(),
			expectedError:  nil,
		},
		{
			name: "repository error",
			dto:  createTestCreateDTO(),
			mockSetup: func(m *MockRepository) {
				m.On("Create", mock.Anything, mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedResult: nil,
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			result, err := service.Create(context.Background(), tt.dto)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assertModelEqual(t, tt.expectedResult, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindByID(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		mockSetup      func(*MockRepository)
		expectedResult *Model
		expectedError  error
	}{
		{
			name: "successful find",
			id:   "incident-123",
			mockSetup: func(m *MockRepository) {
				incident := createTestIncident()
				m.On("FindByID", mock.Anything, "incident-123").Return(incident, nil)
			},
			expectedResult: createTestIncident(),
			expectedError:  nil,
		},
		{
			name: "repository error",
			id:   "incident-123",
			mockSetup: func(m *MockRepository) {
				m.On("FindByID", mock.Anything, "incident-123").Return(nil, errors.New("not found"))
			},
			expectedResult: nil,
			expectedError:  errors.New("not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			result, err := service.FindByID(context.Background(), tt.id)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assertModelEqual(t, tt.expectedResult, result)
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
		expectedResult []*Model
		expectedError  error
	}{
		{
			name:  "successful find all",
			page:  0,
			limit: 10,
			query: "",
			mockSetup: func(m *MockRepository) {
				incidents := []*Model{createTestIncident()}
				m.On("FindAll", mock.Anything, 0, 10, "").Return(incidents, nil)
			},
			expectedResult: []*Model{createTestIncident()},
			expectedError:  nil,
		},
		{
			name:  "successful find all with query",
			page:  1,
			limit: 5,
			query: "test",
			mockSetup: func(m *MockRepository) {
				incidents := []*Model{createTestIncident()}
				m.On("FindAll", mock.Anything, 1, 5, "test").Return(incidents, nil)
			},
			expectedResult: []*Model{createTestIncident()},
			expectedError:  nil,
		},
		{
			name:  "repository error",
			page:  0,
			limit: 10,
			query: "",
			mockSetup: func(m *MockRepository) {
				m.On("FindAll", mock.Anything, 0, 10, "").Return(nil, errors.New("database error"))
			},
			expectedResult: nil,
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			result, err := service.FindAll(context.Background(), tt.page, tt.limit, tt.query)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assertModelSliceEqual(t, tt.expectedResult, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_FindByStatusPageID(t *testing.T) {
	tests := []struct {
		name           string
		statusPageID   string
		mockSetup      func(*MockRepository)
		expectedResult []*Model
		expectedError  error
	}{
		{
			name:         "successful find by status page ID",
			statusPageID: "status-page-123",
			mockSetup: func(m *MockRepository) {
				incidents := []*Model{createTestIncident()}
				m.On("FindByStatusPageID", mock.Anything, "status-page-123").Return(incidents, nil)
			},
			expectedResult: []*Model{createTestIncident()},
			expectedError:  nil,
		},
		{
			name:         "repository error",
			statusPageID: "status-page-123",
			mockSetup: func(m *MockRepository) {
				m.On("FindByStatusPageID", mock.Anything, "status-page-123").Return(nil, errors.New("database error"))
			},
			expectedResult: nil,
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			result, err := service.FindByStatusPageID(context.Background(), tt.statusPageID)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assertModelSliceEqual(t, tt.expectedResult, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Update(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		dto            *UpdateIncidentDTO
		mockSetup      func(*MockRepository)
		expectedResult *Model
		expectedError  error
	}{
		{
			name: "successful update",
			id:   "incident-123",
			dto:  createTestUpdateDTO(),
			mockSetup: func(m *MockRepository) {
				title := "Updated Incident"
				content := "This is an updated incident"
				style := "error"
				pin := true
				active := false
				statusPageID := "status-page-456"
				expectedUpdateModel := &UpdateModel{
					Title:        &title,
					Content:      &content,
					Style:        &style,
					Pin:          &pin,
					Active:       &active,
					StatusPageID: &statusPageID,
				}
				updatedIncident := createTestIncident()
				m.On("Update", mock.Anything, "incident-123", mock.MatchedBy(func(update *UpdateModel) bool {
					return update.Title != nil && *update.Title == *expectedUpdateModel.Title &&
						update.Content != nil && *update.Content == *expectedUpdateModel.Content &&
						update.Style != nil && *update.Style == *expectedUpdateModel.Style &&
						update.Pin != nil && *update.Pin == *expectedUpdateModel.Pin &&
						update.Active != nil && *update.Active == *expectedUpdateModel.Active &&
						update.StatusPageID != nil && *update.StatusPageID == *expectedUpdateModel.StatusPageID
				})).Return(updatedIncident, nil)
			},
			expectedResult: createTestIncident(),
			expectedError:  nil,
		},
		{
			name: "successful partial update",
			id:   "incident-123",
			dto: &UpdateIncidentDTO{
				Title: func() *string { s := "New Title"; return &s }(),
			},
			mockSetup: func(m *MockRepository) {
				updatedIncident := createTestIncident()
				m.On("Update", mock.Anything, "incident-123", mock.MatchedBy(func(update *UpdateModel) bool {
					return update.Title != nil && *update.Title == "New Title" &&
						update.Content == nil &&
						update.Style == nil &&
						update.Pin == nil &&
						update.Active == nil &&
						update.StatusPageID == nil
				})).Return(updatedIncident, nil)
			},
			expectedResult: createTestIncident(),
			expectedError:  nil,
		},
		{
			name: "repository error",
			id:   "incident-123",
			dto:  createTestUpdateDTO(),
			mockSetup: func(m *MockRepository) {
				m.On("Update", mock.Anything, "incident-123", mock.Anything).Return(nil, errors.New("database error"))
			},
			expectedResult: nil,
			expectedError:  errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			result, err := service.Update(context.Background(), tt.id, tt.dto)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assertModelEqual(t, tt.expectedResult, result)
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

func TestServiceImpl_Delete(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		mockSetup     func(*MockRepository)
		expectedError error
	}{
		{
			name: "successful delete",
			id:   "incident-123",
			mockSetup: func(m *MockRepository) {
				m.On("Delete", mock.Anything, "incident-123").Return(nil)
			},
			expectedError: nil,
		},
		{
			name: "repository error",
			id:   "incident-123",
			mockSetup: func(m *MockRepository) {
				m.On("Delete", mock.Anything, "incident-123").Return(errors.New("database error"))
			},
			expectedError: errors.New("database error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockRepo := createTestService()
			tt.mockSetup(mockRepo)

			err := service.Delete(context.Background(), tt.id)

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

func TestNewService(t *testing.T) {
	mockRepo := &MockRepository{}
	logger := zap.NewNop().Sugar()

	service := NewService(mockRepo, logger)

	assert.NotNil(t, service)
	assert.IsType(t, &ServiceImpl{}, service)

	serviceImpl := service.(*ServiceImpl)
	assert.Equal(t, mockRepo, serviceImpl.repository)
	assert.NotNil(t, serviceImpl.logger)
}

// Benchmark tests
func BenchmarkServiceImpl_Create(b *testing.B) {
	service, mockRepo := createTestService()
	dto := createTestCreateDTO()
	incident := createTestIncident()

	mockRepo.On("Create", mock.Anything, mock.Anything).Return(incident, nil)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.Create(ctx, dto)
	}
}

func BenchmarkServiceImpl_FindByID(b *testing.B) {
	service, mockRepo := createTestService()
	incident := createTestIncident()

	mockRepo.On("FindByID", mock.Anything, mock.Anything).Return(incident, nil)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.FindByID(ctx, "incident-123")
	}
}
