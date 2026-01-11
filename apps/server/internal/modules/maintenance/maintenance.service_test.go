package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"

	"vigi/internal/modules/maintenance/utils"
	"vigi/internal/modules/monitor_maintenance"
)

// Mock dependencies
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
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

func (m *MockRepository) FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*Model, error) {
	args := m.Called(ctx, page, limit, q, strategy, orgID)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
	args := m.Called(ctx, id, entity, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
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

func (m *MockRepository) SetActive(ctx context.Context, id string, active bool, orgID string) (*Model, error) {
	args := m.Called(ctx, id, active, orgID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).([]*Model), args.Error(1)
}

type MockMonitorMaintenanceService struct {
	mock.Mock
}

func (m *MockMonitorMaintenanceService) Create(ctx context.Context, monitorID string, maintenanceID string) (*monitor_maintenance.Model, error) {
	args := m.Called(ctx, monitorID, maintenanceID)
	return args.Get(0).(*monitor_maintenance.Model), args.Error(1)
}

func (m *MockMonitorMaintenanceService) FindByID(ctx context.Context, id string) (*monitor_maintenance.Model, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*monitor_maintenance.Model), args.Error(1)
}

func (m *MockMonitorMaintenanceService) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMonitorMaintenanceService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_maintenance.Model, error) {
	args := m.Called(ctx, monitorID)
	return args.Get(0).([]*monitor_maintenance.Model), args.Error(1)
}

func (m *MockMonitorMaintenanceService) FindByMaintenanceID(ctx context.Context, maintenanceID string) ([]*monitor_maintenance.Model, error) {
	args := m.Called(ctx, maintenanceID)
	return args.Get(0).([]*monitor_maintenance.Model), args.Error(1)
}

func (m *MockMonitorMaintenanceService) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	args := m.Called(ctx, monitorID)
	return args.Error(0)
}

func (m *MockMonitorMaintenanceService) DeleteByMaintenanceID(ctx context.Context, maintenanceID string) error {
	args := m.Called(ctx, maintenanceID)
	return args.Error(0)
}

func (m *MockMonitorMaintenanceService) SetMonitors(ctx context.Context, maintenanceID string, monitorIDs []string) error {
	args := m.Called(ctx, maintenanceID, monitorIDs)
	return args.Error(0)
}

func (m *MockMonitorMaintenanceService) GetMonitors(ctx context.Context, maintenanceID string) ([]string, error) {
	args := m.Called(ctx, maintenanceID)
	return args.Get(0).([]string), args.Error(1)
}

type MockCronGenerator struct {
	mock.Mock
}

func (m *MockCronGenerator) GenerateCronExpression(strategy string, params *utils.CronParams) (*string, error) {
	args := m.Called(strategy, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*string), args.Error(1)
}

type MockTimeWindowChecker struct {
	mock.Mock
}

func (m *MockTimeWindowChecker) IsInDateTimePeriod(params *utils.TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	args := m.Called(params, now, loc)
	return args.Bool(0), args.Error(1)
}

func (m *MockTimeWindowChecker) IsInRecurringIntervalWindow(params *utils.TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	args := m.Called(params, now, loc)
	return args.Bool(0), args.Error(1)
}

func (m *MockTimeWindowChecker) IsInCronMaintenanceWindow(params *utils.TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	args := m.Called(params, now, loc)
	return args.Bool(0), args.Error(1)
}

type MockTimeUtils struct {
	mock.Mock
}

func (m *MockTimeUtils) CalculateDurationFromTimes(startTime, endTime string) (int, error) {
	args := m.Called(startTime, endTime)
	return args.Int(0), args.Error(1)
}

func (m *MockTimeUtils) GetDefaultTimezone() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTimeUtils) LoadTimezone(timezone string) *time.Location {
	args := m.Called(timezone)
	return args.Get(0).(*time.Location)
}

func (m *MockTimeUtils) ValidateTimeFormat(timeStr string) error {
	args := m.Called(timeStr)
	return args.Error(0)
}

func (m *MockTimeUtils) ParseTimeString(timeStr string) (time.Time, error) {
	args := m.Called(timeStr)
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *MockTimeUtils) IsCrossDayWindow(startTime, endTime string) (bool, error) {
	args := m.Called(startTime, endTime)
	return args.Bool(0), args.Error(1)
}

type MockValidator struct {
	mock.Mock
}

func (m *MockValidator) ValidateCronAndDuration(params *utils.ValidationParams) error {
	args := m.Called(params)
	return args.Error(0)
}

// Helper functions for creating test data
func createTestService() (*ServiceImpl, *MockRepository, *MockMonitorMaintenanceService, *MockCronGenerator, *MockTimeWindowChecker, *MockTimeUtils, *MockValidator) {
	mockRepo := &MockRepository{}
	mockMonitorMaintenanceService := &MockMonitorMaintenanceService{}
	mockCronGenerator := &MockCronGenerator{}
	mockTimeWindowChecker := &MockTimeWindowChecker{}
	mockTimeUtils := &MockTimeUtils{}
	mockValidator := &MockValidator{}

	logger := zap.NewNop().Sugar()

	service := &ServiceImpl{
		repository:                mockRepo,
		monitorMaintenanceService: mockMonitorMaintenanceService,
		logger:                    logger,
		cronGenerator:             mockCronGenerator,
		timeWindowChecker:         mockTimeWindowChecker,
		timeUtils:                 mockTimeUtils,
		validator:                 mockValidator,
	}

	return service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, mockTimeWindowChecker, mockTimeUtils, mockValidator
}

func createTestCreateUpdateDto() *CreateUpdateDto {
	startTime := "09:00"
	endTime := "17:00"
	duration := 480
	timezone := "UTC"

	return &CreateUpdateDto{
		Title:       "Test Maintenance",
		Description: "Test Description",
		Active:      true,
		Strategy:    "single",
		StartTime:   &startTime,
		EndTime:     &endTime,
		Duration:    &duration,
		Timezone:    &timezone,
		MonitorIds:  []string{"monitor1", "monitor2"},
	}
}

func createTestModel() *Model {
	startTime := "09:00"
	endTime := "17:00"
	duration := 480
	timezone := "UTC"

	return &Model{
		ID:          "test-id",
		Title:       "Test Maintenance",
		Description: "Test Description",
		Active:      true,
		Strategy:    "single",
		StartTime:   &startTime,
		EndTime:     &endTime,
		Duration:    &duration,
		Timezone:    &timezone,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// Test Create method
func TestServiceImpl_Create_Success(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, dto).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, dto.MonitorIds).Return(nil)

	result, err := service.Create(context.Background(), dto)

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
	mockMonitorMaintenanceService.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_ValidationError(t *testing.T) {
	service, _, _, _, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	expectedError := errors.New("validation error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(expectedError)

	result, err := service.Create(context.Background(), dto)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockValidator.AssertExpectations(t)
}

func TestServiceImpl_Create_WithCronGeneration(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	dto.Strategy = "recurring-weekday"
	dto.Cron = nil // Will trigger cron generation
	dto.Weekdays = []int{1, 2, 3, 4, 5}

	expectedModel := createTestModel()
	generatedCron := "0 9 * * 1-5"

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(&generatedCron, nil)
	mockRepo.On("Create", mock.Anything, mock.MatchedBy(func(d *CreateUpdateDto) bool {
		return d.Cron != nil && *d.Cron == generatedCron
	})).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, dto.MonitorIds).Return(nil)

	result, err := service.Create(context.Background(), dto)

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_WithDurationCalculation(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, mockTimeUtils, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	dto.Duration = nil // Will trigger duration calculation
	expectedDuration := 480

	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockTimeUtils.On("CalculateDurationFromTimes", *dto.StartTime, *dto.EndTime).Return(expectedDuration, nil)
	mockRepo.On("Create", mock.Anything, mock.MatchedBy(func(d *CreateUpdateDto) bool {
		return d.Duration != nil && *d.Duration == expectedDuration
	})).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, dto.MonitorIds).Return(nil)

	result, err := service.Create(context.Background(), dto)

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockTimeUtils.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

// Test FindByID method
func TestServiceImpl_FindByID_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModel := createTestModel()

	mockRepo.On("FindByID", mock.Anything, "test-id", mock.Anything).Return(expectedModel, nil)

	result, err := service.FindByID(context.Background(), "test-id", "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
}

func TestServiceImpl_FindByID_NotFound(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedError := errors.New("not found")

	mockRepo.On("FindByID", mock.Anything, "nonexistent-id", mock.Anything).Return(nil, expectedError)

	result, err := service.FindByID(context.Background(), "nonexistent-id", "test-org")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockRepo.AssertExpectations(t)
}

// Test FindAll method
func TestServiceImpl_FindAll_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModels := []*Model{createTestModel()}

	mockRepo.On("FindAll", mock.Anything, 1, 10, "test", "single", "test-org").Return(expectedModels, nil)

	result, err := service.FindAll(context.Background(), 1, 10, "test", "single", "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModels, result)
	mockRepo.AssertExpectations(t)
}

// Test UpdateFull method
func TestServiceImpl_UpdateFull_Success(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("UpdateFull", mock.Anything, "test-id", dto, "test-org").Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, "test-id", dto.MonitorIds).Return(nil)

	result, err := service.UpdateFull(context.Background(), "test-id", dto, "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
	mockMonitorMaintenanceService.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

// Test UpdatePartial method
func TestServiceImpl_UpdatePartial_Success(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, _, _, mockTimeUtils, mockValidator := createTestService()

	title := "Updated Title"
	active := false
	dto := &PartialUpdateDto{
		Title:      &title,
		Active:     &active,
		MonitorIds: []string{"monitor1"},
	}

	currentModel := createTestModel()
	expectedModel := createTestModel()
	expectedModel.Title = title
	expectedModel.Active = active

	// UpdatePartial calls FindByID to get current maintenance for duration calculation
	mockRepo.On("FindByID", mock.Anything, "test-id", "test-org").Return(currentModel, nil)
	// UpdatePartial also calls CalculateDurationFromTimes since start and end times are available
	mockTimeUtils.On("CalculateDurationFromTimes", *currentModel.StartTime, *currentModel.EndTime).Return(480, nil)
	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockRepo.On("UpdatePartial", mock.Anything, "test-id", mock.AnythingOfType("*maintenance.PartialUpdateDto"), "test-org").Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, "test-id", dto.MonitorIds).Return(nil)

	result, err := service.UpdatePartial(context.Background(), "test-id", dto, "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
	mockMonitorMaintenanceService.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockTimeUtils.AssertExpectations(t)
}

func TestServiceImpl_UpdatePartial_WithStrategyChange(t *testing.T) {
	service, mockRepo, _, mockCronGenerator, _, mockTimeUtils, mockValidator := createTestService()

	newStrategy := "recurring-weekday"
	dto := &PartialUpdateDto{
		Strategy: &newStrategy,
	}

	currentModel := createTestModel()
	expectedModel := createTestModel()
	expectedModel.Strategy = newStrategy

	generatedCron := "0 9 * * 1-5"

	mockRepo.On("FindByID", mock.Anything, "test-id", "test-org").Return(currentModel, nil)
	mockCronGenerator.On("GenerateCronExpression", newStrategy, mock.AnythingOfType("*utils.CronParams")).Return(&generatedCron, nil)
	// UpdatePartial also calls CalculateDurationFromTimes since start and end times are available
	mockTimeUtils.On("CalculateDurationFromTimes", *currentModel.StartTime, *currentModel.EndTime).Return(480, nil)
	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockRepo.On("UpdatePartial", mock.Anything, "test-id", mock.MatchedBy(func(d *PartialUpdateDto) bool {
		return d.Cron != nil && *d.Cron == generatedCron
	}), "test-org").Return(expectedModel, nil)

	result, err := service.UpdatePartial(context.Background(), "test-id", dto, "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockCronGenerator.AssertExpectations(t)
	mockTimeUtils.AssertExpectations(t)
}

// Test Delete method
func TestServiceImpl_Delete_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	mockRepo.On("Delete", mock.Anything, "test-id", "test-org").Return(nil)

	err := service.Delete(context.Background(), "test-id", "test-org")

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// Test SetActive method
func TestServiceImpl_SetActive_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModel := createTestModel()
	expectedModel.Active = false

	mockRepo.On("SetActive", mock.Anything, "test-id", false, "test-org").Return(expectedModel, nil)

	result, err := service.SetActive(context.Background(), "test-id", false, "test-org")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
}

// Test IsUnderMaintenance method
func TestServiceImpl_IsUnderMaintenance_ManualStrategy(t *testing.T) {
	service, _, _, _, _, _, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "manual"
	maintenance.Active = true

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.True(t, result)
}

func TestServiceImpl_IsUnderMaintenance_InactiveManual(t *testing.T) {
	service, _, _, _, _, _, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "manual"
	maintenance.Active = false

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.False(t, result)
}

func TestServiceImpl_IsUnderMaintenance_SingleStrategy(t *testing.T) {
	service, _, _, _, _, mockTimeUtils, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "single"
	maintenance.Active = true

	startDateTime := "2024-01-01T09:00"
	endDateTime := "2024-01-01T17:00"
	maintenance.StartDateTime = &startDateTime
	maintenance.EndDateTime = &endDateTime

	loc := time.UTC
	mockTimeUtils.On("GetDefaultTimezone").Return("UTC")
	mockTimeUtils.On("LoadTimezone", "UTC").Return(loc)

	// Create a new mock time window checker for this test
	mockTimeWindowChecker := &MockTimeWindowChecker{}
	service.timeWindowChecker = mockTimeWindowChecker
	mockTimeWindowChecker.On("IsInDateTimePeriod", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.True(t, result)
	mockTimeUtils.AssertExpectations(t)
	mockTimeWindowChecker.AssertExpectations(t)
}

func TestServiceImpl_IsUnderMaintenance_RecurringIntervalStrategy(t *testing.T) {
	service, _, _, _, _, mockTimeUtils, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "recurring-interval"
	maintenance.Active = true

	startDateTime := "2024-01-01T09:00"
	endDateTime := "2024-12-31T17:00"
	intervalDay := 7
	maintenance.StartDateTime = &startDateTime
	maintenance.EndDateTime = &endDateTime
	maintenance.IntervalDay = &intervalDay

	loc := time.UTC
	mockTimeUtils.On("GetDefaultTimezone").Return("UTC")
	mockTimeUtils.On("LoadTimezone", "UTC").Return(loc)

	// Create a new mock time window checker for this test
	mockTimeWindowChecker := &MockTimeWindowChecker{}
	service.timeWindowChecker = mockTimeWindowChecker
	mockTimeWindowChecker.On("IsInDateTimePeriod", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)
	mockTimeWindowChecker.On("IsInRecurringIntervalWindow", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.True(t, result)
	mockTimeUtils.AssertExpectations(t)
	mockTimeWindowChecker.AssertExpectations(t)
}

func TestServiceImpl_IsUnderMaintenance_CronStrategy(t *testing.T) {
	service, _, _, _, _, mockTimeUtils, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "recurring-weekday"
	maintenance.Active = true

	startDateTime := "2024-01-01T09:00"
	endDateTime := "2024-12-31T17:00"
	cron := "0 9 * * 1-5"
	maintenance.StartDateTime = &startDateTime
	maintenance.EndDateTime = &endDateTime
	maintenance.Cron = &cron

	loc := time.UTC
	mockTimeUtils.On("GetDefaultTimezone").Return("UTC")
	mockTimeUtils.On("LoadTimezone", "UTC").Return(loc)

	// Create a new mock time window checker for this test
	mockTimeWindowChecker := &MockTimeWindowChecker{}
	service.timeWindowChecker = mockTimeWindowChecker
	mockTimeWindowChecker.On("IsInDateTimePeriod", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)
	mockTimeWindowChecker.On("IsInCronMaintenanceWindow", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.True(t, result)
	mockTimeUtils.AssertExpectations(t)
	mockTimeWindowChecker.AssertExpectations(t)
}

// Test GetMaintenancesByMonitorID method
func TestServiceImpl_GetMaintenancesByMonitorID_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModels := []*Model{createTestModel()}

	mockRepo.On("GetMaintenancesByMonitorID", mock.Anything, "monitor1").Return(expectedModels, nil)

	result, err := service.GetMaintenancesByMonitorID(context.Background(), "monitor1")

	assert.NoError(t, err)
	assert.Equal(t, expectedModels, result)
	mockRepo.AssertExpectations(t)
}

// Test GetMonitors method
func TestServiceImpl_GetMonitors_Success(t *testing.T) {
	service, _, mockMonitorMaintenanceService, _, _, _, _ := createTestService()

	expectedMonitorIDs := []string{"monitor1", "monitor2"}

	mockMonitorMaintenanceService.On("GetMonitors", mock.Anything, "test-id").Return(expectedMonitorIDs, nil)

	result, err := service.GetMonitors(context.Background(), "test-id")

	assert.NoError(t, err)
	assert.Equal(t, expectedMonitorIDs, result)
	mockMonitorMaintenanceService.AssertExpectations(t)
}

// Test error scenarios
func TestServiceImpl_Create_RepositoryError(t *testing.T) {
	service, mockRepo, _, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	expectedError := errors.New("repository error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, dto).Return(nil, expectedError)

	result, err := service.Create(context.Background(), dto)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockRepo.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_MonitorMaintenanceServiceError(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	expectedModel := createTestModel()
	expectedError := errors.New("monitor maintenance service error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, dto).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, dto.MonitorIds).Return(expectedError)

	result, err := service.Create(context.Background(), dto)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockRepo.AssertExpectations(t)
	mockMonitorMaintenanceService.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_CronGenerationError(t *testing.T) {
	service, _, _, mockCronGenerator, _, _, mockValidator := createTestService()

	dto := createTestCreateUpdateDto()
	dto.Strategy = "recurring-weekday"
	dto.Cron = nil
	dto.Weekdays = []int{1, 2, 3, 4, 5}

	expectedError := errors.New("cron generation error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockCronGenerator.On("GenerateCronExpression", dto.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, expectedError)

	result, err := service.Create(context.Background(), dto)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockCronGenerator.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
}

func TestServiceImpl_IsUnderMaintenance_Inactive(t *testing.T) {
	service, _, _, _, _, _, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Active = false

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.False(t, result)
}

func TestServiceImpl_IsUnderMaintenance_UnknownStrategy(t *testing.T) {
	service, _, _, _, _, mockTimeUtils, _ := createTestService()

	maintenance := createTestModel()
	maintenance.Strategy = "unknown-strategy"
	maintenance.Active = true

	startDateTime := "2024-01-01T09:00"
	endDateTime := "2024-01-01T17:00"
	maintenance.StartDateTime = &startDateTime
	maintenance.EndDateTime = &endDateTime

	loc := time.UTC
	mockTimeUtils.On("GetDefaultTimezone").Return("UTC")
	mockTimeUtils.On("LoadTimezone", "UTC").Return(loc)

	// Create a new mock time window checker for this test
	mockTimeWindowChecker := &MockTimeWindowChecker{}
	service.timeWindowChecker = mockTimeWindowChecker
	mockTimeWindowChecker.On("IsInDateTimePeriod", mock.AnythingOfType("*utils.TimeWindowParams"), mock.AnythingOfType("time.Time"), loc).Return(true, nil)

	result, err := service.IsUnderMaintenance(context.Background(), maintenance)

	assert.NoError(t, err)
	assert.False(t, result) // Unknown strategy should return false
	mockTimeUtils.AssertExpectations(t)
	mockTimeWindowChecker.AssertExpectations(t)
}
