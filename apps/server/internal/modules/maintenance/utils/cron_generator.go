package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronGenerator handles generation of cron expressions for different maintenance strategies
type CronGenerator struct{}

// NewCronGenerator creates a new cron generator instance
func NewCronGenerator() *CronGenerator {
	return &CronGenerator{}
}

// GenerateCronExpression generates a cron expression based on the maintenance strategy and parameters
func (cg *CronGenerator) GenerateCronExpression(strategy string, params *CronParams) (*string, error) {
	switch strategy {
	case "recurring-interval":
		return cg.generateRecurringIntervalCron(params)
	case "recurring-weekday":
		return cg.generateRecurringWeekdayCron(params)
	case "recurring-day-of-month":
		return cg.generateRecurringDayOfMonthCron(params)
	default:
		// For non-recurring strategies, return nil (no cron needed)
		return nil, nil
	}
}

// CronParams contains the parameters needed for cron generation
type CronParams struct {
	StartTime   *string
	EndTime     *string
	Weekdays    []int
	DaysOfMonth []int
	IntervalDay *int
}

// generateRecurringIntervalCron generates cron expression for recurring-interval strategy
func (cg *CronGenerator) generateRecurringIntervalCron(params *CronParams) (*string, error) {
	if params.StartTime == nil || params.IntervalDay == nil || *params.IntervalDay <= 0 {
		return nil, errors.New("recurring-interval strategy requires start_time and valid interval_day")
	}

	// Parse start time to get hour and minute
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return nil, errors.New("invalid start_time format")
	}

	// For recurring-interval, we need to create a cron that runs every X days at the specified time
	// Since cron doesn't support "every X days" directly, we'll use a more complex approach
	// For now, we'll create a daily cron and let the service logic handle the interval check
	hour := startTime.Hour()
	minute := startTime.Minute()

	// Note: The actual interval logic is handled in the maintenance window checker
	// This cron will trigger daily, but the service will check if it's the right interval day
	cronExpr := fmt.Sprintf("%d %d * * *", minute, hour)
	return &cronExpr, nil
}

// generateRecurringWeekdayCron generates cron expression for recurring-weekday strategy
func (cg *CronGenerator) generateRecurringWeekdayCron(params *CronParams) (*string, error) {
	if params.StartTime == nil || len(params.Weekdays) == 0 {
		return nil, errors.New("recurring-weekday strategy requires start_time and weekdays")
	}

	// Parse start time to get hour and minute
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return nil, errors.New("invalid start_time format")
	}

	hour := startTime.Hour()
	minute := startTime.Minute()

	// Convert weekdays to cron format (0=Sunday, 1=Monday, ..., 6=Saturday)
	// Cron uses 0=Sunday, 1=Monday, ..., 6=Saturday, so no conversion needed
	weekdayList := make([]string, len(params.Weekdays))
	for i, weekday := range params.Weekdays {
		weekdayList[i] = strconv.Itoa(weekday)
	}
	weekdays := strings.Join(weekdayList, ",")

	cronExpr := fmt.Sprintf("%d %d * * %s", minute, hour, weekdays)
	return &cronExpr, nil
}

// generateRecurringDayOfMonthCron generates cron expression for recurring-day-of-month strategy
func (cg *CronGenerator) generateRecurringDayOfMonthCron(params *CronParams) (*string, error) {
	if params.StartTime == nil || len(params.DaysOfMonth) == 0 {
		return nil, errors.New("recurring-day-of-month strategy requires start_time and days_of_month")
	}

	// Parse start time to get hour and minute
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return nil, errors.New("invalid start_time format")
	}

	hour := startTime.Hour()
	minute := startTime.Minute()

	// Convert days of month to cron format
	dayList := make([]string, len(params.DaysOfMonth))
	for i, day := range params.DaysOfMonth {
		dayList[i] = strconv.Itoa(day)
	}
	days := strings.Join(dayList, ",")

	cronExpr := fmt.Sprintf("%d %d %s * *", minute, hour, days)
	return &cronExpr, nil
}
