package utils

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func ptr[T any](v T) *T { return &v }

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
		StartDateTime: ptr(startDateTime),
		IntervalDay:   ptr(intervalDay),
		StartTime:     ptr(startTime),
		EndTime:       ptr(endTime),
	}
	return func(now time.Time) (bool, error) {
		return twc.IsInRecurringIntervalWindow(params, now, loc)
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
