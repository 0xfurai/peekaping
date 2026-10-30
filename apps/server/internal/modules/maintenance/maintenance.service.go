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
	if err := mr.prepare(entity); err != nil {
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

	// Handle monitor IDs if provided
	if monitorIDs != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, created.ID, monitorIDs)
		if err != nil {
			return nil, err
		}
	}

	return created, nil
}

// prepare checks the window's cron and duration, and fills in the cron and the duration that follow
// from its strategy and times when it has none. UpdateFull does the same inline.
func (mr *ServiceImpl) prepare(entity *Model) error {
	// Validate cron and duration
	if err := mr.validator.ValidateCronAndDuration(&utils.ValidationParams{
		Cron:     entity.Cron,
		Duration: entity.Duration,
	}); err != nil {
		return err
	}
	if err := mr.fillCron(entity); err != nil {
		return err
	}
	return mr.fillDuration(entity)
}

// fillCron generates the cron expression of a recurring strategy that has none
func (mr *ServiceImpl) fillCron(entity *Model) error {
	if entity.Cron != nil && *entity.Cron != "" {
		return nil
	}
	generatedCron, err := mr.generateCronExpression(entity)
	if err != nil {
		mr.logger.Errorf("Failed to generate cron expression for maintenance: %v", err)
		return err
	}
	if generatedCron != nil {
		entity.Cron = generatedCron
		mr.logger.Debugf("Generated cron expression for maintenance: %s", *generatedCron)
	}
	return nil
}

// fillDuration calculates the duration from the start and end times when the window has none
func (mr *ServiceImpl) fillDuration(entity *Model) error {
	if entity.Duration != nil || entity.StartTime == nil || entity.EndTime == nil {
		return nil
	}
	duration, err := mr.timeUtils.CalculateDurationFromTimes(*entity.StartTime, *entity.EndTime)
	if err != nil {
		mr.logger.Errorf("Failed to calculate duration from start and end times: %v", err)
		return err
	}
	entity.Duration = &duration
	mr.logger.Debugf("Calculated duration from start/end times: %d minutes", duration)
	return nil
}

// Check says whether the controller and Create would take the window, and changes nothing: the
// rules of the create route's body, then the cron and duration that Create works out
func (mr *ServiceImpl) Check(entity *Model) error {
	if err := validation.Validate.Struct(dtoOf(entity)); err != nil {
		return err
	}
	copied := *entity
	return mr.prepare(&copied)
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
	// Validate cron and duration
	if err := mr.validator.ValidateCronAndDuration(&utils.ValidationParams{
		Cron:     entity.Cron,
		Duration: entity.Duration,
	}); err != nil {
		return nil, err
	}

	// Generate cron expression for recurring strategies if not provided
	if entity.Cron == nil || *entity.Cron == "" {
		generatedCron, err := mr.generateCronExpression(entity)
		if err != nil {
			mr.logger.Errorf("Failed to generate cron expression for maintenance: %v", err)
			return nil, err
		}
		if generatedCron != nil {
			entity.Cron = generatedCron
			mr.logger.Debugf("Generated cron expression for maintenance: %s", *generatedCron)
		}
	}

	// Calculate duration from StartTime and EndTime if not provided
	if entity.Duration == nil && entity.StartTime != nil && entity.EndTime != nil {
		duration, err := mr.timeUtils.CalculateDurationFromTimes(*entity.StartTime, *entity.EndTime)
		if err != nil {
			mr.logger.Errorf("Failed to calculate duration from start and end times: %v", err)
			return nil, err
		}
		entity.Duration = &duration
		mr.logger.Debugf("Calculated duration from start/end times: %d minutes", duration)
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

	// Handle monitor IDs if provided
	if monitorIDs != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, id, monitorIDs)
		if err != nil {
			return nil, err
		}
	}

	return updated, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *UpdateModel, monitorIDs []string) (*Model, error) {
	// If strategy is being updated, we might need to regenerate cron expression
	if entity.Strategy != nil {
		// Get the current maintenance to merge with partial update
		current, err := mr.repository.FindByID(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, ErrNotFound
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

		// Generate cron expression for recurring strategies if not provided
		if entity.Cron == nil || *entity.Cron == "" {
			generatedCron, err := mr.generateCronExpression(schedule)
			if err != nil {
				mr.logger.Errorf("Failed to generate cron expression for maintenance: %v", err)
				return nil, err
			}
			if generatedCron != nil {
				entity.Cron = generatedCron
				mr.logger.Debugf("Generated cron expression for maintenance: %s", *generatedCron)
			}
		}
	}

	// Calculate duration from StartTime and EndTime if not provided
	if entity.Duration == nil {
		// Get current maintenance to merge with partial update
		current, err := mr.repository.FindByID(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, ErrNotFound
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
			duration, err := mr.timeUtils.CalculateDurationFromTimes(*startTime, *endTime)
			if err != nil {
				mr.logger.Errorf("Failed to calculate duration from start and end times: %v", err)
				return nil, err
			}
			entity.Duration = &duration
			mr.logger.Debugf("Calculated duration from start/end times: %d minutes", duration)
		}
	}

	// Validate that if cron is provided, duration is also required
	if err := mr.validator.ValidateCronAndDuration(&utils.ValidationParams{
		Cron:     entity.Cron,
		Duration: entity.Duration,
	}); err != nil {
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

	// Handle monitor IDs if provided
	if monitorIDs != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, id, monitorIDs)
		if err != nil {
			return nil, err
		}
	}

	return updated, nil
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

	// Get timezone (default to UTC if not specified)
	timezone := mr.timeUtils.GetDefaultTimezone()
	if maintenance.Timezone != nil && *maintenance.Timezone != "" {
		timezone = *maintenance.Timezone
	}

	mr.logger.Debugf("timezone: %s", timezone)

	// Load timezone
	loc := mr.timeUtils.LoadTimezone(timezone)
	now := time.Now().In(loc)

	// Create time window parameters
	timeWindowParams := &utils.TimeWindowParams{
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
		isInRecurringInterval, err := mr.timeWindowChecker.IsInRecurringIntervalWindow(timeWindowParams, now, loc)
		if err != nil {
			return false, err
		}

		mr.logger.Debugf("isInRecurringInterval: %t", isInRecurringInterval)

		return isInRecurringInterval && isInDateTimePeriod, nil
	}

	// Handle cron strategy
	if maintenance.Cron != nil && *maintenance.Cron != "" {
		isInCronWindow, err := mr.timeWindowChecker.IsInCronMaintenanceWindow(timeWindowParams, now, loc)
		if err != nil {
			return false, err
		}

		mr.logger.Debugf("isInCronWindow: %t", isInCronWindow)

		return isInCronWindow && isInDateTimePeriod, nil
	}

	// For any other strategy or unhandled cases
	return false, nil
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
