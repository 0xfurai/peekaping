package stats_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketStarts returns the start of every stat row the monitor has for period.
func bucketStarts(t *testing.T, svc stats.Service, monitorID string, period stats.StatPeriod, now time.Time) []time.Time {
	t.Helper()
	rows, err := svc.FindStatsByMonitorIDAndTimeRange(context.Background(), monitorID, now.AddDate(-5, 0, 0), now, period)
	require.NoError(t, err)
	starts := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		starts = append(starts, row.Timestamp.UTC())
	}
	return starts
}

func TestDeleteExpiredKeepsEachPeriodForItsRetention(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc stats.Service, monitorID string) {
		now := time.Date(2026, 10, 18, 15, 30, 20, 0, time.UTC)
		checks := []time.Time{
			now.Add(-48 * time.Hour),                // minute bucket 15:30 on Oct 16: the oldest one kept
			now.Add(-48*time.Hour - time.Minute),    // minute bucket 15:29 on Oct 16: expired
			now.AddDate(0, 0, -90),                  // hour bucket 15:00 on Jul 20: the oldest one kept
			now.AddDate(0, 0, -90).Add(-time.Hour),  // hour bucket 14:00 on Jul 20: expired
			now.AddDate(-2, 0, 0),                   // day bucket Oct 18, 2024: kept forever
		}
		for _, at := range checks {
			aggregate(t, svc, monitorID, shared.MonitorStatusUp, 100, at)
		}

		deleted, err := svc.DeleteExpired(context.Background(), now)
		require.NoError(t, err)

		day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
		assert.Equal(t, []time.Time{
			time.Date(2026, 10, 16, 15, 30, 0, 0, time.UTC),
		}, bucketStarts(t, svc, monitorID, stats.StatMinutely, now))
		assert.Equal(t, []time.Time{
			time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC),
		}, bucketStarts(t, svc, monitorID, stats.StatHourly, now))
		assert.Equal(t, []time.Time{
			day(2024, 10, 18), day(2026, 7, 20), day(2026, 10, 16),
		}, bucketStarts(t, svc, monitorID, stats.StatDaily, now))
		assert.Equal(t, map[stats.StatPeriod]int64{stats.StatMinutely: 4, stats.StatHourly: 2}, deleted)
	})
}
