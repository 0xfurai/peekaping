package utils

import (
	"errors"
	"time"
)

// TimeUtils handles time-related operations like duration calculation and timezone handling
type TimeUtils struct{}

// NewTimeUtils creates a new time utils instance
func NewTimeUtils() *TimeUtils {
	return &TimeUtils{}
}

// CalculateDurationFromTimes calculates duration in minutes from start and end time strings
func (tu *TimeUtils) CalculateDurationFromTimes(startTime, endTime string) (int, error) {
	start, err := time.Parse("15:04", startTime)
	if err != nil {
		return 0, err
	}

	end, err := time.Parse("15:04", endTime)
	if err != nil {
		return 0, err
	}

	// Handle case where end time is on the next day (e.g., 23:00 to 01:00)
	if end.Before(start) {
		// Add 24 hours to end time to get the correct duration
		end = end.Add(24 * time.Hour)
	}

	duration := int(end.Sub(start).Minutes())

	// Validate that duration is positive and reasonable (not more than 24 hours)
	if duration <= 0 {
		return 0, errors.New("invalid time range: end time must be after start time")
	}

	if duration > 1440 { // 24 hours * 60 minutes
		return 0, errors.New("duration cannot exceed 24 hours")
	}

	return duration, nil
}

// LoadTimezone loads a timezone location, with fallback to UTC if invalid
func (tu *TimeUtils) LoadTimezone(timezone string) *time.Location {
	if timezone == "SAME_AS_SERVER" {
		timezone = time.Now().Location().String()
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		// Fallback to UTC if timezone is invalid
		return time.UTC
	}
	return loc
}

// GetDefaultTimezone returns the default timezone string
func (tu *TimeUtils) GetDefaultTimezone() string {
	return "UTC"
}

// ValidateTimeFormat validates if a time string is in HH:MM format
func (tu *TimeUtils) ValidateTimeFormat(timeStr string) error {
	_, err := time.Parse("15:04", timeStr)
	if err != nil {
		return errors.New("invalid time format, expected HH:MM")
	}
	return nil
}

// ParseTimeString parses a time string in HH:MM format and returns a time.Time
func (tu *TimeUtils) ParseTimeString(timeStr string) (time.Time, error) {
	return time.Parse("15:04", timeStr)
}

// IsCrossDayWindow checks if a time window crosses midnight (e.g., 23:00 - 01:00)
func (tu *TimeUtils) IsCrossDayWindow(startTime, endTime string) (bool, error) {
	start, err := time.Parse("15:04", startTime)
	if err != nil {
		return false, err
	}

	end, err := time.Parse("15:04", endTime)
	if err != nil {
		return false, err
	}

	return end.Before(start), nil
}
