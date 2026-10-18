package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidator_ValidateCronAndDuration(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name        string
		params      *ValidationParams
		expectError bool
	}{
		{name: "no cron, no duration", params: &ValidationParams{}},
		{name: "empty cron, no duration", params: &ValidationParams{Cron: stringPtr("")}},
		{name: "no cron, any duration", params: &ValidationParams{Duration: intPtr(0)}},
		{name: "cron with positive duration", params: &ValidationParams{Cron: stringPtr("0 2 * * *"), Duration: intPtr(60)}},
		{name: "cron without duration", params: &ValidationParams{Cron: stringPtr("0 2 * * *")}, expectError: true},
		{name: "cron with zero duration", params: &ValidationParams{Cron: stringPtr("0 2 * * *"), Duration: intPtr(0)}, expectError: true},
		{name: "cron with negative duration", params: &ValidationParams{Cron: stringPtr("0 2 * * *"), Duration: intPtr(-5)}, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCronAndDuration(tt.params)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
