package apitest

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeCheck is one check of a monitor, made by a probe.
type probeCheck struct {
	probeID string
	status  shared.MonitorStatus
	ping    int
	at      time.Time
	msg     string
}

// recordChecks stores each check as a heartbeat and counts it in the stats, as the ingester does.
func (a *app) recordChecks(t *testing.T, monitorID string, checks []probeCheck) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service, statService stats.Service) error {
		ctx := context.Background()
		for _, c := range checks {
			if _, err := heartbeats.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, ProbeID: c.probeID, Status: c.status, Ping: c.ping, Msg: c.msg, Time: c.at}); err != nil {
				return err
			}
			if err := statService.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{MonitorID: monitorID, ProbeID: c.probeID, Status: int(c.status), Ping: c.ping, Time: c.at.Unix()}); err != nil {
				return err
			}
		}
		return nil
	}))
}

type heartbeatResponse struct {
	ProbeID string `json:"probe_id"`
	Msg     string `json:"msg"`
}

func (a *app) heartbeatMessages(t *testing.T, query string) (msgs, probes []string) {
	t.Helper()
	list := decode[[]heartbeatResponse](t, a.request(t, http.MethodGet, query, nil, a.asAdmin), http.StatusOK)
	for _, hb := range list {
		msgs, probes = append(msgs, hb.Msg), append(probes, hb.ProbeID)
	}
	return msgs, probes
}

func TestMonitorsAPI_HeartbeatsCarryTheirProbeAndTakeAProbeFilter(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	a.recordChecks(t, id, []probeCheck{
		{probeID: "local", status: shared.MonitorStatusUp, at: at, msg: "local 1"},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusDown, at: at.Add(time.Minute), msg: "frankfurt 1"},
		{probeID: "", status: shared.MonitorStatusUp, at: at.Add(2 * time.Minute), msg: "unattributed"},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusUp, at: at.Add(3 * time.Minute), msg: "frankfurt 2"},
	})
	path := "/api/v1/monitors/" + id + "/heartbeats"

	msgs, probes := a.heartbeatMessages(t, path+"?reverse=true")
	assert.Equal(t, []string{"local 1", "frankfurt 1", "unattributed", "frankfurt 2"}, msgs, "without probe_id: every location")
	assert.Equal(t, []string{"local", "probe-frankfurt", "local", "probe-frankfurt"}, probes, "each heartbeat names its probe")

	msgs, probes = a.heartbeatMessages(t, path+"?reverse=true&probe_id=probe-frankfurt")
	assert.Equal(t, []string{"frankfurt 1", "frankfurt 2"}, msgs)
	assert.Equal(t, []string{"probe-frankfurt", "probe-frankfurt"}, probes)

	msgs, _ = a.heartbeatMessages(t, path+"?reverse=true&probe_id=local")
	assert.Equal(t, []string{"local 1", "unattributed"}, msgs, "a heartbeat with no probe came from the local probe")

	msgs, _ = a.heartbeatMessages(t, path+"?probe_id=probe-frankfurt&limit=1")
	assert.Equal(t, []string{"frankfurt 2"}, msgs, "with the other query parameters")

	msgs, _ = a.heartbeatMessages(t, path+"?probe_id=probe-oslo")
	assert.Empty(t, msgs, "a probe that ran no checks of the monitor")
}

type uptimeResponse struct {
	ProbeID    string  `json:"probe_id"`
	Uptime24h  float64 `json:"24h"`
	Uptime7d   float64 `json:"7d"`
	Uptime30d  float64 `json:"30d"`
	Uptime365d float64 `json:"365d"`
}

func TestMonitorsAPI_UptimeStatsTakeAProbeFilter(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	at := time.Now().UTC().Add(-30 * time.Minute)
	a.recordChecks(t, id, []probeCheck{
		{probeID: "local", status: shared.MonitorStatusUp, ping: 100, at: at},
		{probeID: "local", status: shared.MonitorStatusUp, ping: 100, at: at.Add(time.Second)},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusUp, ping: 300, at: at.Add(2 * time.Second)},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusDown, at: at.Add(3 * time.Second)},
	})
	path := "/api/v1/monitors/" + id + "/stats/uptime"

	all := decode[uptimeResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, uptimeResponse{Uptime24h: 75, Uptime7d: 75, Uptime30d: 75, Uptime365d: 75}, all, "without probe_id: every location's checks")

	local := decode[uptimeResponse](t, a.request(t, http.MethodGet, path+"?probe_id=local", nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, uptimeResponse{ProbeID: "local", Uptime24h: 100, Uptime7d: 100, Uptime30d: 100, Uptime365d: 100}, local)

	frankfurt := decode[uptimeResponse](t, a.request(t, http.MethodGet, path+"?probe_id=probe-frankfurt", nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, uptimeResponse{ProbeID: "probe-frankfurt", Uptime24h: 50, Uptime7d: 50, Uptime30d: 50, Uptime365d: 50}, frankfurt)

	oslo := decode[uptimeResponse](t, a.request(t, http.MethodGet, path+"?probe_id=probe-oslo", nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, uptimeResponse{ProbeID: "probe-oslo"}, oslo, "a probe that ran no checks of the monitor")
}

type statPointResponse struct {
	Up   int     `json:"up"`
	Down int     `json:"down"`
	Ping float64 `json:"ping"`
}

type statPointsResponse struct {
	ProbeID string              `json:"probe_id"`
	Points  []statPointResponse `json:"points"`
	AvgPing *float64            `json:"avgPing"`
	Uptime  *float64            `json:"uptime"`
}

func TestMonitorsAPI_StatPointsTakeAProbeFilter(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	minute := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	a.recordChecks(t, id, []probeCheck{
		{probeID: "local", status: shared.MonitorStatusUp, ping: 100, at: minute.Add(10 * time.Second)},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusUp, ping: 300, at: minute.Add(20 * time.Second)},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusDown, at: minute.Add(30 * time.Second)},
		{probeID: "probe-frankfurt", status: shared.MonitorStatusUp, ping: 500, at: minute.Add(time.Minute + 5*time.Second)},
	})
	path := "/api/v1/monitors/" + id + "/stats/points?granularity=minute&since=" +
		url.QueryEscape(minute.Format(time.RFC3339)) + "&until=" + url.QueryEscape(minute.Add(time.Minute).Format(time.RFC3339))
	pointsFor := func(probeID string) statPointsResponse {
		query := path
		if probeID != "" {
			query += "&probe_id=" + probeID
		}
		return decode[statPointsResponse](t, a.request(t, http.MethodGet, query, nil, a.asAdmin), http.StatusOK)
	}
	ptr := func(v float64) *float64 { return &v }

	assert.Equal(t, statPointsResponse{
		Points:  []statPointResponse{{Up: 2, Down: 1, Ping: 200}, {Up: 1, Ping: 500}},
		AvgPing: ptr(300),
		Uptime:  ptr(75),
	}, pointsFor(""), "without probe_id: every location's checks")

	assert.Equal(t, statPointsResponse{
		ProbeID: "local",
		Points:  []statPointResponse{{Up: 1, Ping: 100}, {}},
		AvgPing: ptr(100),
		Uptime:  ptr(100),
	}, pointsFor("local"))

	frankfurt := pointsFor("probe-frankfurt")
	require.NotNil(t, frankfurt.Uptime)
	assert.InDelta(t, 66.6667, *frankfurt.Uptime, 1e-4, "two of three checks were Up")
	frankfurt.Uptime = nil
	assert.Equal(t, statPointsResponse{
		ProbeID: "probe-frankfurt",
		Points:  []statPointResponse{{Up: 1, Down: 1, Ping: 300}, {Up: 1, Ping: 500}},
		AvgPing: ptr(400),
	}, frankfurt)

	assert.Equal(t, statPointsResponse{ProbeID: "probe-oslo", Points: []statPointResponse{{}, {}}}, pointsFor("probe-oslo"), "a probe that ran no checks of the monitor")
}
