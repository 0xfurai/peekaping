package healthcheck

import (
	"context"
	"errors"
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

// pushLookup answers the push token lookup with a monitor or an error.
type pushLookup struct {
	monitor.Service
	monitor *monitor.Model
	err     error
}

func (s *pushLookup) FindOneByPushTokenAnyWorkspace(context.Context, string) (*monitor.Model, error) {
	return s.monitor, s.err
}

// pushMaintenance answers whether the monitor's maintenance windows are active, or fails to.
type pushMaintenance struct {
	maintenance.Service
	windows     []*maintenance.Model
	listErr     error
	active      bool
	activeErr   error
	asked       int
	askedWindow *maintenance.Model
}

func (m *pushMaintenance) GetMaintenancesByMonitorIDAnyWorkspace(context.Context, string) ([]*maintenance.Model, error) {
	return m.windows, m.listErr
}

func (m *pushMaintenance) IsUnderMaintenance(_ context.Context, window *maintenance.Model) (bool, error) {
	m.asked++
	m.askedWindow = window
	return m.active, m.activeErr
}

// failingIngester refuses every result.
type failingIngester struct{}

func (failingIngester) Ingest(context.Context, *ingester.Result) error {
	return errors.New("queue is down")
}

func TestPushEndpoint_Outcomes(t *testing.T) {
	active := func(edit func(*monitor.Model)) *monitor.Model {
		m := &monitor.Model{ID: "mon-1", Name: "Cron job", Type: "push", Active: true, PushToken: "token"}
		if edit != nil {
			edit(m)
		}
		return m
	}

	tests := []struct {
		name       string
		lookup     *pushLookup
		ingester   ingester.Service
		query      string
		wantCode   int
		wantBody   string
		wantStored bool
	}{
		{name: "stored", lookup: &pushLookup{monitor: active(nil)}, query: "status=up", wantCode: http.StatusOK, wantBody: `"ok":"true"`, wantStored: true},
		{name: "monitor lookup fails", lookup: &pushLookup{err: errors.New("database is down")}, query: "status=up", wantCode: http.StatusNotFound, wantBody: "Monitor not found for pushToken"},
		{name: "no monitor has the token", lookup: &pushLookup{}, query: "status=up", wantCode: http.StatusNotFound, wantBody: "Monitor not found for pushToken"},
		{
			name: "workspace suspended by the administrator", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.WorkspaceSuspended, m.WorkspaceSuspendedByAdmin = true, true })},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "The workspace of this monitor is suspended by the install's administrator",
		},
		{
			name: "workspace suspended until the owner verifies their email", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.WorkspaceSuspended = true })},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "The workspace of this monitor is suspended until its owner verifies their email address",
		},
		{
			name: "type turned off", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.TypeDisabled = true })},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "This monitor's type is turned off on this install",
		},
		{
			name: "past the workspace's limit", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.OverLimit = true })},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "This monitor is past its workspace's limit of active monitors",
		},
		{
			name: "monitor paused", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.Active = false })},
			query: "status=up", wantCode: http.StatusBadRequest, wantBody: "Monitor is not active",
		},
		{
			name: "suspension is reported before the other refusals", lookup: &pushLookup{monitor: active(func(m *monitor.Model) {
				m.WorkspaceSuspended, m.TypeDisabled, m.OverLimit, m.Active = true, true, true, false
			})},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "suspended until its owner verifies",
		},
		{
			name: "type turned off is reported before the limit and a paused monitor", lookup: &pushLookup{monitor: active(func(m *monitor.Model) {
				m.TypeDisabled, m.OverLimit, m.Active = true, true, false
			})},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "turned off",
		},
		{
			name: "the limit is reported before a paused monitor", lookup: &pushLookup{monitor: active(func(m *monitor.Model) { m.OverLimit, m.Active = true, false })},
			query: "status=up", wantCode: http.StatusForbidden, wantBody: "past its workspace's limit",
		},
		{
			name: "ingesting fails", lookup: &pushLookup{monitor: active(nil)}, ingester: failingIngester{},
			query: "status=up", wantCode: http.StatusInternalServerError, wantBody: "Failed to process push heartbeat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := &capturingIngester{}
			var ingest ingester.Service = results
			if tt.ingester != nil {
				ingest = tt.ingester
			}
			router := httpx.NewRouter()
			RegisterPushEndpoint(router.Group("/api/v1"), tt.lookup, nil, ingest, noMaintenance{}, zap.NewNop().Sugar())

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/token?"+tt.query, nil))

			assert.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tt.wantBody)
			assert.Equal(t, tt.wantStored, len(results.results) == 1)
		})
	}
}

func TestPushEndpoint_Maintenance(t *testing.T) {
	push := func(t *testing.T, windows *pushMaintenance, query string) *ingester.Result {
		t.Helper()
		lookup := &pushLookup{monitor: &monitor.Model{ID: "mon-1", Type: "push", Active: true, PushToken: "token"}}
		results := &capturingIngester{}
		router := httpx.NewRouter()
		RegisterPushEndpoint(router.Group("/api/v1"), lookup, nil, results, windows, zap.NewNop().Sugar())

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/push/token?"+query, nil))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, results.results, 1)
		return results.results[0]
	}
	window := &maintenance.Model{ID: "window-1"}

	t.Run("an active window records maintenance and no ping", func(t *testing.T) {
		windows := &pushMaintenance{windows: []*maintenance.Model{window}, active: true}

		result := push(t, windows, "status=down&msg=disk+full&ping=40")

		assert.Equal(t, shared.MonitorStatusMaintenance, result.Status)
		assert.Equal(t, "Monitor under maintenance", result.Message)
		assert.Equal(t, 0, result.PingMs)
		assert.Same(t, window, windows.askedWindow)
	})

	t.Run("a window that is not active leaves the push as it is", func(t *testing.T) {
		windows := &pushMaintenance{windows: []*maintenance.Model{window}}

		result := push(t, windows, "status=down&msg=disk+full&ping=40")

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "disk full", result.Message)
		assert.Equal(t, 40, result.PingMs)
	})

	t.Run("windows that can't be listed leave the push as it is", func(t *testing.T) {
		windows := &pushMaintenance{listErr: errors.New("database is down")}

		result := push(t, windows, "status=up&ping=7")

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Equal(t, "OK", result.Message)
		assert.Equal(t, 7, result.PingMs)
	})

	t.Run("a window that can't be read leaves the push as it is", func(t *testing.T) {
		windows := &pushMaintenance{windows: []*maintenance.Model{window}, activeErr: errors.New("bad schedule")}

		result := push(t, windows, "status=up&ping=7")

		assert.Equal(t, shared.MonitorStatusUp, result.Status)
		assert.Equal(t, 7, result.PingMs)
		assert.Equal(t, 1, windows.asked)
	})
}
