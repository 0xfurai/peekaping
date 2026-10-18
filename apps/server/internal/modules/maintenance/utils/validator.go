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
