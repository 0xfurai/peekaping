package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestTimeUtils_CalculateDurationFromTimes(t *testing.T) {
	utils := NewTimeUtils(zap.NewNop().Sugar())

	tests := []struct {
		name        string
		startTime   string
		endTime     string
		expected    int
		expectError bool
	}{
		{
			name:        "normal time range",
			startTime:   "09:00",
			endTime:     "17:00",
			expected:    480, // 8 hours * 60 minutes
			expectError: false,
		},
		{
			name:        "cross-day time range",
			startTime:   "23:00",
			endTime:     "01:00",
			expected:    120, // 2 hours * 60 minutes
			expectError: false,
		},
		{
			name:        "same time",
			startTime:   "12:00",
			endTime:     "12:00",
			expected:    0,
			expectError: true,
		},
		{
			name:        "invalid start time format",
			startTime:   "25:00",
			endTime:     "17:00",
			expected:    0,
			expectError: true,
		},
		{
			name:        "invalid end time format",
			startTime:   "09:00",
			endTime:     "25:00",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.CalculateDurationFromTimes(tt.startTime, tt.endTime)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTimeUtils_LoadTimezone(t *testing.T) {
	utils := NewTimeUtils(zap.NewNop().Sugar())

	tests := []struct {
		name     string
		timezone string
		expected string
	}{
		{
			name:     "valid timezone",
			timezone: "UTC",
			expected: "UTC",
		},
		{
			name:     "valid timezone with location",
			timezone: "America/New_York",
			expected: "America/New_York",
		},
		{
			name:     "same as server",
			timezone: "SAME_AS_SERVER",
			expected: time.Now().Location().String(),
		},
		{
			name:     "invalid timezone falls back to UTC",
			timezone: "Invalid/Timezone",
			expected: "UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := utils.LoadTimezone(tt.timezone)
			assert.Equal(t, tt.expected, loc.String())
		})
	}
}

// SAME_AS_SERVER is the zone the process runs in, so maintenance windows agree
// with logs and everything else on time.Local even if TZ changes later.
func TestTimeUtils_LoadTimezone_SameAsServerIsProcessZone(t *testing.T) {
	utils := NewTimeUtils(zap.NewNop().Sugar())

	_ = time.Local.String() // initialize time.Local before TZ changes
	t.Setenv("TZ", "Europe/Kyiv")

	assert.Same(t, time.Local, utils.LoadTimezone("SAME_AS_SERVER"))
}

func TestTimeUtils_LoadTimezone_WarnsWhenZoneFailsToLoad(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	utils := NewTimeUtils(zap.New(core).Sugar())

	loc := utils.LoadTimezone("Europe/Kiev2")

	assert.Equal(t, "UTC", loc.String())
	assert.Equal(t, 1, logs.FilterField(zap.String("timezone", "Europe/Kiev2")).Len())
}

func TestTimeUtils_ValidateTimeFormat(t *testing.T) {
	utils := NewTimeUtils(zap.NewNop().Sugar())

	tests := []struct {
		name        string
		timeStr     string
		expectError bool
	}{
		{
			name:        "valid time format",
			timeStr:     "14:30",
			expectError: false,
		},
		{
			name:        "valid time format with leading zeros",
			timeStr:     "09:05",
			expectError: false,
		},
		{
			name:        "invalid time format - hour too high",
			timeStr:     "25:00",
			expectError: true,
		},
		{
			name:        "invalid time format - minute too high",
			timeStr:     "12:60",
			expectError: true,
		},
		{
			name:        "invalid time format - wrong separator",
			timeStr:     "12.30",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidateTimeFormat(tt.timeStr)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTimeUtils_IsCrossDayWindow(t *testing.T) {
	utils := NewTimeUtils(zap.NewNop().Sugar())

	tests := []struct {
		name        string
		startTime   string
		endTime     string
		expected    bool
		expectError bool
	}{
		{
			name:        "normal day window",
			startTime:   "09:00",
			endTime:     "17:00",
			expected:    false,
			expectError: false,
		},
		{
			name:        "cross-day window",
			startTime:   "23:00",
			endTime:     "01:00",
			expected:    true,
			expectError: false,
		},
		{
			name:        "invalid time format",
			startTime:   "25:00",
			endTime:     "17:00",
			expected:    false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.IsCrossDayWindow(tt.startTime, tt.endTime)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
