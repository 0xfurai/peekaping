package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCronGenerator_GenerateCronExpression(t *testing.T) {
	generator := NewCronGenerator()

	tests := []struct {
		name     string
		strategy string
		params   *CronParams
		expected *string
		hasError bool
	}{
		{
			name:     "recurring-weekday strategy",
			strategy: "recurring-weekday",
			params: &CronParams{
				StartTime: stringPtr("14:30"),
				Weekdays:  []int{1, 3, 5}, // Monday, Wednesday, Friday
			},
			expected: stringPtr("30 14 * * 1,3,5"),
			hasError: false,
		},
		{
			name:     "recurring-day-of-month strategy",
			strategy: "recurring-day-of-month",
			params: &CronParams{
				StartTime:   stringPtr("09:00"),
				DaysOfMonth: []int{1, 15},
			},
			expected: stringPtr("0 9 1,15 * *"),
			hasError: false,
		},
		{
			name:     "recurring-interval strategy",
			strategy: "recurring-interval",
			params: &CronParams{
				StartTime:   stringPtr("02:00"),
				IntervalDay: intPtr(3),
			},
			expected: stringPtr("0 2 * * *"),
			hasError: false,
		},
		{
			name:     "manual strategy",
			strategy: "manual",
			params:   &CronParams{},
			expected: nil,
			hasError: false,
		},
		{
			name:     "recurring-weekday missing start time",
			strategy: "recurring-weekday",
			params: &CronParams{
				Weekdays: []int{1, 3, 5},
			},
			expected: nil,
			hasError: true,
		},
		{
			name:     "recurring-weekday missing weekdays",
			strategy: "recurring-weekday",
			params: &CronParams{
				StartTime: stringPtr("14:30"),
			},
			expected: nil,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generator.GenerateCronExpression(tt.strategy, tt.params)

			if tt.hasError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				if tt.expected == nil {
					assert.Nil(t, result)
				} else {
					assert.Equal(t, *tt.expected, *result)
				}
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
