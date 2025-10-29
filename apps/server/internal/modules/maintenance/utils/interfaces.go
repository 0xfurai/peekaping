package utils

import (
	"time"
)

// CronGeneratorInterface defines the interface for cron generation
type CronGeneratorInterface interface {
	GenerateCronExpression(strategy string, params *CronParams) (*string, error)
}

// TimeWindowCheckerInterface defines the interface for time window checking
type TimeWindowCheckerInterface interface {
	IsInDateTimePeriod(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error)
	IsInRecurringIntervalWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error)
	IsInCronMaintenanceWindow(params *TimeWindowParams, now time.Time, loc *time.Location) (bool, error)
}

// TimeUtilsInterface defines the interface for time utilities
type TimeUtilsInterface interface {
	CalculateDurationFromTimes(startTime, endTime string) (int, error)
	GetDefaultTimezone() string
	LoadTimezone(timezone string) *time.Location
	ValidateTimeFormat(timeStr string) error
	ParseTimeString(timeStr string) (time.Time, error)
	IsCrossDayWindow(startTime, endTime string) (bool, error)
}

// ValidatorInterface defines the interface for validation
type ValidatorInterface interface {
	ValidateCronAndDuration(params *ValidationParams) error
}
