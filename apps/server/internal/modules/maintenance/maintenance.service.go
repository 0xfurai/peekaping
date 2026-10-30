package maintenance

import (
	"context"
	"time"

	"go.uber.org/zap"

	"peekaping/internal/modules/maintenance/utils"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/validation"
)

// Service works on maintenance windows, which belong to a workspace (docs/adr/0005 section 3).
//
// A method that serves a workspace's request takes the workspace right after the context, and
// answers ErrNotFound for a window of another workspace. A window covers only the monitors of its
// own workspace: a create or update that names a monitor of another workspace, or one that doesn't
// exist, fails with monitor_maintenance.ErrUnknownMonitor before anything is written. A method
// whose name ends in AnyWorkspace works across workspaces, for the paths that run for the whole
// install.
type Service interface {
	// Check says whether Create would take the window, without writing anything: the rules the
	// create route holds a body to, and the cron and duration Create works out. It is nil when it
	// would, and otherwise says why not.
	Check(entity *Model) error
	// Create makes the window in the workspace, whatever workspace the window says
	Create(ctx context.Context, workspaceID workspace.ID, entity *Model, monitorIDs []string) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, strategy string) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every window of the install, for the usage ping
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model, monitorIDs []string) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel, monitorIDs []string) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error

	SetActive(ctx context.Context, workspaceID workspace.ID, id string, active bool) (*Model, error)

	// GetStatus returns whether the maintenance is currently active
	IsUnderMaintenance(ctx context.Context, maintenance *Model) (bool, error)

	// GetMaintenancesByMonitorIDAnyWorkspace returns the active windows of a monitor. It is for
	// the checks and the push endpoint, which decide for a monitor whatever its workspace.
	GetMaintenancesByMonitorIDAnyWorkspace(ctx context.Context, monitorID string) ([]*Model, error)

	// Get monitors for a maintenance. The caller has loaded the window in its workspace first.
	GetMonitors(ctx context.Context, id string) ([]string, error)
}

type ServiceImpl struct {
	repository                Repository
	monitorMaintenanceService monitor_maintenance.Service
	logger                    *zap.SugaredLogger
	cronGenerator             utils.CronGeneratorInterface
	timeWindowChecker         utils.TimeWindowCheckerInterface
	timeUtils                 utils.TimeUtilsInterface
	validator                 utils.ValidatorInterface
}

func NewService(
	repository Repository,
	monitorMaintenanceService monitor_maintenance.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository:                repository,
		monitorMaintenanceService: monitorMaintenanceService,
		logger:                    logger.Named("[maintenance-service]"),
		cronGenerator:             utils.NewCronGenerator(),
		timeWindowChecker:         utils.NewTimeWindowChecker(logger),
		timeUtils:                 utils.NewTimeUtils(logger),
		validator:                 utils.NewValidator(),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, entity *Model, monitorIDs []string) (*Model, error) {
	if err := mr.prepareSchedule(entity); err != nil {
		return nil, err
	}

	// Every monitor must be in the workspace, and is checked before the window is written
	if err := mr.monitorMaintenanceService.RequireMonitors(ctx, workspaceID, monitorIDs); err != nil {
		return nil, err
	}

	// Store times directly without timezone conversion
	created, err := mr.repository.Create(ctx, workspaceID, entity)
	if err != nil {
		return nil, err
	}

	if err := mr.setMonitors(ctx, created.ID, monitorIDs); err != nil {
		return nil, err
	}

	return created, nil
}

// prepareSchedule validates the cron and duration of a window as they come, then fills in the cron
// and the duration it leaves out
func (mr *ServiceImpl) prepareSchedule(entity *Model) error {
	// Validate cron and duration
	if err := mr.validator.ValidateCronAndDuration(&utils.ValidationParams{
		Cron:     entity.Cron,
		Duration: entity.Duration,
	}); err != nil {
		return err
	}

	if err := mr.fillCron(&entity.Cron, entity); err != nil {
		return err
	}

	// Calculate duration from StartTime and EndTime if not provided
	if entity.Duration == nil && entity.StartTime != nil && entity.EndTime != nil {
		duration, err := mr.calculateDuration(*entity.StartTime, *entity.EndTime)
		if err != nil {
			return err
		}
		entity.Duration = &duration
	}
	return nil
}

// Check says whether the controller and Create would take the window, and changes nothing: the
// rules of the create route's body, then the cron and duration that Create works out
func (mr *ServiceImpl) Check(entity *Model) error {
	if err := validation.Validate.Struct(dtoOf(entity)); err != nil {
		return err
	}
	copied := *entity
	return mr.prepareSchedule(&copied)
}

// fillCron generates the cron expression of the schedule into cron, if it has none, for the
// recurring strategies that have one
func (mr *ServiceImpl) fillCron(cron **string, schedule *Model) error {
	// Generate cron expression for recurring strategies if not provided
	if *cron != nil && **cron != "" {
		return nil
	}
	generatedCron, err := mr.generateCronExpression(schedule)
	if err != nil {
		mr.logger.Errorf("Failed to generate cron expression for maintenance: %v", err)
		return err
	}
	if generatedCron != nil {
		*cron = generatedCron
		mr.logger.Debugf("Generated cron expression for maintenance: %s", *generatedCron)
	}
	return nil
}

// calculateDuration is the duration in minutes between a start and an end time
func (mr *ServiceImpl) calculateDuration(startTime, endTime string) (int, error) {
	duration, err := mr.timeUtils.CalculateDurationFromTimes(startTime, endTime)
	if err != nil {
		mr.logger.Errorf("Failed to calculate duration from start and end times: %v", err)
		return 0, err
	}
	mr.logger.Debugf("Calculated duration from start/end times: %d minutes", duration)
	return duration, nil
}

// setMonitors sets the monitors of the window, if the request names any
func (mr *ServiceImpl) setMonitors(ctx context.Context, id string, monitorIDs []string) error {
	// Handle monitor IDs if provided
	if monitorIDs == nil {
		return nil
	}
	return mr.monitorMaintenanceService.SetMonitors(ctx, id, monitorIDs)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	model, err := mr.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}

	return model, nil
}

func (mr *ServiceImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return mr.repository.FindAllAnyWorkspace(ctx, page, limit)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string, strategy string) ([]*Model, error) {
	models, err := mr.repository.FindAll(ctx, workspaceID, page, limit, q, strategy)
	if err != nil {
		return nil, err
	}

	return models, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *Model, monitorIDs []string) (*Model, error) {
	if err := mr.prepareSchedule(entity); err != nil {
		return nil, err
	}

	// A window of another workspace is not found, whatever monitors the request names, and every
	// monitor must be in the workspace, both before anything is written
	if err := mr.requireWindowAndMonitors(ctx, workspaceID, id, monitorIDs); err != nil {
		return nil, err
	}

	// Store times directly without timezone conversion
	updated, err := mr.repository.UpdateFull(ctx, workspaceID, id, entity)
	if err != nil {
		return nil, err
	}

	if err := mr.setMonitors(ctx, id, monitorIDs); err != nil {
		return nil, err
	}

	return updated, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel, monitorIDs []string) (*Model, error) {
	if err := mr.preparePartialSchedule(ctx, workspaceID, id, entity); err != nil {
		return nil, err
	}

	// A window of another workspace is not found, whatever monitors the request names, and every
	// monitor must be in the workspace, both before anything is written
	if err := mr.requireWindowAndMonitors(ctx, workspaceID, id, monitorIDs); err != nil {
		return nil, err
	}

	// Store times directly without timezone conversion
	updated, err := mr.repository.UpdatePartial(ctx, workspaceID, id, entity)
	if err != nil {
		return nil, err
	}

	if err := mr.setMonitors(ctx, id, monitorIDs); err != nil {
		return nil, err
	}

	return updated, nil
}

// preparePartialSchedule fills in the cron and the duration a partial update leaves out, from the
// update over the window it changes, and validates them
func (mr *ServiceImpl) preparePartialSchedule(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error {
	// If strategy is being updated, we might need to regenerate cron expression
	if entity.Strategy != nil {
		if err := mr.fillCronForStrategy(ctx, workspaceID, id, entity); err != nil {
			return err
		}
	}

	// Calculate duration from StartTime and EndTime if not provided
	if entity.Duration == nil {
		if err := mr.fillDurationFromTimes(ctx, workspaceID, id, entity); err != nil {
			return err
		}
	}

	// Validate that if cron is provided, duration is also required
	return mr.validator.ValidateCronAndDuration(&utils.ValidationParams{
		Cron:     entity.Cron,
		Duration: entity.Duration,
	})
}

// currentWindow is the window a partial update changes, ErrNotFound if it isn't in the workspace
func (mr *ServiceImpl) currentWindow(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	current, err := mr.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}
	return current, nil
}

// fillCronForStrategy generates the cron expression for a changed strategy, if the update has none
func (mr *ServiceImpl) fillCronForStrategy(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error {
	// Get the current maintenance to merge with partial update
	current, err := mr.currentWindow(ctx, workspaceID, id)
	if err != nil {
		return err
	}

	schedule := &Model{
		Strategy:    *entity.Strategy,
		StartTime:   entity.StartTime,
		EndTime:     entity.EndTime,
		Weekdays:    entity.Weekdays,
		DaysOfMonth: entity.DaysOfMonth,
		IntervalDay: entity.IntervalDay,
	}

	// Use current values if not provided in partial update
	if schedule.StartTime == nil {
		schedule.StartTime = current.StartTime
	}
	if schedule.Weekdays == nil {
		schedule.Weekdays = current.Weekdays
	}
	if schedule.DaysOfMonth == nil {
		schedule.DaysOfMonth = current.DaysOfMonth
	}
	if schedule.IntervalDay == nil {
		schedule.IntervalDay = current.IntervalDay
	}

	return mr.fillCron(&entity.Cron, schedule)
}

// fillDurationFromTimes calculates the duration of an update from its start and end times, each
// of which is the window's if the update has none
func (mr *ServiceImpl) fillDurationFromTimes(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel) error {
	// Get current maintenance to merge with partial update
	current, err := mr.currentWindow(ctx, workspaceID, id)
	if err != nil {
		return err
	}

	// Determine start and end times (use provided values or current values)
	startTime := entity.StartTime
	if startTime == nil {
		startTime = current.StartTime
	}

	endTime := entity.EndTime
	if endTime == nil {
		endTime = current.EndTime
	}

	// Calculate duration if both times are available
	if startTime != nil && endTime != nil {
		duration, err := mr.calculateDuration(*startTime, *endTime)
		if err != nil {
			return err
		}
		entity.Duration = &duration
	}
	return nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	return mr.repository.Delete(ctx, workspaceID, id)
}

// requireWindowAndMonitors is ErrNotFound when the window isn't in the workspace, and otherwise
// the error of the first monitor that isn't in it
func (mr *ServiceImpl) requireWindowAndMonitors(ctx context.Context, workspaceID workspace.ID, id string, monitorIDs []string) error {
	found, err := mr.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if found == nil {
		return ErrNotFound
	}
	return mr.monitorMaintenanceService.RequireMonitors(ctx, workspaceID, monitorIDs)
}

func (mr *ServiceImpl) SetActive(ctx context.Context, workspaceID workspace.ID, id string, active bool) (*Model, error) {
	model, err := mr.repository.SetActive(ctx, workspaceID, id, active)
	if err != nil {
		return nil, err
	}

	return model, nil
}

// IsUnderMaintenance determines if the maintenance is currently active based on strategy and timing
func (mr *ServiceImpl) IsUnderMaintenance(ctx context.Context, maintenance *Model) (bool, error) {
	mr.logger.Debugf("Checking if maintenance %s is under maintenance", maintenance.ID)

	// If not active, return false
	if !maintenance.Active {
		return false, nil
	}

	if maintenance.Strategy == "manual" {
		return maintenance.Active, nil
	}

	timezone := mr.timezoneOf(maintenance)

	mr.logger.Debugf("timezone: %s", timezone)

	// Load timezone
	loc := mr.timeUtils.LoadTimezone(timezone)
	now := time.Now().In(loc)

	timeWindowParams := timeWindowParamsOf(maintenance)

	isInDateTimePeriod, err := mr.timeWindowChecker.IsInDateTimePeriod(timeWindowParams, now, loc)
	if err != nil {
		return false, err
	}
	mr.logger.Debugf("isInDateTimePeriod: %t", isInDateTimePeriod)

	// Handle single strategy - under-maintenance when within the scheduled time
	if maintenance.Strategy == "single" {
		return isInDateTimePeriod, nil
	}

	// Handle recurring-interval strategy
	if maintenance.Strategy == "recurring-interval" {
		return mr.inRecurringSlot(mr.timeWindowChecker.IsInRecurringIntervalWindow, "isInRecurringInterval", timeWindowParams, isInDateTimePeriod, now, loc)
	}

	// Handle cron strategy
	if maintenance.Cron != nil && *maintenance.Cron != "" {
		return mr.inRecurringSlot(mr.timeWindowChecker.IsInCronMaintenanceWindow, "isInCronWindow", timeWindowParams, isInDateTimePeriod, now, loc)
	}

	// For any other strategy or unhandled cases
	return false, nil
}

// inRecurringSlot reports whether now is in a slot of a recurring window, as check finds it, and in
// the window's date and time period
func (mr *ServiceImpl) inRecurringSlot(check func(*utils.TimeWindowParams, time.Time, *time.Location) (bool, error), label string, params *utils.TimeWindowParams, isInDateTimePeriod bool, now time.Time, loc *time.Location) (bool, error) {
	isInSlot, err := check(params, now, loc)
	if err != nil {
		return false, err
	}

	mr.logger.Debugf("%s: %t", label, isInSlot)

	return isInSlot && isInDateTimePeriod, nil
}

// timezoneOf is the timezone of the window (default to UTC if not specified)
func (mr *ServiceImpl) timezoneOf(maintenance *Model) string {
	timezone := mr.timeUtils.GetDefaultTimezone()
	if maintenance.Timezone != nil && *maintenance.Timezone != "" {
		timezone = *maintenance.Timezone
	}
	return timezone
}

// timeWindowParamsOf are the time window parameters of the window
func timeWindowParamsOf(maintenance *Model) *utils.TimeWindowParams {
	return &utils.TimeWindowParams{
		StartDateTime: maintenance.StartDateTime,
		EndDateTime:   maintenance.EndDateTime,
		StartTime:     maintenance.StartTime,
		EndTime:       maintenance.EndTime,
		IntervalDay:   maintenance.IntervalDay,
		Cron:          maintenance.Cron,
		Duration:      maintenance.Duration,
		Weekdays:      maintenance.Weekdays,
		DaysOfMonth:   maintenance.DaysOfMonth,
		Timezone:      maintenance.Timezone,
	}
}

// generateCronExpression generates a cron expression based on the maintenance strategy and parameters
func (mr *ServiceImpl) generateCronExpression(model *Model) (*string, error) {
	params := &utils.CronParams{
		StartTime:   model.StartTime,
		EndTime:     model.EndTime,
		Weekdays:    model.Weekdays,
		DaysOfMonth: model.DaysOfMonth,
		IntervalDay: model.IntervalDay,
	}

	return mr.cronGenerator.GenerateCronExpression(model.Strategy, params)
}

// GetMaintenancesByMonitorIDAnyWorkspace returns all active maintenances for a given monitor_id
func (mr *ServiceImpl) GetMaintenancesByMonitorIDAnyWorkspace(ctx context.Context, monitorID string) ([]*Model, error) {
	models, err := mr.repository.GetMaintenancesByMonitorIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	return models, nil
}

// GetMonitors returns the list of monitor IDs for a given maintenance
func (mr *ServiceImpl) GetMonitors(ctx context.Context, id string) ([]string, error) {
	return mr.monitorMaintenanceService.GetMonitors(ctx, id)
}
