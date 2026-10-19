package apitest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createHeartbeat records a Heartbeat the way a check does, which publishes it on the event bus,
// and returns its time in Unix seconds. The repository stamps a Heartbeat with the time it's created.
func (a *app) createHeartbeat(t *testing.T, monitorID string, status shared.MonitorStatus, pingMs int) int64 {
	t.Helper()
	var unixSeconds int64
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service) error {
		created, err := heartbeats.Create(context.Background(), &heartbeat.CreateUpdateDto{
			MonitorID: monitorID, Status: status, Ping: pingMs,
		})
		if err == nil {
			unixSeconds = created.Time.Unix()
		}
		return err
	}))
	return unixSeconds
}

// monitorSeries scrapes /metrics, which needs no credentials, and returns the Monitor series
// keyed by name and labels.
func (a *app) monitorSeries(t *testing.T) map[string]float64 {
	t.Helper()
	w := a.request(t, http.MethodGet, "/metrics", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return parseSeries(t, w.Body.String())
}

func parseSeries(t *testing.T, exposition string) map[string]float64 {
	t.Helper()
	parsed := map[string]float64{}
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, "peekaping_monitor_") {
			continue
		}
		cut := strings.LastIndex(line, " ")
		require.Positive(t, cut, line)
		value, err := strconv.ParseFloat(line[cut+1:], 64)
		require.NoError(t, err, line)
		parsed[line[:cut]] = value
	}
	return parsed
}

// eventuallySeries waits for the scrape to carry exactly the Monitor series in want, written as
// exposition lines. The collector hears of Heartbeats and Monitor changes from the event bus.
func (a *app) eventuallySeries(t *testing.T, want string) {
	t.Helper()
	wanted := parseSeries(t, want)
	if !assert.Eventually(t, func() bool {
		w := a.request(t, http.MethodGet, "/metrics", nil)
		return w.Code == http.StatusOK && assert.ObjectsAreEqual(wanted, parseSeries(t, w.Body.String()))
	}, 5*time.Second, 20*time.Millisecond) {
		require.Equal(t, wanted, a.monitorSeries(t))
	}
}

func TestMetrics_AreOffByDefault(t *testing.T) {
	a := newApp(t)

	w := a.request(t, http.MethodGet, "/metrics", nil)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	for _, r := range a.routes() {
		assert.NotEqual(t, "/metrics", r.path)
	}
}

func TestMetrics_ServeThePrometheusFormatWithoutCredentials(t *testing.T) {
	a := newApp(t, withMetrics)

	w := a.request(t, http.MethodGet, "/metrics", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain; version=0.0.4"), w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "# TYPE peekaping_build_info gauge")
	assert.Contains(t, w.Body.String(), "# TYPE go_goroutines gauge")
	assert.NotContains(t, a.request(t, http.MethodGet, "/openapi.json", nil).Body.String(), "/metrics")
}

func TestMetrics_AreNotReadableByAPageOnAnotherOrigin(t *testing.T) {
	a := newApp(t, withMetrics)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()

	a.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for name := range w.Header() {
		assert.False(t, strings.HasPrefix(name, "Access-Control-"), "unexpected header %s", name)
	}
}

func TestMetrics_FollowAMonitorThroughItsLife(t *testing.T) {
	a := newAppWithAdmin(t, withMetrics)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	path := "/api/v1/monitors/" + id
	labels := `monitor_id="` + id + `",monitor_name="Checkout",monitor_type="push"`

	// A Heartbeat that's Up brings the Monitor's status, time and Response time
	at := a.createHeartbeat(t, id, shared.MonitorStatusUp, 250)
	a.eventuallySeries(t, fmt.Sprintf(`
peekaping_monitor_status{%[1]s} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{%[1]s} %[2]d
peekaping_monitor_response_seconds{%[1]s} 0.25
`, labels, at))

	// Once it's Down there is no Response time
	at = a.createHeartbeat(t, id, shared.MonitorStatusDown, 0)
	a.eventuallySeries(t, fmt.Sprintf(`
peekaping_monitor_status{%[1]s} 0
peekaping_monitor_last_heartbeat_timestamp_seconds{%[1]s} %[2]d
`, labels, at))

	// Pausing removes the series, and the next Heartbeat after resuming brings them back
	a.pauseMonitor(t, id)
	a.eventuallySeries(t, "")
	decode[monitorResponse](t, a.request(t, http.MethodPatch, path, map[string]any{"active": true}, a.asAdmin), http.StatusOK)
	at = a.createHeartbeat(t, id, shared.MonitorStatusUp, 80)
	a.eventuallySeries(t, fmt.Sprintf(`
peekaping_monitor_status{%[1]s} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{%[1]s} %[2]d
peekaping_monitor_response_seconds{%[1]s} 0.08
`, labels, at))

	// Renaming moves the series to the new name with the same values
	decode[monitorResponse](t, a.request(t, http.MethodPatch, path, map[string]any{"name": "Checkout API"}, a.asAdmin), http.StatusOK)
	renamed := `monitor_id="` + id + `",monitor_name="Checkout API",monitor_type="push"`
	a.eventuallySeries(t, fmt.Sprintf(`
peekaping_monitor_status{%[1]s} 1
peekaping_monitor_last_heartbeat_timestamp_seconds{%[1]s} %[2]d
peekaping_monitor_response_seconds{%[1]s} 0.08
`, renamed, at))

	// Deleting removes them
	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	a.eventuallySeries(t, "")
}
