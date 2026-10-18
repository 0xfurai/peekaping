package utils

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

// windowCase is one moment and whether it falls inside the maintenance window.
type windowCase struct {
	at   time.Time
	want bool
}

func (c windowCase) String() string {
	return fmt.Sprintf("%s want %t", c.at.Format("Mon 2006-01-02 15:04 MST"), c.want)
}

func checkWindow(t *testing.T, check func(time.Time) (bool, error), cases []windowCase) {
	t.Helper()
	for _, c := range cases {
		got, err := check(c.at)
		require.NoError(t, err, c.String())
		assert.Equal(t, c.want, got, c.String())
	}
}

func intervalWindow(loc *time.Location, startDateTime string, intervalDay int, startTime, endTime string) func(time.Time) (bool, error) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	params := &TimeWindowParams{
		StartDateTime: stringPtr(startDateTime),
		IntervalDay:   intPtr(intervalDay),
		StartTime:     stringPtr(startTime),
		EndTime:       stringPtr(endTime),
	}
	return func(now time.Time) (bool, error) {
		return twc.IsInRecurringIntervalWindow(params, now, loc)
	}
}

// cronWindow builds the window the service checks for the cron, weekday and
// day-of-month strategies: the generated cron, with the duration taken from
// the start and end times.
func cronWindow(t *testing.T, loc *time.Location, strategy string, params *CronParams) func(time.Time) (bool, error) {
	t.Helper()
	cronExpr, err := NewCronGenerator().GenerateCronExpression(strategy, params)
	require.NoError(t, err)
	duration, err := NewTimeUtils(zap.NewNop().Sugar()).CalculateDurationFromTimes(*params.StartTime, *params.EndTime)
	require.NoError(t, err)

	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	window := &TimeWindowParams{Cron: cronExpr, Duration: &duration}
	return func(now time.Time) (bool, error) {
		return twc.IsInCronMaintenanceWindow(window, now.In(loc), loc)
	}
}

func dateTimeWindow(loc *time.Location, startDateTime, endDateTime string) func(time.Time) (bool, error) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	params := &TimeWindowParams{StartDateTime: stringPtr(startDateTime), EndDateTime: stringPtr(endDateTime)}
	return func(now time.Time) (bool, error) {
		return twc.IsInDateTimePeriod(params, now.In(loc), loc)
	}
}

func TestDateTimePeriod_IsReadInTheMaintenanceTimeZone(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	check := dateTimeWindow(tokyo, "2026-05-10T02:00", "2026-05-10T04:00")

	checkWindow(t, check, []windowCase{
		{time.Date(2026, 5, 10, 1, 59, 0, 0, tokyo), false},
		{time.Date(2026, 5, 10, 2, 0, 0, 0, tokyo), true},
		{time.Date(2026, 5, 10, 3, 0, 0, 0, tokyo), true},
		{time.Date(2026, 5, 10, 4, 0, 0, 0, tokyo), false},
		{time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC), false}, // 12:00 in Tokyo
		{time.Date(2026, 5, 9, 18, 0, 0, 0, time.UTC), true},  // 03:00 in Tokyo
	})
}

func TestDateTimePeriod_RequiresBothEnds(t *testing.T) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())

	_, err := twc.IsInDateTimePeriod(&TimeWindowParams{StartDateTime: stringPtr("2026-05-10T02:00")}, time.Now(), time.UTC)
	assert.Error(t, err)
	_, err = twc.IsInDateTimePeriod(&TimeWindowParams{EndDateTime: stringPtr("2026-05-10T02:00")}, time.Now(), time.UTC)
	assert.Error(t, err)
}

// "Same as Server Timezone" reads the window in the zone the process runs in.
func TestDateTimePeriod_SameAsServerUsesTheProcessZone(t *testing.T) {
	kyiv := mustLoad(t, "Europe/Kyiv")
	original := time.Local
	time.Local = kyiv
	t.Cleanup(func() { time.Local = original })

	loc := NewTimeUtils(zap.NewNop().Sugar()).LoadTimezone("SAME_AS_SERVER")
	check := dateTimeWindow(loc, "2026-05-10T02:00", "2026-05-10T04:00")

	checkWindow(t, check, []windowCase{
		{time.Date(2026, 5, 10, 3, 0, 0, 0, kyiv), true},
		{time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC), false}, // 06:00 in Kyiv
	})
}

func TestCronWindow_CustomCronInTheMaintenanceTimeZone(t *testing.T) {
	newYork := mustLoad(t, "America/New_York")
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	params := &TimeWindowParams{Cron: stringPtr("0 22 * * *"), Duration: intPtr(180)}
	check := func(now time.Time) (bool, error) {
		return twc.IsInCronMaintenanceWindow(params, now.In(newYork), newYork)
	}

	checkWindow(t, check, []windowCase{
		{time.Date(2026, 6, 1, 21, 59, 0, 0, newYork), false},
		{time.Date(2026, 6, 1, 22, 0, 0, 0, newYork), true},
		{time.Date(2026, 6, 2, 0, 59, 0, 0, newYork), true},
		{time.Date(2026, 6, 2, 1, 0, 0, 0, newYork), false},
		{time.Date(2026, 6, 1, 22, 30, 0, 0, time.UTC), false}, // 18:30 in New York
	})

	t.Run("now in another zone is read in the maintenance zone", func(t *testing.T) {
		inWindow := time.Date(2026, 6, 1, 22, 30, 0, 0, newYork).UTC()
		got, err := twc.IsInCronMaintenanceWindow(params, inWindow, newYork)
		require.NoError(t, err)
		assert.True(t, got)
	})
}

func TestCronWindow_WeekdayWindowCrossingMidnightBelongsToItsStartDay(t *testing.T) {
	tokyo := mustLoad(t, "Asia/Tokyo")
	check := cronWindow(t, tokyo, "recurring-weekday", &CronParams{
		StartTime: stringPtr("23:00"),
		EndTime:   stringPtr("01:00"),
		Weekdays:  []int{1}, // Monday
	})
	// 2026-01-05 is a Monday.
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, tokyo) }

	checkWindow(t, check, []windowCase{
		{day(5, 0, 30), false}, // the tail of Sunday
		{day(5, 22, 59), false},
		{day(5, 23, 0), true},
		{day(6, 0, 30), true},
		{day(6, 1, 0), false},
		{day(6, 23, 30), false},
		{time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC), false}, // Tuesday 08:30 in Tokyo
	})
}

func TestCronWindow_DayOfMonthWindowCrossingMidnight(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	check := cronWindow(t, berlin, "recurring-day-of-month", &CronParams{
		StartTime:   stringPtr("22:00"),
		EndTime:     stringPtr("02:00"),
		DaysOfMonth: []int{1, 15},
	})

	checkWindow(t, check, []windowCase{
		{time.Date(2026, 1, 15, 23, 0, 0, 0, berlin), true},
		{time.Date(2026, 1, 16, 1, 59, 0, 0, berlin), true},
		{time.Date(2026, 1, 16, 2, 0, 0, 0, berlin), false},
		{time.Date(2026, 1, 16, 23, 0, 0, 0, berlin), false},
		{time.Date(2026, 1, 31, 23, 0, 0, 0, berlin), false},
		{time.Date(2026, 2, 2, 1, 0, 0, 0, berlin), true}, // the tail of Feb 1
	})
}

func TestCronWindow_FollowsLocalTimeAcrossDST(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	check := cronWindow(t, berlin, "recurring-weekday", &CronParams{
		StartTime: stringPtr("02:30"),
		EndTime:   stringPtr("03:30"),
		Weekdays:  []int{0}, // Sunday
	})

	t.Run("the Sunday after spring forward", func(t *testing.T) {
		checkWindow(t, check, []windowCase{
			{time.Date(2026, 4, 5, 2, 29, 0, 0, berlin), false},
			{time.Date(2026, 4, 5, 2, 45, 0, 0, berlin), true},
			{time.Date(2026, 4, 5, 3, 30, 0, 0, berlin), false},
		})
	})

	t.Run("fall back covers both passes through 02:30-03:30", func(t *testing.T) {
		// On 2026-10-25 clocks go back from 03:00 CEST to 02:00 CET.
		checkWindow(t, check, []windowCase{
			{time.Date(2026, 10, 25, 0, 45, 0, 0, time.UTC), true},  // 02:45 CEST
			{time.Date(2026, 10, 25, 1, 45, 0, 0, time.UTC), true},  // 02:45 CET
			{time.Date(2026, 10, 25, 2, 15, 0, 0, time.UTC), true},  // 03:15 CET
			{time.Date(2026, 10, 25, 2, 30, 0, 0, time.UTC), false}, // 03:30 CET
			{time.Date(2026, 11, 1, 2, 45, 0, 0, berlin), true},
		})
	})
}

func TestCronWindow_IncompleteOrInvalidCron(t *testing.T) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	now := time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC)

	for name, params := range map[string]*TimeWindowParams{
		"no cron":       {Duration: intPtr(60)},
		"empty cron":    {Cron: stringPtr(""), Duration: intPtr(60)},
		"no duration":   {Cron: stringPtr("0 23 * * *")},
		"zero duration": {Cron: stringPtr("0 23 * * *"), Duration: intPtr(0)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := twc.IsInCronMaintenanceWindow(params, now, time.UTC)
			require.NoError(t, err)
			assert.False(t, got)
		})
	}

	t.Run("unparseable cron", func(t *testing.T) {
		_, err := twc.IsInCronMaintenanceWindow(&TimeWindowParams{Cron: stringPtr("every day"), Duration: intPtr(60)}, now, time.UTC)
		assert.Error(t, err)
	})
}

func TestRecurringInterval_RejectsIncompleteSchedules(t *testing.T) {
	twc := NewTimeWindowChecker(zap.NewNop().Sugar())
	valid := func() *TimeWindowParams {
		return &TimeWindowParams{
			StartDateTime: stringPtr("2026-01-01T00:00"),
			IntervalDay:   intPtr(2),
			StartTime:     stringPtr("02:00"),
			EndTime:       stringPtr("04:00"),
		}
	}

	for name, breakIt := range map[string]func(p *TimeWindowParams){
		"no start date-time":  func(p *TimeWindowParams) { p.StartDateTime = nil },
		"bad start date-time": func(p *TimeWindowParams) { p.StartDateTime = stringPtr("01/01/2026") },
		"no interval":         func(p *TimeWindowParams) { p.IntervalDay = nil },
		"zero interval":       func(p *TimeWindowParams) { p.IntervalDay = intPtr(0) },
		"no start time":       func(p *TimeWindowParams) { p.StartTime = nil },
		"no end time":         func(p *TimeWindowParams) { p.EndTime = nil },
		"bad start time":      func(p *TimeWindowParams) { p.StartTime = stringPtr("25:00") },
		"bad end time":        func(p *TimeWindowParams) { p.EndTime = stringPtr("4am") },
	} {
		t.Run(name, func(t *testing.T) {
			params := valid()
			breakIt(params)
			_, err := twc.IsInRecurringIntervalWindow(params, time.Date(2026, 1, 3, 3, 0, 0, 0, time.UTC), time.UTC)
			assert.Error(t, err)
		})
	}
}

func TestRecurringInterval_WindowCrossingMidnightBelongsToItsStartDay(t *testing.T) {
	// Every 2 days from Jan 1, 23:00 to 01:00: the windows open on Jan 1, 3 and 5.
	check := intervalWindow(time.UTC, "2026-01-01T00:00", 2, "23:00", "01:00")
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, time.UTC) }

	checkWindow(t, check, []windowCase{
		{day(1, 0, 30), false}, // the tail of Dec 31, which is not a maintenance day
		{day(1, 22, 59), false},
		{day(1, 23, 0), true},
		{day(2, 0, 30), true}, // the tail of Jan 1's window
		{day(2, 1, 0), false},
		{day(2, 23, 30), false},
		{day(3, 0, 30), false}, // the tail of Jan 2, not a maintenance day
		{day(3, 23, 30), true},
		{day(4, 0, 59), true},
	})
}

func TestRecurringInterval_KeepsDayAndLocalTimeAcrossDST(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")

	t.Run("spring forward", func(t *testing.T) {
		// Clocks jump from 02:00 to 03:00 on Sunday 2026-03-29.
		check := intervalWindow(berlin, "2026-03-01T00:00", 2, "00:00", "02:00")
		checkWindow(t, check, []windowCase{
			{time.Date(2026, 3, 29, 0, 30, 0, 0, berlin), true},
			{time.Date(2026, 3, 30, 0, 30, 0, 0, berlin), false},
			{time.Date(2026, 3, 31, 0, 30, 0, 0, berlin), true},
			{time.Date(2026, 3, 31, 1, 59, 0, 0, berlin), true},
			{time.Date(2026, 3, 31, 2, 0, 0, 0, berlin), false},
		})
	})

	t.Run("window spanning the spring-forward jump keeps its wall-clock end", func(t *testing.T) {
		check := intervalWindow(berlin, "2026-03-01T00:00", 2, "01:00", "04:00")
		checkWindow(t, check, []windowCase{
			{time.Date(2026, 3, 29, 1, 30, 0, 0, berlin), true},
			{time.Date(2026, 3, 29, 3, 30, 0, 0, berlin), true},
			{time.Date(2026, 3, 29, 4, 0, 0, 0, berlin), false},
		})
	})

	t.Run("fall back", func(t *testing.T) {
		// Clocks go back from 03:00 to 02:00 on Sunday 2026-10-25.
		check := intervalWindow(berlin, "2026-10-01T00:00", 2, "23:00", "23:59")
		checkWindow(t, check, []windowCase{
			{time.Date(2026, 10, 25, 23, 30, 0, 0, berlin), true},
			{time.Date(2026, 10, 26, 23, 30, 0, 0, berlin), false},
			{time.Date(2026, 10, 27, 23, 30, 0, 0, berlin), true},
		})
	})
}

func TestRecurringInterval_StartsAtFirstWindowAfterStartDateTime(t *testing.T) {
	// Created at 14:37 with a 14:00-16:00 window every 2 days. Jan 1's window
	// opened before the start, so the schedule runs Jan 2, 4, 6...
	check := intervalWindow(time.UTC, "2026-01-01T14:37", 2, "14:00", "16:00")
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, time.UTC) }

	checkWindow(t, check, []windowCase{
		{day(1, 15, 0), false},
		{day(2, 14, 0), true},
		{day(2, 14, 20), true},
		{day(2, 15, 30), true}, // past the start's time of day, same window
		{day(3, 15, 0), false},
		{day(4, 14, 20), true},
		{day(4, 15, 0), true},
		{day(4, 16, 0), false},
	})
}

func TestRecurringInterval_StartBeforeTheDailyWindowIncludesTheStartDay(t *testing.T) {
	check := intervalWindow(time.UTC, "2026-01-01T01:00", 3, "02:00", "04:00")
	day := func(d, h, m int) time.Time { return time.Date(2026, 1, d, h, m, 0, 0, time.UTC) }

	checkWindow(t, check, []windowCase{
		{day(1, 2, 30), true},
		{day(2, 2, 30), false},
		{day(3, 2, 30), false},
		{day(4, 2, 30), true},
		{day(7, 3, 59), true},
	})
}
