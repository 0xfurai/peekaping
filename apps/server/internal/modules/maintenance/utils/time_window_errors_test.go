package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// A recurring interval schedule that can't be checked says why, and says the first thing wrong with
// it, in the order the fields are read.
func TestRecurringInterval_SaysWhatIsWrongWithTheSchedule(t *testing.T) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	valid := func() *TimeWindowParams {
		return &TimeWindowParams{
			StartDateTime: stringPtr("2026-01-01T00:00"),
			IntervalDay:   intPtr(2),
			StartTime:     stringPtr("02:00"),
			EndTime:       stringPtr("04:00"),
		}
	}

	for _, tt := range []struct {
		name    string
		breakIt func(p *TimeWindowParams)
		want    string
	}{
		{"no start date-time", func(p *TimeWindowParams) { p.StartDateTime = nil }, "maintenance has no start date time"},
		{"no interval", func(p *TimeWindowParams) { p.IntervalDay = nil }, "maintenance has no valid interval day"},
		{"zero interval", func(p *TimeWindowParams) { p.IntervalDay = intPtr(0) }, "maintenance has no valid interval day"},
		{"negative interval", func(p *TimeWindowParams) { p.IntervalDay = intPtr(-3) }, "maintenance has no valid interval day"},
		{"no start time", func(p *TimeWindowParams) { p.StartTime = nil }, "maintenance has no start or end time for daily window"},
		{"no end time", func(p *TimeWindowParams) { p.EndTime = nil }, "maintenance has no start or end time for daily window"},
		{"bad start time", func(p *TimeWindowParams) { p.StartTime = stringPtr("25:00") }, "invalid start time format"},
		{"bad end time", func(p *TimeWindowParams) { p.EndTime = stringPtr("4am") }, "invalid end time format"},
		{"bad start date-time", func(p *TimeWindowParams) { p.StartDateTime = stringPtr("01/01/2026") }, "invalid start date time format"},
		{"no start date-time before anything else", func(p *TimeWindowParams) {
			p.StartDateTime, p.IntervalDay, p.StartTime = nil, nil, nil
		}, "maintenance has no start date time"},
		{"no interval before the times", func(p *TimeWindowParams) {
			p.IntervalDay, p.StartTime, p.EndTime = nil, nil, stringPtr("4am")
		}, "maintenance has no valid interval day"},
		{"missing times before bad ones", func(p *TimeWindowParams) {
			p.StartTime, p.EndTime = nil, stringPtr("4am")
		}, "maintenance has no start or end time for daily window"},
		{"bad start time before bad end time", func(p *TimeWindowParams) {
			p.StartTime, p.EndTime = stringPtr("25:00"), stringPtr("4am")
		}, "invalid start time format"},
		{"bad times before a bad start date-time", func(p *TimeWindowParams) {
			p.EndTime, p.StartDateTime = stringPtr("4am"), stringPtr("01/01/2026")
		}, "invalid end time format"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			params := valid()
			tt.breakIt(params)

			got, err := twc.IsInRecurringIntervalWindow(params, time.Date(2026, 1, 3, 3, 0, 0, 0, time.UTC), time.UTC)

			assert.False(t, got)
			assert.EqualError(t, err, tt.want)
		})
	}
}

// An interval of every day has a window each day, a window of one minute is open for that minute
// only, and now is read in the maintenance's own zone.
func TestRecurringInterval_EdgesOfTheSchedule(t *testing.T) {
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, time.UTC) }

	t.Run("every day", func(t *testing.T) {
		check := intervalWindow(time.UTC, "2026-01-01T00:00", 1, "10:00", "10:30")
		checkWindow(t, check, []windowCase{
			{day(1, 10, 15), true},
			{day(2, 10, 15), true},
			{day(9, 10, 29), true},
			{day(9, 10, 30), false},
			{day(9, 9, 59), false},
		})
	})

	t.Run("a window of equal start and end times is empty", func(t *testing.T) {
		check := intervalWindow(time.UTC, "2026-01-01T00:00", 1, "10:00", "10:00")
		checkWindow(t, check, []windowCase{
			{day(2, 10, 0), false},
			{day(2, 9, 59), false},
			{day(2, 10, 1), false},
		})
	})

	t.Run("before the first window", func(t *testing.T) {
		check := intervalWindow(time.UTC, "2026-01-05T00:00", 2, "10:00", "12:00")
		checkWindow(t, check, []windowCase{
			{day(3, 11, 0), false},
			{day(4, 11, 0), false},
			{day(5, 11, 0), true},
			{day(6, 11, 0), false},
			{day(7, 11, 0), true},
		})
	})

	t.Run("a window that crosses midnight, the day after the first", func(t *testing.T) {
		check := intervalWindow(time.UTC, "2026-01-05T00:00", 2, "22:00", "02:00")
		checkWindow(t, check, []windowCase{
			{day(5, 21, 59), false},
			{day(5, 22, 0), true},
			{day(6, 1, 59), true},
			{day(6, 2, 0), false},
			{day(6, 23, 0), false},
			{day(7, 23, 0), true},
		})
	})

	t.Run("now in another zone is read in the maintenance's", func(t *testing.T) {
		tokyo := mustLoad(t, "Asia/Tokyo")
		check := intervalWindow(tokyo, "2026-01-01T00:00", 1, "09:00", "10:00")
		// 00:30 UTC is 09:30 in Tokyo
		checkWindow(t, check, []windowCase{
			{day(2, 0, 30), true},
			{day(2, 1, 30), false},
			{day(1, 23, 59), false},
		})
	})
}
