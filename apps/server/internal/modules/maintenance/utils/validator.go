package utils

import "errors"

// Validator handles validation logic for maintenance operations
type Validator struct{}

// NewValidator creates a new validator instance
func NewValidator() *Validator {
	return &Validator{}
}

// ValidationParams contains the parameters needed for validation
type ValidationParams struct {
	Cron     *string
	Duration *int
	Strategy *string
}

// ValidateCronAndDuration validates that if cron is provided, duration is also required
func (v *Validator) ValidateCronAndDuration(params *ValidationParams) error {
	if params.Cron != nil && *params.Cron != "" {
		if params.Duration == nil || *params.Duration <= 0 {
			return errors.New("duration is required when cron expression is provided")
		}
	}
	return nil
}

// ValidateStrategy validates that the strategy is one of the supported values
func (v *Validator) ValidateStrategy(strategy string) error {
	validStrategies := map[string]bool{
		"manual":                 true,
		"single":                 true,
		"recurring-interval":     true,
		"recurring-weekday":      true,
		"recurring-day-of-month": true,
	}

	if !validStrategies[strategy] {
		return errors.New("invalid strategy: must be one of manual, single, recurring-interval, recurring-weekday, recurring-day-of-month")
	}

	return nil
}

// ValidateWeekdays validates that weekdays are within valid range (0-6)
func (v *Validator) ValidateWeekdays(weekdays []int) error {
	for _, weekday := range weekdays {
		if weekday < 0 || weekday > 6 {
			return errors.New("weekday must be between 0 (Sunday) and 6 (Saturday)")
		}
	}
	return nil
}

// ValidateDaysOfMonth validates that days of month are within valid range (1-31)
func (v *Validator) ValidateDaysOfMonth(daysOfMonth []int) error {
	for _, day := range daysOfMonth {
		if day < 1 || day > 31 {
			return errors.New("day of month must be between 1 and 31")
		}
	}
	return nil
}

// ValidateIntervalDay validates that interval day is positive
func (v *Validator) ValidateIntervalDay(intervalDay *int) error {
	if intervalDay != nil && *intervalDay <= 0 {
		return errors.New("interval day must be positive")
	}
	return nil
}

// ValidateDuration validates that duration is positive and reasonable
func (v *Validator) ValidateDuration(duration *int) error {
	if duration != nil {
		if *duration <= 0 {
			return errors.New("duration must be positive")
		}
		if *duration > 1440 { // 24 hours * 60 minutes
			return errors.New("duration cannot exceed 24 hours")
		}
	}
	return nil
}
