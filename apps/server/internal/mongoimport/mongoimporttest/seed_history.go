package mongoimporttest

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// HistoryStart is the first minute of the heartbeats and stats of the "checkout" monitor.
var HistoryStart = time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)

// HeartbeatNames are the names of the heartbeats of the "checkout" monitor, for ID.
var HeartbeatNames = []string{"up", "up again", "down", "maintenance", "pending", "late", "no time"}

// StatSample is a heartbeat as the stats see it, for the seed to aggregate.
type StatSample struct {
	After  time.Duration // after HistoryStart
	Status shared.MonitorStatus
	Ping   int
}

// StatSamples are the checks of the "checkout" monitor that the stats count. The first minute has
// two Up checks, a Down and a Maintenance one: up is 3, down is 1, maintenance is 1, and the
// average response time is 150 ms over the two Up checks. The third hour has only Maintenance checks.
var StatSamples = []StatSample{
	{0, shared.MonitorStatusUp, 100},
	{10 * time.Second, shared.MonitorStatusUp, 200},
	{20 * time.Second, shared.MonitorStatusDown, 0},
	{30 * time.Second, shared.MonitorStatusMaintenance, 0},
	{time.Minute, shared.MonitorStatusPending, 0},
	{time.Hour, shared.MonitorStatusUp, 50},
	{2 * time.Hour, shared.MonitorStatusMaintenance, 0},
	{2*time.Hour + time.Second, shared.MonitorStatusMaintenance, 0},
	{25 * time.Hour, shared.MonitorStatusUp, 300},
}

func (in *Install) seedHistory(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	checkout, nightly := in.ID("monitor", "checkout"), in.ID("monitor", "nightly")
	missing := primitive.NewObjectID() // a monitor that was deleted

	// Heartbeats: the first and the sixth have an end time, and the others have a zero one
	heartbeats := heartbeat.NewMongoRepository(in.Client, in.Config)
	for i, h := range []heartbeat.Model{
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 120, Duration: 60, Important: true, Notified: true, Time: HistoryStart, EndTime: HistoryStart.Add(time.Minute)},
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 80, Duration: 60, Time: HistoryStart.Add(time.Minute)},
		{Status: shared.MonitorStatusDown, Msg: "connection refused", Duration: 60, DownCount: 3, Retries: 3, Important: true, Time: HistoryStart.Add(2 * time.Minute)},
		{Status: shared.MonitorStatusMaintenance, Msg: "In maintenance", Time: HistoryStart.Add(3 * time.Minute)},
		{Status: shared.MonitorStatusPending, Msg: "timeout", Retries: 1, Time: HistoryStart.Add(4 * time.Minute)},
		{Status: shared.MonitorStatusUp, Msg: "200 - OK", Ping: 95, Duration: 3600, Time: HistoryStart.Add(25 * time.Hour), EndTime: HistoryStart.Add(26 * time.Hour)},
	} {
		h.MonitorID = checkout
		created, err := heartbeats.Create(ctx, &h)
		require.NoError(t, err)
		in.remember("heartbeat", HeartbeatNames[i], created.ID)
	}
	// One that never got a time
	in.remember("heartbeat", HeartbeatNames[len(HeartbeatNames)-1], in.insert(t, "heartbeat", bson.D{
		{Key: "monitor_id", Value: mustObjectID(t, checkout)}, {Key: "status", Value: 1}, {Key: "msg", Value: "200 - OK"}, {Key: "ping", Value: 70},
		{Key: "duration", Value: 0}, {Key: "down_count", Value: 0}, {Key: "retries", Value: 0}, {Key: "important", Value: false}, {Key: "notified", Value: false},
	}).Hex())
	// One of a monitor that was deleted, which MongoDB doesn't cascade to
	in.insert(t, "heartbeat", bson.D{
		{Key: "monitor_id", Value: missing}, {Key: "status", Value: 1}, {Key: "msg", Value: "ok"}, {Key: "ping", Value: 1}, {Key: "time", Value: HistoryStart},
	})

	// Stats, aggregated by the stats service as a running install does
	stat := stats.NewService(stats.NewMongoRepository(in.Client, in.Config), in.Logger)
	for _, sample := range StatSamples {
		require.NoError(t, stat.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{
			MonitorID: checkout, Status: int(sample.Status), Ping: sample.Ping, Time: HistoryStart.Add(sample.After).Unix(),
		}))
	}
	// Two documents for one minute of the "nightly" monitor, which a race between the first two heartbeats of a
	// bucket could leave. The counts are 2 + 3 up of which 1 maintenance, 1 down, and 2 + 2 response times
	// averaging 100 and 60 ms, so the minute is 5 up, 1 down, 1 maintenance and an average of 80 ms over 4 checks.
	minute := HistoryStart.Add(3 * time.Hour)
	statDocument := func(monitorID primitive.ObjectID, at time.Time, up, down, maintenance int, ping, min, max float64) bson.D {
		return bson.D{
			{Key: "monitor_id", Value: monitorID}, {Key: "timestamp", Value: at}, {Key: "up", Value: up}, {Key: "down", Value: down},
			{Key: "maintenance", Value: maintenance}, {Key: "ping", Value: ping}, {Key: "ping_min", Value: min}, {Key: "ping_max", Value: max},
		}
	}
	nightlyID := mustObjectID(t, nightly)
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute, 2, 1, 0, 100, 90, 110))
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute, 3, 0, 1, 60, 40, 90))
	// More maintenance checks than up ones, which can't be: the response times count nothing
	in.insert(t, "stat_minutely", statDocument(nightlyID, minute.Add(time.Minute), 1, 0, 2, 0, 0, 0))
	// Of a monitor that was deleted, and one without a time
	in.insert(t, "stat_minutely", statDocument(missing, minute, 1, 0, 0, 10, 10, 10))
	in.insert(t, "stat_hourly", bson.D{{Key: "monitor_id", Value: nightlyID}, {Key: "up", Value: 1}, {Key: "down", Value: 0}, {Key: "maintenance", Value: 0}, {Key: "ping", Value: 5}})

	// TLS information and certificate expiry notifications, which refer to the monitor by a string
	tls := monitor_tls_info.NewMongoRepository(in.Client, in.Config)
	_, err := tls.Create(ctx, &monitor_tls_info.Model{MonitorID: checkout, InfoJSON: `{"valid":true,"certInfo":{"subject":"checkout.example.com"}}`})
	require.NoError(t, err)
	_, err = tls.Create(ctx, &monitor_tls_info.Model{MonitorID: missing.Hex(), InfoJSON: `{"valid":false}`})
	require.NoError(t, err)
	sent := notification_sent_history.NewMongoRepository(in.Client, in.Config)
	for _, record := range []notification_sent_history.Model{{Type: "certificate", MonitorID: checkout, Days: 14}, {Type: "certificate", MonitorID: checkout, Days: 7}, {Type: "certificate", MonitorID: missing.Hex(), Days: 7}} {
		require.NoError(t, sent.RecordSent(ctx, &record))
	}
}
