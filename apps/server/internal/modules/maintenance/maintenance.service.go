package maintenance

import (
	"context"
	"time"

	"go.uber.org/zap"

	"vigi/internal/modules/maintenance/utils"
	"vigi/internal/modules/monitor_maintenance"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error

	SetActive(ctx context.Context, id string, active bool, orgID string) (*Model, error)

	// GetStatus returns whether the maintenance is currently active
	IsUnderMaintenance(ctx context.Context, maintenance *Model) (bool, error)

	// Get maintenances by monitor ID
	GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)

	// Get monitors for a maintenance
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
		timeUtils:                 utils.NewTimeUtils(),
		validator:                 utils.NewValidator(),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
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

	// Store times directly without timezone conversion
	created, err := mr.repository.Create(ctx, entity)
	if err != nil {
		return nil, err
	}

	// Handle monitor IDs if provided
	if entity.MonitorIds != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, created.ID, entity.MonitorIds)
		if err != nil {
			return nil, err
		}
	}

	return created, nil
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	model, err := mr.repository.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	return model, nil
}

func (mr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*Model, error) {
	models, err := mr.repository.FindAll(ctx, page, limit, q, strategy, orgID)
	if err != nil {
		return nil, err
	}

	return models, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
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

	// Store times directly without timezone conversion
	updated, err := mr.repository.UpdateFull(ctx, id, entity, orgID)
	if err != nil {
		return nil, err
	}

	// Handle monitor IDs if provided
	if entity.MonitorIds != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, id, entity.MonitorIds)
		if err != nil {
			return nil, err
		}
	}

	return updated, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
	// If strategy is being updated, we might need to regenerate cron expression
	if entity.Strategy != nil {
		// Get the current maintenance to merge with partial update
		current, err := mr.repository.FindByID(ctx, id, orgID)
		if err != nil {
			return nil, err
		}

		// Create a temporary DTO to check if we need to generate cron
		tempDto := &CreateUpdateDto{
			Strategy:    *entity.Strategy,
			StartTime:   entity.StartTime,
			EndTime:     entity.EndTime,
			Weekdays:    entity.Weekdays,
			DaysOfMonth: entity.DaysOfMonth,
			IntervalDay: entity.IntervalDay,
		}

		// Use current values if not provided in partial update
		if tempDto.StartTime == nil {
			tempDto.StartTime = current.StartTime
		}
		if tempDto.Weekdays == nil {
			tempDto.Weekdays = current.Weekdays
		}
		if tempDto.DaysOfMonth == nil {
			tempDto.DaysOfMonth = current.DaysOfMonth
		}
		if tempDto.IntervalDay == nil {
			tempDto.IntervalDay = current.IntervalDay
		}

		// Generate cron expression for recurring strategies if not provided
		if entity.Cron == nil || *entity.Cron == "" {
			generatedCron, err := mr.generateCronExpression(tempDto)
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
		current, err := mr.repository.FindByID(ctx, id, orgID)
		if err != nil {
			return nil, err
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

	// Store times directly without timezone conversion
	updated, err := mr.repository.UpdatePartial(ctx, id, entity, orgID)
	if err != nil {
		return nil, err
	}

	// Handle monitor IDs if provided
	if entity.MonitorIds != nil {
		err = mr.monitorMaintenanceService.SetMonitors(ctx, id, entity.MonitorIds)
		if err != nil {
			return nil, err
		}
	}

	return updated, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string, orgID string) error {
	return mr.repository.Delete(ctx, id, orgID)
}

func (mr *ServiceImpl) SetActive(ctx context.Context, id string, active bool, orgID string) (*Model, error) {
	model, err := mr.repository.SetActive(ctx, id, active, orgID)
	if err != nil {
		return nil, err
	}

	return model, nil
}

// IsUnderMaintenance determines if the maintenance is currently active based on strategy and timing
func (mr *ServiceImpl) IsUnderMaintenance(ctx context.Context, maintenance *Model) (bool, error) {
	mr.logger.Debugf("Checking if maintenance %s is under maintenance", maintenance.ID)
	mr.logger.Debugf("Maintenance: %+v", maintenance)

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
func (mr *ServiceImpl) generateCronExpression(dto *CreateUpdateDto) (*string, error) {
	params := &utils.CronParams{
		StartTime:   dto.StartTime,
		EndTime:     dto.EndTime,
		Weekdays:    dto.Weekdays,
		DaysOfMonth: dto.DaysOfMonth,
		IntervalDay: dto.IntervalDay,
	}

	return mr.cronGenerator.GenerateCronExpression(dto.Strategy, params)
}

// GetMaintenancesByMonitorID returns all active maintenances for a given monitor_id
func (mr *ServiceImpl) GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	models, err := mr.repository.GetMaintenancesByMonitorID(ctx, monitorID)
	if err != nil {
		return nil, err
	}

	return models, nil
}

// GetMonitors returns the list of monitor IDs for a given maintenance
func (mr *ServiceImpl) GetMonitors(ctx context.Context, id string) ([]string, error) {
	return mr.monitorMaintenanceService.GetMonitors(ctx, id)
}
