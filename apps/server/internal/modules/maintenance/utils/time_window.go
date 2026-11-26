package utils

import (
	"errors"
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

	// now is within [start, end), like the recurring windows
	return !now.Before(startDateInTz) && now.Before(endDateInTz), nil
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

	// The schedule fires in now's location, so read now in the maintenance's.
	now = now.In(loc)
	duration := time.Duration(*params.Duration) * time.Minute
	searchStart := now.Add(-duration)

	lastRun := schedule.Next(searchStart)
	if lastRun.After(now) {
		// The next run is in the future, so no run happened within the window
		twc.logger.Debugf("lastRun is after now", now)
		return false, nil
	}

	endOfWindow := lastRun.Add(duration)

	// now is within [lastRun, lastRun+duration)
	return now.Before(endOfWindow), nil
}

// IsInRecurringIntervalWindow checks if the current time falls within a recurring interval maintenance window.
// The schedule starts with the first daily window at or after StartDateTime and repeats every IntervalDay
// calendar days in loc. A window that crosses midnight belongs to the day it starts.
func (twc *TimeWindowChecker) IsInRecurringIntervalWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error) {
	startTime, endTime, err := parseDailyWindow(params)
	if err != nil {
		return false, err
	}

	now = now.In(loc)

	// Days are counted on calendar dates, not elapsed hours, so a DST change
	// doesn't shift the schedule.
	startDateInTz := twc.convertToTimezone(*params.StartDateTime, loc)
	if startDateInTz.IsZero() {
		return false, errors.New("invalid start date time format")
	}
	firstDay := calendarDate(startDateInTz)
	if wallClock(firstDay, startTime, loc).Before(startDateInTz) {
		firstDay = firstDay.AddDate(0, 0, 1)
	}

	// Only windows that started today or yesterday can contain now.
	today := calendarDate(now)
	for _, day := range []time.Time{today, today.AddDate(0, 0, -1)} {
		if !isIntervalDay(day, firstDay, *params.IntervalDay) {
			continue
		}

		if inDailyWindow(now, day, startTime, endTime, loc) {
			return true, nil
		}
	}

	return false, nil
}

// parseDailyWindow checks the fields a recurring interval schedule needs and returns the start and
// end times of its daily window, parsed from their HH:MM format
func parseDailyWindow(params *TimeWindowParams) (startTime, endTime time.Time, err error) {
	// Validate required fields for recurring-interval strategy
	if params.StartDateTime == nil {
		return startTime, endTime, errors.New("maintenance has no start date time")
	}

	if params.IntervalDay == nil || *params.IntervalDay <= 0 {
		return startTime, endTime, errors.New("maintenance has no valid interval day")
	}

	if params.StartTime == nil || params.EndTime == nil {
		return startTime, endTime, errors.New("maintenance has no start or end time for daily window")
	}

	// Parse start and end times from string format (HH:MM)
	startTime, err = time.Parse("15:04", *params.StartTime)
	if err != nil {
		return startTime, endTime, errors.New("invalid start time format")
	}

	endTime, err = time.Parse("15:04", *params.EndTime)
	if err != nil {
		return startTime, endTime, errors.New("invalid end time format")
	}
	return startTime, endTime, nil
}

// isIntervalDay reports whether day is firstDay or a whole number of intervals after it
func isIntervalDay(day, firstDay time.Time, intervalDay int) bool {
	daysSinceFirst := int(day.Sub(firstDay) / (24 * time.Hour))
	return daysSinceFirst >= 0 && daysSinceFirst%intervalDay == 0
}

// inDailyWindow reports whether now is in the window that opens on day at startTime and closes at
// endTime, on the day after if that is earlier
func inDailyWindow(now, day, startTime, endTime time.Time, loc *time.Location) bool {
	crossesMidnight := endTime.Before(startTime)

	windowStart := wallClock(day, startTime, loc)
	windowEnd := wallClock(day, endTime, loc)
	if crossesMidnight {
		windowEnd = wallClock(day.AddDate(0, 0, 1), endTime, loc)
	}

	return !now.Before(windowStart) && now.Before(windowEnd)
}

// calendarDate returns t's date as midnight UTC, so dates subtract to whole days.
func calendarDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// wallClock returns the moment when the clock in loc shows clock's time on day's date.
func wallClock(day time.Time, clock time.Time, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
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
