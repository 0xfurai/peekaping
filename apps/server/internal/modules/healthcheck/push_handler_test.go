package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParsePushStatus(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  shared.MonitorStatus
	}{
		{name: "up string", value: "up", want: shared.MonitorStatusUp},
		{name: "down string", value: "down", want: shared.MonitorStatusDown},
		{name: "case and whitespace insensitive", value: " UP ", want: shared.MonitorStatusUp},
		{name: "numeric up", value: "1", want: shared.MonitorStatusUp},
		{name: "numeric down", value: "0", want: shared.MonitorStatusDown},
		{name: "missing defaults to up", value: "", want: shared.MonitorStatusUp},
		{name: "failure word is down", value: "fail", want: shared.MonitorStatusDown},
		{name: "typo is down", value: "dwon", want: shared.MonitorStatusDown},
		{name: "maintenance number is down", value: "3", want: shared.MonitorStatusDown},
		{name: "out of range number is down", value: "-1", want: shared.MonitorStatusDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePushStatus(tt.value); got != tt.want {
				t.Errorf("parsePushStatus(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestParsePushPing(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   int
		wantOK bool
	}{
		{name: "integer", value: "500", want: 500, wantOK: true},
		{name: "decimal is rounded", value: "123.5", want: 124, wantOK: true},
		{name: "whitespace is trimmed", value: " 42 ", want: 42, wantOK: true},
		{name: "missing is zero", value: "", want: 0, wantOK: true},
		{name: "largest column value", value: "2147483647", want: 2147483647, wantOK: true},
		{name: "malformed is dropped", value: "unknown", want: 0, wantOK: false},
		{name: "negative is dropped", value: "-500", want: 0, wantOK: false},
		{name: "beyond the column is dropped", value: "3000000000", want: 0, wantOK: false},
		{name: "NaN is dropped", value: "NaN", want: 0, wantOK: false},
		{name: "infinity is dropped", value: "Inf", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePushPing(tt.value)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parsePushPing(%q) = (%d, %t), want (%d, %t)", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// pushMonitorService finds one active monitor by its push token.
type pushMonitorService struct {
	monitor.Service
	monitor *monitor.Model
}

func (s *pushMonitorService) FindOneByPushToken(_ context.Context, token string) (*monitor.Model, error) {
	if token != s.monitor.PushToken {
		return nil, nil
	}
	return s.monitor, nil
}

// capturingIngester records every result it is asked to ingest.
type capturingIngester struct {
	results []*ingester.Result
}

func (c *capturingIngester) Ingest(_ context.Context, r *ingester.Result) error {
	c.results = append(c.results, r)
	return nil
}

// noMaintenance is a maintenance service with no windows, so a pushed
// heartbeat keeps the status it reports.
type noMaintenance struct{ maintenance.Service }

func (noMaintenance) GetMaintenancesByMonitorID(context.Context, string) ([]*maintenance.Model, error) {
	return nil, nil
}

func TestPushEndpoint(t *testing.T) {

	push := func(t *testing.T, query string) *ingester.Result {
		t.Helper()
		monitors := &pushMonitorService{monitor: &monitor.Model{ID: "mon-1", Type: "push", Active: true, PushToken: "token"}}
		results := &capturingIngester{}
		router := httpx.NewRouter()
		RegisterPushEndpoint(router.Group("/api/v1"), monitors, nil, results, noMaintenance{}, zap.NewNop().Sugar())

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/token?"+query, nil))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, results.results, 1)
		return results.results[0]
	}

	t.Run("URL copied from the UI records an up beat", func(t *testing.T) {
		payload := push(t, "status=up&msg=OK&ping=")
		assert.Equal(t, shared.MonitorStatusUp, payload.Status)
		assert.Equal(t, "OK", payload.Message)
		assert.Equal(t, 0, payload.PingMs)
	})

	t.Run("forwards status, message and ping", func(t *testing.T) {
		payload := push(t, "status=down&msg=disk+full&ping=12.4")
		assert.Equal(t, shared.MonitorStatusDown, payload.Status)
		assert.Equal(t, "disk full", payload.Message)
		assert.Equal(t, 12, payload.PingMs)
	})

	t.Run("down beat without a message is not reported as OK", func(t *testing.T) {
		payload := push(t, "status=down")
		assert.Equal(t, "Down", payload.Message)
	})

	t.Run("invalid ping keeps the beat", func(t *testing.T) {
		payload := push(t, "status=up&ping=-5")
		assert.Equal(t, shared.MonitorStatusUp, payload.Status)
		assert.Equal(t, 0, payload.PingMs)
	})
}
