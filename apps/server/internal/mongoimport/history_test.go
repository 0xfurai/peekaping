package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/stats"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

func checkHistory(t *testing.T, in *mongoimporttest.Install, db *bun.DB, report *mongoimport.Report) {
	ctx := context.Background()
	checkout := in.ID("monitor", "checkout")

	t.Run("heartbeats", func(t *testing.T) {
		sqlBeats := heartbeat.NewSQLRepository(db)
		for _, name := range mongoimporttest.HeartbeatNames {
			want := mongoimporttest.GetByID[mongoimporttest.HeartbeatDocument](t, in, "heartbeat", in.ID("heartbeat", name))
			got, err := sqlBeats.FindByID(ctx, uuidOf(t, want.ID.Hex()))
			require.NoError(t, err)
			require.NotNil(t, got, name)
			assert.Equal(t, uuidOf(t, checkout), got.MonitorID, name)
			assert.Equal(t, want.Status, int(got.Status), name)
			assert.Equal(t, want.Msg, got.Msg, name)
			assert.Equal(t, want.Ping, got.Ping, name)
			assert.Equal(t, want.Duration, got.Duration, name)
			assert.Equal(t, want.DownCount, got.DownCount, name)
			assert.Equal(t, want.Retries, got.Retries, name)
			assert.Equal(t, want.Important, got.Important, name)
			assert.Equal(t, want.Notified, got.Notified, name)
			if name == "no time" {
				// It was made when its ObjectID was
				assert.True(t, got.Time.Equal(want.ID.Timestamp()), "%s", got.Time)
			} else {
				assertSameTime(t, want.Time, got.Time, name)
			}
			// An end time that was never set is NULL, not the year 1
			if want.EndTime.IsZero() {
				assert.True(t, got.EndTime.IsZero(), "%s: %s", name, got.EndTime)
			} else {
				assertSameTime(t, want.EndTime, got.EndTime, name)
			}
		}
		// They are all the monitor's, and the one of a deleted monitor is not
		history, err := sqlBeats.FindByMonitorIDPaginated(ctx, uuidOf(t, checkout), "", 100, 0, nil, true)
		require.NoError(t, err)
		assert.Len(t, history, len(mongoimporttest.HeartbeatNames))
		all, err := sqlBeats.FindAll(ctx, 0, 100)
		require.NoError(t, err)
		assert.Len(t, all, len(mongoimporttest.HeartbeatNames))

		r := collectionReport(t, report, "heartbeat")
		assert.Equal(t, "heartbeats", r.Table)
		assert.Equal(t, 8, r.Read)
		assert.Equal(t, 7, r.Copied)
		assert.Equal(t, map[string]int{"monitor no longer exists": 1}, r.Skipped)
		assert.Equal(t, map[string]int{"time missing or zero: used the time the ObjectID was made": 1}, r.Fixed)
	})

	t.Run("stats", func(t *testing.T) {
		sqlStats := stats.NewService(stats.NewSQLRepository(db), zap.NewNop().Sugar())
		since, until := mongoimporttest.HistoryStart.Add(-time.Hour), mongoimporttest.HistoryStart.Add(48*time.Hour)

		// Every stat reads back through the stats service as the document of MongoDB says
		for _, period := range []struct {
			collection string
			period     stats.StatPeriod
		}{{"stat_minutely", stats.StatMinutely}, {"stat_hourly", stats.StatHourly}, {"stat_daily", stats.StatDaily}} {
			want := mongoimporttest.List[mongoimporttest.StatDocument](t, in, period.collection, bson.M{
				"monitor_id": mustObjectID(t, checkout), "timestamp": bson.M{"$gte": since, "$lte": until},
			}, bson.D{{Key: "timestamp", Value: 1}})
			got, err := sqlStats.FindStatsByMonitorIDAndTimeRange(ctx, uuidOf(t, checkout), "", since, until, period.period)
			require.NoError(t, err)
			require.NotEmpty(t, want, period.period)
			require.Len(t, got, len(want), period.period)
			for i := range want {
				assert.True(t, want[i].Timestamp.Equal(got[i].Timestamp), "%s %d: %s is not %s", period.period, i, got[i].Timestamp, want[i].Timestamp)
				assert.Equal(t, want[i].Up, got[i].Up, "%s %d", period.period, i)
				assert.Equal(t, want[i].Down, got[i].Down, "%s %d", period.period, i)
				assert.Equal(t, want[i].Maintenance, got[i].Maintenance, "%s %d", period.period, i)
				assert.Equal(t, want[i].PingCount(), got[i].PingCount, "%s %d", period.period, i)
				assert.InDelta(t, want[i].Ping, got[i].Ping, 1e-9, "%s %d", period.period, i)
				assert.Equal(t, want[i].PingMin, got[i].PingMin, "%s %d", period.period, i)
				assert.Equal(t, want[i].PingMax, got[i].PingMax, "%s %d", period.period, i)
			}
		}

		// And the numbers are the ones the checks add up to: in the first minute two Up checks of
		// 100 and 200 ms, a Down and a Maintenance one
		minute, err := sqlStats.FindStatsByMonitorIDAndTimeRange(ctx, uuidOf(t, checkout), "", mongoimporttest.HistoryStart, mongoimporttest.HistoryStart, stats.StatMinutely)
		require.NoError(t, err)
		require.Len(t, minute, 1)
		assert.Equal(t, 3, minute[0].Up)
		assert.Equal(t, 1, minute[0].Down)
		assert.Equal(t, 1, minute[0].Maintenance)
		assert.Equal(t, 2, minute[0].PingCount)
		assert.InDelta(t, 150, minute[0].Ping, 1e-9)
		assert.Equal(t, 100.0, minute[0].PingMin)
		assert.Equal(t, 200.0, minute[0].PingMax)
		// An hour of Maintenance checks alone has no response time
		maintenance, err := sqlStats.FindStatsByMonitorIDAndTimeRange(ctx, uuidOf(t, checkout), "", mongoimporttest.HistoryStart.Add(2*time.Hour), mongoimporttest.HistoryStart.Add(2*time.Hour), stats.StatHourly)
		require.NoError(t, err)
		require.Len(t, maintenance, 1)
		assert.Equal(t, 2, maintenance[0].Up)
		assert.Equal(t, 0, maintenance[0].Down)
		assert.Equal(t, 2, maintenance[0].Maintenance)
		assert.Zero(t, maintenance[0].PingCount)
		assert.Zero(t, maintenance[0].Ping)
		assert.Zero(t, maintenance[0].PingMin)
		assert.Zero(t, maintenance[0].PingMax)

		// Two documents for one minute add up
		nightly := uuidOf(t, in.ID("monitor", "nightly"))
		at := mongoimporttest.HistoryStart.Add(3 * time.Hour)
		merged, err := sqlStats.FindStatsByMonitorIDAndTimeRange(ctx, nightly, "", at, at, stats.StatMinutely)
		require.NoError(t, err)
		require.Len(t, merged, 1)
		assert.Equal(t, 5, merged[0].Up)
		assert.Equal(t, 1, merged[0].Down)
		assert.Equal(t, 1, merged[0].Maintenance)
		assert.Equal(t, 4, merged[0].PingCount)
		assert.InDelta(t, 80, merged[0].Ping, 1e-9)
		assert.Equal(t, 40.0, merged[0].PingMin)
		assert.Equal(t, 110.0, merged[0].PingMax)
		// More Maintenance than Up checks can't have response times
		odd, err := sqlStats.FindStatsByMonitorIDAndTimeRange(ctx, nightly, "", at.Add(time.Minute), at.Add(time.Minute), stats.StatMinutely)
		require.NoError(t, err)
		require.Len(t, odd, 1)
		assert.Zero(t, odd[0].PingCount)
		assert.Zero(t, odd[0].Ping)

		r := collectionReport(t, report, "stat_minutely")
		assert.Equal(t, "stat_minutely", r.Table)
		assert.Equal(t, 9, r.Read)
		assert.Equal(t, 7, r.Copied)
		assert.Equal(t, map[string]int{
			"monitor no longer exists": 1,
			"a document of the same monitor and time, merged into the one copied": 1,
		}, r.Skipped)
		assert.Equal(t, map[string]int{"maintenance is more than up: ping_count set to 0": 1}, r.Fixed)
		r = collectionReport(t, report, "stat_hourly")
		assert.Equal(t, 5, r.Read)
		assert.Equal(t, 4, r.Copied)
		assert.Equal(t, map[string]int{"no timestamp": 1}, r.Skipped)
		r = collectionReport(t, report, "stat_daily")
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 2, r.Copied)
	})

	t.Run("TLS information and notification history", func(t *testing.T) {
		tls, err := monitor_tls_info.NewSQLRepository(db).GetByMonitorID(ctx, uuidOf(t, checkout))
		require.NoError(t, err)
		require.NotNil(t, tls)
		assert.JSONEq(t, `{"valid":true,"certInfo":{"subject":"checkout.example.com"}}`, tls.InfoJSON)
		r := collectionReport(t, report, "monitor_tls_info")
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 1, r.Copied)
		assert.Equal(t, map[string]int{"monitor no longer exists": 1}, r.Skipped)

		history := notification_sent_history.NewSQLRepository(db)
		sent, err := history.GetByMonitorAndType(ctx, uuidOf(t, checkout), "certificate")
		require.NoError(t, err)
		var days []int
		for _, record := range sent {
			days = append(days, record.Days)
		}
		assert.ElementsMatch(t, []int{14, 7}, days)
		// The 7-day notification was sent, so it isn't sent again
		covered, err := history.CheckIfSent(ctx, "certificate", uuidOf(t, checkout), 7)
		require.NoError(t, err)
		assert.True(t, covered)
		r = collectionReport(t, report, "notification_sent_history")
		assert.Equal(t, 3, r.Read)
		assert.Equal(t, 2, r.Copied)
		assert.Equal(t, map[string]int{"monitor no longer exists": 1}, r.Skipped)
	})
}
