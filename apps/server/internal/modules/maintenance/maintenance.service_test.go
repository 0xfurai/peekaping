package maintenance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"

	"peekaping/internal/modules/maintenance/utils"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/workspace"
)

// testWorkspace is the workspace the tests' requests run in
const testWorkspace = workspace.ID("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01")

// Mock dependencies
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Create(ctx context.Context, workspaceID workspace.ID, entity *Model) (*Model, error) {
	args := m.Called(ctx, workspaceID, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	args := m.Called(ctx, workspaceID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, strategy string) ([]*Model, error) {
	args := m.Called(ctx, workspaceID, page, limit, q, strategy)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model) (*Model, error) {
	args := m.Called(ctx, workspaceID, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) (*Model, error) {
	args := m.Called(ctx, workspaceID, id, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	args := m.Called(ctx, workspaceID, id)
	return args.Error(0)
}

func (m *MockRepository) SetActive(ctx context.Context, workspaceID workspace.ID, id string, active bool) (*Model, error) {
	args := m.Called(ctx, workspaceID, id, active)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Model), args.Error(1)
}

func (m *MockRepository) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*Model), args.Error(1)
}

func (m *MockRepository) GetMaintenancesByMonitorIDAnyWorkspace(ctx context.Context, monitorID string) ([]*Model, error) {
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

// RequireMonitors accepts every monitor unless a test says one is unknown
func (m *MockMonitorMaintenanceService) RequireMonitors(ctx context.Context, workspaceID workspace.ID, monitorIDs []string) error {
	if len(m.ExpectedCalls) == 0 {
		return nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "RequireMonitors" {
			return m.Called(ctx, workspaceID, monitorIDs).Error(0)
		}
	}
	return nil
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

func createTestCreateModel() *Model {
	startTime := "09:00"
	endTime := "17:00"
	duration := 480
	timezone := "UTC"

	return &Model{
		Title:       "Test Maintenance",
		Description: "Test Description",
		Active:      true,
		Strategy:    "single",
		StartTime:   &startTime,
		EndTime:     &endTime,
		Duration:    &duration,
		Timezone:    &timezone,
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

	model := createTestCreateModel()
	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, testWorkspace, model).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, []string{"monitor1", "monitor2"}).Return(nil)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
	mockMonitorMaintenanceService.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_ValidationError(t *testing.T) {
	service, _, _, _, _, _, mockValidator := createTestService()

	model := createTestCreateModel()
	expectedError := errors.New("validation error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(expectedError)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockValidator.AssertExpectations(t)
}

func TestServiceImpl_Create_WithCronGeneration(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	model := createTestCreateModel()
	model.Strategy = "recurring-weekday"
	model.Cron = nil // Will trigger cron generation
	model.Weekdays = []int{1, 2, 3, 4, 5}

	expectedModel := createTestModel()
	generatedCron := "0 9 * * 1-5"

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(&generatedCron, nil)
	mockRepo.On("Create", mock.Anything, testWorkspace, mock.MatchedBy(func(d *Model) bool {
		return d.Cron != nil && *d.Cron == generatedCron
	})).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, []string{"monitor1", "monitor2"}).Return(nil)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_WithDurationCalculation(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, mockTimeUtils, mockValidator := createTestService()

	model := createTestCreateModel()
	model.Duration = nil // Will trigger duration calculation
	expectedDuration := 480

	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockTimeUtils.On("CalculateDurationFromTimes", *model.StartTime, *model.EndTime).Return(expectedDuration, nil)
	mockRepo.On("Create", mock.Anything, testWorkspace, mock.MatchedBy(func(d *Model) bool {
		return d.Duration != nil && *d.Duration == expectedDuration
	})).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, []string{"monitor1", "monitor2"}).Return(nil)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockTimeUtils.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

// Test FindByID method
func TestServiceImpl_FindByID_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModel := createTestModel()

	mockRepo.On("FindByID", mock.Anything, testWorkspace, "test-id").Return(expectedModel, nil)

	result, err := service.FindByID(context.Background(), testWorkspace, "test-id")

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockRepo.AssertExpectations(t)
}

func TestServiceImpl_FindByID_NotFound(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedError := errors.New("not found")

	mockRepo.On("FindByID", mock.Anything, testWorkspace, "nonexistent-id").Return(nil, expectedError)

	result, err := service.FindByID(context.Background(), testWorkspace, "nonexistent-id")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockRepo.AssertExpectations(t)
}

// Test FindAll method
func TestServiceImpl_FindAll_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModels := []*Model{createTestModel()}

	mockRepo.On("FindAll", mock.Anything, testWorkspace, 1, 10, "test", "single").Return(expectedModels, nil)

	result, err := service.FindAll(context.Background(), testWorkspace, 1, 10, "test", "single")

	assert.NoError(t, err)
	assert.Equal(t, expectedModels, result)
	mockRepo.AssertExpectations(t)
}

// Test UpdateFull method
func TestServiceImpl_UpdateFull_Success(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	model := createTestCreateModel()
	expectedModel := createTestModel()

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("FindByID", mock.Anything, testWorkspace, "test-id").Return(expectedModel, nil)
	mockRepo.On("UpdateFull", mock.Anything, testWorkspace, "test-id", model).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, "test-id", []string{"monitor1", "monitor2"}).Return(nil)

	result, err := service.UpdateFull(context.Background(), testWorkspace, "test-id", model, []string{"monitor1", "monitor2"})

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
	model := &UpdateModel{
		Title:  &title,
		Active: &active,
	}

	currentModel := createTestModel()
	expectedModel := createTestModel()
	expectedModel.Title = title
	expectedModel.Active = active

	// UpdatePartial calls FindByID to get current maintenance for duration calculation
	mockRepo.On("FindByID", mock.Anything, testWorkspace, "test-id").Return(currentModel, nil)
	// UpdatePartial also calls CalculateDurationFromTimes since start and end times are available
	mockTimeUtils.On("CalculateDurationFromTimes", *currentModel.StartTime, *currentModel.EndTime).Return(480, nil)
	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockRepo.On("UpdatePartial", mock.Anything, testWorkspace, "test-id", mock.AnythingOfType("*maintenance.UpdateModel")).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, "test-id", []string{"monitor1"}).Return(nil)

	result, err := service.UpdatePartial(context.Background(), testWorkspace, "test-id", model, []string{"monitor1"})

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
	model := &UpdateModel{
		Strategy: &newStrategy,
	}

	currentModel := createTestModel()
	expectedModel := createTestModel()
	expectedModel.Strategy = newStrategy

	generatedCron := "0 9 * * 1-5"

	mockRepo.On("FindByID", mock.Anything, testWorkspace, "test-id").Return(currentModel, nil)
	mockCronGenerator.On("GenerateCronExpression", newStrategy, mock.AnythingOfType("*utils.CronParams")).Return(&generatedCron, nil)
	// UpdatePartial also calls CalculateDurationFromTimes since start and end times are available
	mockTimeUtils.On("CalculateDurationFromTimes", *currentModel.StartTime, *currentModel.EndTime).Return(480, nil)
	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockRepo.On("UpdatePartial", mock.Anything, testWorkspace, "test-id", mock.MatchedBy(func(d *UpdateModel) bool {
		return d.Cron != nil && *d.Cron == generatedCron
	})).Return(expectedModel, nil)

	result, err := service.UpdatePartial(context.Background(), testWorkspace, "test-id", model, nil)

	assert.NoError(t, err)
	assert.Equal(t, expectedModel, result)
	mockCronGenerator.AssertExpectations(t)
	mockTimeUtils.AssertExpectations(t)
}

// Test Delete method
func TestServiceImpl_Delete_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	mockRepo.On("Delete", mock.Anything, testWorkspace, "test-id").Return(nil)

	err := service.Delete(context.Background(), testWorkspace, "test-id")

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// Test SetActive method
func TestServiceImpl_SetActive_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModel := createTestModel()
	expectedModel.Active = false

	mockRepo.On("SetActive", mock.Anything, testWorkspace, "test-id", false).Return(expectedModel, nil)

	result, err := service.SetActive(context.Background(), testWorkspace, "test-id", false)

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

// A single maintenance window follows its date period. A recurring one also
// needs now to fall in one of its slots (every N days, or the slots its cron
// expression opens), so the checker answers every combination.
func TestServiceImpl_IsUnderMaintenance_TimedStrategies(t *testing.T) {
	intervalDay := 7
	cron := "0 9 * * 1-5"
	strategies := []struct {
		strategy string
		cron     *string
		slot     string // the checker method for the strategy's recurring slots
	}{
		{"single", nil, ""},
		{"recurring-interval", nil, "IsInRecurringIntervalWindow"},
		{"recurring-weekday", &cron, "IsInCronMaintenanceWindow"},
		{"recurring-day-of-month", &cron, "IsInCronMaintenanceWindow"},
		{"cron", &cron, "IsInCronMaintenanceWindow"},
	}
	for _, s := range strategies {
		for _, inPeriod := range []bool{true, false} {
			for _, inSlot := range []bool{true, false} {
				name := fmt.Sprintf("%s/period=%t/slot=%t", s.strategy, inPeriod, inSlot)
				if s.slot == "" {
					if !inSlot {
						continue // a single maintenance window has no slots
					}
					name = fmt.Sprintf("%s/period=%t", s.strategy, inPeriod)
				}
				t.Run(name, func(t *testing.T) {
					service, _, _, _, mockTimeWindowChecker, mockTimeUtils, _ := createTestService()
					mockTimeUtils.On("GetDefaultTimezone").Return("UTC")
					mockTimeUtils.On("LoadTimezone", "UTC").Return(time.UTC)
					mockTimeWindowChecker.On("IsInDateTimePeriod", mock.Anything, mock.Anything, time.UTC).Return(inPeriod, nil)
					if s.slot != "" {
						mockTimeWindowChecker.On(s.slot, mock.Anything, mock.Anything, time.UTC).Return(inSlot, nil)
					}

					maintenance := createTestModel()
					maintenance.Strategy = s.strategy
					maintenance.IntervalDay = &intervalDay
					maintenance.Cron = s.cron

					result, err := service.IsUnderMaintenance(context.Background(), maintenance)

					assert.NoError(t, err)
					assert.Equal(t, inPeriod && inSlot, result)
				})
			}
		}
	}
}

// Test GetMaintenancesByMonitorID method
func TestServiceImpl_GetMaintenancesByMonitorIDAnyWorkspace_Success(t *testing.T) {
	service, mockRepo, _, _, _, _, _ := createTestService()

	expectedModels := []*Model{createTestModel()}

	mockRepo.On("GetMaintenancesByMonitorIDAnyWorkspace", mock.Anything, "monitor1").Return(expectedModels, nil)

	result, err := service.GetMaintenancesByMonitorIDAnyWorkspace(context.Background(), "monitor1")

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

	model := createTestCreateModel()
	expectedError := errors.New("repository error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, testWorkspace, model).Return(nil, expectedError)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
	mockRepo.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
	mockCronGenerator.AssertExpectations(t)
}

func TestServiceImpl_Create_MonitorMaintenanceServiceError(t *testing.T) {
	service, mockRepo, mockMonitorMaintenanceService, mockCronGenerator, _, _, mockValidator := createTestService()

	model := createTestCreateModel()
	expectedModel := createTestModel()
	expectedError := errors.New("monitor maintenance service error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	// For "single" strategy, cron generator returns nil
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, nil)
	mockRepo.On("Create", mock.Anything, testWorkspace, model).Return(expectedModel, nil)
	mockMonitorMaintenanceService.On("SetMonitors", mock.Anything, expectedModel.ID, []string{"monitor1", "monitor2"}).Return(expectedError)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

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

	model := createTestCreateModel()
	model.Strategy = "recurring-weekday"
	model.Cron = nil
	model.Weekdays = []int{1, 2, 3, 4, 5}

	expectedError := errors.New("cron generation error")

	mockValidator.On("ValidateCronAndDuration", mock.AnythingOfType("*utils.ValidationParams")).Return(nil)
	mockCronGenerator.On("GenerateCronExpression", model.Strategy, mock.AnythingOfType("*utils.CronParams")).Return(nil, expectedError)

	result, err := service.Create(context.Background(), testWorkspace, model, []string{"monitor1", "monitor2"})

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

func TestServiceImpl_UpdatePartial_MonitorPresence(t *testing.T) {
	for _, monitorIDs := range [][]string{nil, {}} {
		name := "omitted"
		if monitorIDs != nil {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			service, repo, monitors, _, _, _, validator := createTestService()
			duration := 90
			patch := &UpdateModel{Duration: &duration}
			repo.On("FindByID", mock.Anything, testWorkspace, "maintenance").Return(&Model{ID: "maintenance"}, nil)
			repo.On("UpdatePartial", mock.Anything, testWorkspace, "maintenance", patch).Return(&Model{ID: "maintenance"}, nil)
			validator.On("ValidateCronAndDuration", mock.Anything).Return(nil)
			if monitorIDs != nil {
				monitors.On("SetMonitors", mock.Anything, "maintenance", monitorIDs).Return(nil).Once()
			}
			_, err := service.UpdatePartial(context.Background(), testWorkspace, "maintenance", patch, monitorIDs)
			assert.NoError(t, err)
			repo.AssertExpectations(t)
			monitors.AssertExpectations(t)
			if monitorIDs == nil {
				monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

// A window covers only the monitors of its own workspace, and a monitor of another workspace is
// refused as one that doesn't exist, before the window or a link is written (AC4)
func TestServiceImpl_Create_RefusesAMonitorOfAnotherWorkspaceBeforeWritingAnything(t *testing.T) {
	service, repo, monitors, cron, _, _, validator := createTestService()
	validator.On("ValidateCronAndDuration", mock.Anything).Return(nil)
	cron.On("GenerateCronExpression", mock.Anything, mock.Anything).Return(nil, nil)
	unknown := fmt.Errorf("%w: monitor1", monitor_maintenance.ErrUnknownMonitor)
	monitors.On("RequireMonitors", mock.Anything, testWorkspace, []string{"mine", "monitor1"}).Return(unknown)

	result, err := service.Create(context.Background(), testWorkspace, createTestCreateModel(), []string{"mine", "monitor1"})

	assert.ErrorIs(t, err, monitor_maintenance.ErrUnknownMonitor)
	assert.Nil(t, result)
	repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
	monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
}

func TestServiceImpl_Updates_RefuseAMonitorOfAnotherWorkspaceBeforeWritingAnything(t *testing.T) {
	for name, update := range map[string]func(s *ServiceImpl) error{
		"full": func(s *ServiceImpl) error {
			_, err := s.UpdateFull(context.Background(), testWorkspace, "window", createTestCreateModel(), []string{"monitor1"})
			return err
		},
		"partial": func(s *ServiceImpl) error {
			duration := 90
			_, err := s.UpdatePartial(context.Background(), testWorkspace, "window", &UpdateModel{Duration: &duration}, []string{"monitor1"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, repo, monitors, cron, _, _, validator := createTestService()
			validator.On("ValidateCronAndDuration", mock.Anything).Return(nil)
			cron.On("GenerateCronExpression", mock.Anything, mock.Anything).Return(nil, nil)
			repo.On("FindByID", mock.Anything, testWorkspace, "window").Return(&Model{ID: "window"}, nil)
			monitors.On("RequireMonitors", mock.Anything, testWorkspace, []string{"monitor1"}).Return(fmt.Errorf("%w: monitor1", monitor_maintenance.ErrUnknownMonitor))

			err := update(service)

			assert.ErrorIs(t, err, monitor_maintenance.ErrUnknownMonitor)
			repo.AssertNotCalled(t, "UpdateFull", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			repo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			monitors.AssertNotCalled(t, "SetMonitors", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// A window of another workspace is not found, whatever monitors the request names
func TestServiceImpl_Updates_AWindowOfAnotherWorkspaceIsNotFound(t *testing.T) {
	service, repo, monitors, cron, _, _, validator := createTestService()
	validator.On("ValidateCronAndDuration", mock.Anything).Return(nil)
	cron.On("GenerateCronExpression", mock.Anything, mock.Anything).Return(nil, nil)
	repo.On("FindByID", mock.Anything, testWorkspace, "window").Return(nil, nil)
	monitors.On("RequireMonitors", mock.Anything, testWorkspace, []string{"monitor1"}).Return(fmt.Errorf("%w: monitor1", monitor_maintenance.ErrUnknownMonitor))

	_, err := service.UpdateFull(context.Background(), testWorkspace, "window", createTestCreateModel(), []string{"monitor1"})

	assert.ErrorIs(t, err, ErrNotFound, "and not the answer for a monitor that isn't there")
	monitors.AssertNotCalled(t, "RequireMonitors", mock.Anything, mock.Anything, mock.Anything)
}
