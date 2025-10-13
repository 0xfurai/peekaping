package utils

import (
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// TimeWindowChecker handles time window validation and checking for maintenance schedules
type TimeWindowChecker struct {
	logger *zap.SugaredLogger
}

// NewTimeWindowChecker creates a new time window checker instance
func NewTimeWindowChecker(
	logger *zap.SugaredLogger,
) *TimeWindowChecker {
	return &TimeWindowChecker{
		logger: logger.Named("[time-window-checker]"),
	}
}

// TimeWindowParams contains the parameters needed for time window checking
type TimeWindowParams struct {
	StartDateTime *string
	EndDateTime   *string
	StartTime     *string
	EndTime       *string
	IntervalDay   *int
	Cron          *string
	Duration      *int
	Weekdays      []int
	DaysOfMonth   []int
	Timezone      *string
}

// IsInDateTimePeriod checks if the current time falls within the date time period
func (twc *TimeWindowChecker) IsInDateTimePeriod(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	if params.StartDateTime == nil || params.EndDateTime == nil {
		return false, errors.New("maintenance has no start or end date time")
	}

	// Set database time to timezone from db without converting
	startDateInTz := twc.convertToTimezone(*params.StartDateTime, loc)
	endDateInTz := twc.convertToTimezone(*params.EndDateTime, loc)

	// Check if we're within the date time window
	return now.After(startDateInTz) && now.Before(endDateInTz), nil
}

// IsInCronMaintenanceWindow checks if the current time falls within a cron-based maintenance window
func (twc *TimeWindowChecker) IsInCronMaintenanceWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	if params.Cron == nil || *params.Cron == "" {
		twc.logger.Debugf("cron is nil or empty")
		return false, nil
	}

	if params.Duration == nil || *params.Duration <= 0 {
		twc.logger.Debugf("duration is nil or less than or equal to 0")
		return false, nil
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(*params.Cron)
	if err != nil {
		twc.logger.Debugf("error parsing cron: %v", err)
		return false, err
	}

	duration := time.Duration(*params.Duration) * time.Minute
	searchStart := now.Add(-duration)

	lastRun := schedule.Next(searchStart)
	fmt.Println("lastRun", lastRun)
	if lastRun.After(now) {
		// The next run is in the future, so no run happened within the window
		twc.logger.Debugf("lastRun is after now", now)
		return false, nil
	}

	endOfWindow := lastRun.Add(duration)

	// now is within [lastRun, lastRun+duration)
	return now.Before(endOfWindow), nil
}

// IsInRecurringIntervalWindow checks if the current time falls within a recurring interval maintenance window
func (twc *TimeWindowChecker) IsInRecurringIntervalWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	// Validate required fields for recurring-interval strategy
	if params.StartDateTime == nil {
		return false, errors.New("maintenance has no start date time")
	}

	if params.IntervalDay == nil || *params.IntervalDay <= 0 {
		return false, errors.New("maintenance has no valid interval day")
	}

	if params.StartTime == nil || params.EndTime == nil {
		return false, errors.New("maintenance has no start or end time for daily window")
	}

	// Parse start and end times from string format (HH:MM)
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return false, errors.New("invalid start time format")
	}

	endTime, err := time.Parse("15:04", *params.EndTime)
	if err != nil {
		return false, errors.New("invalid end time format")
	}

	// Convert start date to the maintenance timezone
	startDateInTz := twc.convertToTimezone(*params.StartDateTime, loc)

	// Calculate days since the start date
	daysSinceStart := int(now.Sub(startDateInTz).Hours() / 24)

	// Check if today is a maintenance day based on the interval
	if daysSinceStart < 0 || daysSinceStart%*params.IntervalDay != 0 {
		return false, nil
	}

	// Today is a maintenance day, now check if we're within the daily time window
	// Create today's start and end times in the maintenance timezone
	todayStart := time.Date(now.Year(), now.Month(), now.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, loc)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, loc)

	// Handle case where end time is next day (e.g., 23:00 - 01:00)
	if endTime.Before(startTime) {
		// For cross-day windows, we need to check two possible windows:
		// 1. Window starting today: todayStart to tomorrow's endTime
		todayEnd = todayEnd.Add(24 * time.Hour)
		if now.After(todayStart) && now.Before(todayEnd) {
			return true, nil
		}

		// 2. Window starting yesterday: yesterdayStart to today's endTime
		yesterdayStart := todayStart.Add(-24 * time.Hour)
		todayEndOriginal := time.Date(now.Year(), now.Month(), now.Day(),
			endTime.Hour(), endTime.Minute(), 0, 0, loc)
		if now.After(yesterdayStart) && now.Before(todayEndOriginal) {
			return true, nil
		}

		return false, nil
	}

	// Check if we're within the daily time window
	return now.After(todayStart) && now.Before(todayEnd), nil
}

// IsInRecurringWeekdayWindow checks if the current time falls within a recurring weekday maintenance window
func (twc *TimeWindowChecker) IsInRecurringWeekdayWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	if params.StartTime == nil || params.EndTime == nil {
		return false, errors.New("maintenance has no start or end time for daily window")
	}

	if len(params.Weekdays) == 0 {
		return false, errors.New("maintenance has no weekdays specified")
	}

	// Parse start and end times from string format (HH:MM)
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return false, errors.New("invalid start time format")
	}

	endTime, err := time.Parse("15:04", *params.EndTime)
	if err != nil {
		return false, errors.New("invalid end time format")
	}

	// Check if today is a maintenance day (weekday)
	todayWeekday := int(now.Weekday())
	isMaintenanceDay := false
	for _, weekday := range params.Weekdays {
		if weekday == todayWeekday {
			isMaintenanceDay = true
			break
		}
	}

	if !isMaintenanceDay {
		return false, nil
	}

	// Today is a maintenance day, now check if we're within the daily time window
	// Create today's start and end times in the maintenance timezone
	todayStart := time.Date(now.Year(), now.Month(), now.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, loc)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, loc)

	// Handle case where end time is next day (e.g., 23:00 - 01:00)
	if endTime.Before(startTime) {
		// For cross-day windows, we need to check two possible windows:
		// 1. Window starting today: todayStart to tomorrow's endTime
		todayEnd = todayEnd.Add(24 * time.Hour)
		if now.After(todayStart) && now.Before(todayEnd) {
			return true, nil
		}

		// 2. Window starting yesterday: yesterdayStart to today's endTime
		yesterdayStart := todayStart.Add(-24 * time.Hour)
		todayEndOriginal := time.Date(now.Year(), now.Month(), now.Day(),
			endTime.Hour(), endTime.Minute(), 0, 0, loc)
		if now.After(yesterdayStart) && now.Before(todayEndOriginal) {
			return true, nil
		}

		return false, nil
	}

	// Check if we're within the daily time window
	return now.After(todayStart) && now.Before(todayEnd), nil
}

// IsInRecurringDayOfMonthWindow checks if the current time falls within a recurring day of month maintenance window
func (twc *TimeWindowChecker) IsInRecurringDayOfMonthWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	if params.StartTime == nil || params.EndTime == nil {
		return false, errors.New("maintenance has no start or end time for daily window")
	}

	if len(params.DaysOfMonth) == 0 {
		return false, errors.New("maintenance has no days of month specified")
	}

	// Parse start and end times from string format (HH:MM)
	startTime, err := time.Parse("15:04", *params.StartTime)
	if err != nil {
		return false, errors.New("invalid start time format")
	}

	endTime, err := time.Parse("15:04", *params.EndTime)
	if err != nil {
		return false, errors.New("invalid end time format")
	}

	// Check if today is a maintenance day (day of month)
	todayDay := now.Day()
	isMaintenanceDay := false
	for _, day := range params.DaysOfMonth {
		if day == todayDay {
			isMaintenanceDay = true
			break
		}
	}

	if !isMaintenanceDay {
		return false, nil
	}

	// Today is a maintenance day, now check if we're within the daily time window
	// Create today's start and end times in the maintenance timezone
	todayStart := time.Date(now.Year(), now.Month(), now.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, loc)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, loc)

	// Handle case where end time is next day (e.g., 23:00 - 01:00)
	if endTime.Before(startTime) {
		// For cross-day windows, we need to check two possible windows:
		// 1. Window starting today: todayStart to tomorrow's endTime
		todayEnd = todayEnd.Add(24 * time.Hour)
		if now.After(todayStart) && now.Before(todayEnd) {
			return true, nil
		}

		// 2. Window starting yesterday: yesterdayStart to today's endTime
		yesterdayStart := todayStart.Add(-24 * time.Hour)
		todayEndOriginal := time.Date(now.Year(), now.Month(), now.Day(),
			endTime.Hour(), endTime.Minute(), 0, 0, loc)
		if now.After(yesterdayStart) && now.Before(todayEndOriginal) {
			return true, nil
		}

		return false, nil
	}

	// Check if we're within the daily time window
	return now.After(todayStart) && now.Before(todayEnd), nil
}

// convertToTimezone constructs a time in the specified timezone from the components of another time
func (twc *TimeWindowChecker) convertToTimezone(inputTime string, loc *time.Location) time.Time {
	parsedTime, err := time.Parse("2006-01-02T15:04", inputTime)
	if err != nil {
		return time.Time{}
	}

	return time.Date(
		parsedTime.Year(), parsedTime.Month(), parsedTime.Day(),
		parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second(),
		parsedTime.Nanosecond(), loc,
	)
}
