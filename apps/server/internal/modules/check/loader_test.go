package check

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const monitorID = "mon-1"

// fakeMonitorService finds one monitor
type fakeMonitorService struct {
	mon *monitor.Model
	err error
}

func (s *fakeMonitorService) FindByIDAnyWorkspace(_ context.Context, id string) (*monitor.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.mon == nil || s.mon.ID != id {
		return nil, nil
	}
	return s.mon, nil
}

// fakeProxyService finds proxies by ID
type fakeProxyService struct {
	proxies map[string]*proxy.Model
	err     error
}

func (s *fakeProxyService) FindByID(_ context.Context, _ workspace.ID, id string) (*proxy.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.proxies[id], nil
}

// fakeMaintenanceService has maintenance windows for the monitor, some of them active
type fakeMaintenanceService struct {
	windows []*maintenance.Model
	active  map[string]bool
	listErr error
	// statusErr lists the windows whose status can't be read
	statusErr map[string]bool
}

func (s *fakeMaintenanceService) GetMaintenancesByMonitorIDAnyWorkspace(_ context.Context, _ string) ([]*maintenance.Model, error) {
	return s.windows, s.listErr
}

func (s *fakeMaintenanceService) IsUnderMaintenance(_ context.Context, m *maintenance.Model) (bool, error) {
	if s.statusErr[m.ID] {
		return false, errors.New("bad schedule")
	}
	return s.active[m.ID], nil
}

// fakeHeartbeatService returns a monitor's latest heartbeat, and fails the test if asked
// for a monitor that isn't expected to read one
type fakeHeartbeatService struct {
	t      *testing.T
	latest []*heartbeat.Model
	err    error
	reads  int
}

func (s *fakeHeartbeatService) FindByMonitorIDPaginated(_ context.Context, id string, limit, page int, important *bool, reverse bool) ([]*heartbeat.Model, error) {
	s.reads++
	// The newest heartbeat: one per page, first page, newest first
	assert.Equal(s.t, monitorID, id)
	assert.Equal(s.t, 1, limit)
	assert.Equal(s.t, 0, page)
	assert.Nil(s.t, important)
	assert.False(s.t, reverse)
	return s.latest, s.err
}

type fixture struct {
	loader      *Loader
	monitors    *fakeMonitorService
	proxies     *fakeProxyService
	maintenance *fakeMaintenanceService
	heartbeats  *fakeHeartbeatService
}

func newFixture(t *testing.T, mon *monitor.Model) *fixture {
	t.Helper()
	f := &fixture{
		monitors:    &fakeMonitorService{mon: mon},
		proxies:     &fakeProxyService{proxies: map[string]*proxy.Model{}},
		maintenance: &fakeMaintenanceService{active: map[string]bool{}},
		heartbeats:  &fakeHeartbeatService{t: t},
	}
	f.loader = NewLoader(f.monitors, f.proxies, f.maintenance, f.heartbeats, zap.NewNop().Sugar())
	return f
}

func activeMonitor() *monitor.Model {
	return &monitor.Model{ID: monitorID, Name: "Test", Type: "http", Active: true, Interval: 60, Timeout: 30, MaxRetries: 3}
}

func TestLoader_LoadsTheMonitorForACheck(t *testing.T) {
	mon := activeMonitor()
	f := newFixture(t, mon)

	job, err := f.loader.Load(context.Background(), monitorID)

	require.NoError(t, err)
	assert.Equal(t, &Job{Monitor: mon}, job)
	assert.Zero(t, f.heartbeats.reads, "only push monitors read their latest heartbeat")
}

func TestLoader_SkipsMonitorsThatCantBeChecked(t *testing.T) {
	t.Run("a monitor that doesn't exist", func(t *testing.T) {
		f := newFixture(t, nil)

		job, err := f.loader.Load(context.Background(), monitorID)

		assert.ErrorIs(t, err, ErrMonitorNotFound)
		assert.Nil(t, job)
	})

	t.Run("a paused monitor", func(t *testing.T) {
		mon := activeMonitor()
		mon.Active = false
		f := newFixture(t, mon)

		job, err := f.loader.Load(context.Background(), monitorID)

		assert.ErrorIs(t, err, ErrMonitorPaused)
		assert.Nil(t, job)
	})

	t.Run("a monitor that can't be read", func(t *testing.T) {
		f := newFixture(t, activeMonitor())
		f.monitors.err = errors.New("database error")

		job, err := f.loader.Load(context.Background(), monitorID)

		assert.ErrorIs(t, err, f.monitors.err)
		assert.NotErrorIs(t, err, ErrMonitorNotFound)
		assert.NotErrorIs(t, err, ErrMonitorPaused)
		assert.Nil(t, job)
	})
}

func TestLoader_Maintenance(t *testing.T) {
	windows := func(ids ...string) []*maintenance.Model {
		out := []*maintenance.Model{}
		for _, id := range ids {
			out = append(out, &maintenance.Model{ID: id})
		}
		return out
	}

	tests := []struct {
		name      string
		windows   []*maintenance.Model
		active    map[string]bool
		statusErr map[string]bool
		want      bool
	}{
		{name: "no maintenance windows", windows: windows(), want: false},
		{name: "a window that isn't active", windows: windows("m1"), want: false},
		{name: "an active window", windows: windows("m1"), active: map[string]bool{"m1": true}, want: true},
		{name: "one active window among several", windows: windows("m1", "m2"), active: map[string]bool{"m2": true}, want: true},
		{
			name:      "a window whose status can't be read is skipped",
			windows:   windows("m1", "m2"),
			active:    map[string]bool{"m2": true},
			statusErr: map[string]bool{"m1": true},
			want:      true,
		},
		{name: "only windows whose status can't be read", windows: windows("m1"), statusErr: map[string]bool{"m1": true}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, activeMonitor())
			f.maintenance.windows = tt.windows
			f.maintenance.active = tt.active
			f.maintenance.statusErr = tt.statusErr

			job, err := f.loader.Load(context.Background(), monitorID)

			require.NoError(t, err)
			assert.Equal(t, tt.want, job.IsUnderMaintenance)
		})
	}

	t.Run("windows that can't be listed fail the load", func(t *testing.T) {
		f := newFixture(t, activeMonitor())
		f.maintenance.listErr = errors.New("database error")

		job, err := f.loader.Load(context.Background(), monitorID)

		assert.ErrorIs(t, err, f.maintenance.listErr)
		assert.Nil(t, job)
	})
}

func TestLoader_Proxy(t *testing.T) {
	behindProxy := func() *monitor.Model {
		mon := activeMonitor()
		mon.ProxyId = "proxy-1"
		return mon
	}
	proxyModel := &proxy.Model{ID: "proxy-1", Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "user", Password: "pass"}

	t.Run("loads the monitor's proxy", func(t *testing.T) {
		f := newFixture(t, behindProxy())
		f.proxies.proxies["proxy-1"] = proxyModel

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Equal(t, proxyModel, job.Proxy)
	})

	t.Run("checks without the proxy when it can't be loaded", func(t *testing.T) {
		f := newFixture(t, behindProxy())
		f.proxies.err = errors.New("proxy not found")

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Nil(t, job.Proxy)
	})

	t.Run("no proxy without a proxy ID", func(t *testing.T) {
		f := newFixture(t, activeMonitor())
		f.proxies.err = errors.New("must not be asked")

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Nil(t, job.Proxy)
	})
}

func TestLoader_PushMonitorCarriesItsLatestHeartbeat(t *testing.T) {
	pushMonitor := func() *monitor.Model {
		mon := activeMonitor()
		mon.Type = "push"
		mon.Interval = 3600
		return mon
	}
	latest := &heartbeat.Model{
		ID:        "hb-9",
		MonitorID: monitorID,
		Status:    shared.MonitorStatusUp,
		Msg:       "pushed",
		Time:      time.Date(2026, 10, 18, 6, 0, 0, 0, time.UTC),
	}

	t.Run("carries its latest heartbeat", func(t *testing.T) {
		f := newFixture(t, pushMonitor())
		f.heartbeats.latest = []*heartbeat.Model{latest}

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Equal(t, latest, job.LastHeartbeat)
		assert.Equal(t, 1, f.heartbeats.reads)
	})

	t.Run("checks without one when none exists", func(t *testing.T) {
		f := newFixture(t, pushMonitor())
		f.heartbeats.latest = []*heartbeat.Model{}

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Nil(t, job.LastHeartbeat)
	})

	t.Run("checks without one when it can't be read", func(t *testing.T) {
		f := newFixture(t, pushMonitor())
		f.heartbeats.err = errors.New("database is locked")

		job, err := f.loader.Load(context.Background(), monitorID)

		require.NoError(t, err)
		assert.Nil(t, job.LastHeartbeat)
	})
}

func TestLoader_CertExpiryFlag(t *testing.T) {
	tests := []struct {
		name        string
		monitorType string
		config      string
		want        bool
	}{
		{name: "on in an http monitor", monitorType: "http", config: `{"url": "https://example.com", "check_cert_expiry": true}`, want: true},
		{name: "on in an smtp monitor", monitorType: "smtp", config: `{"check_cert_expiry": true}`, want: true},
		{name: "read for every type", monitorType: "tcp", config: `{"check_cert_expiry": true}`, want: true},
		{name: "off in the config", monitorType: "http", config: `{"check_cert_expiry": false}`, want: false},
		{name: "absent from the config", monitorType: "http", config: `{"url": "https://example.com"}`, want: false},
		{name: "no config", monitorType: "http", config: "", want: false},
		{name: "a config that isn't JSON", monitorType: "http", config: `{`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := activeMonitor()
			mon.Type = tt.monitorType
			mon.Config = tt.config
			f := newFixture(t, mon)

			job, err := f.loader.Load(context.Background(), monitorID)

			require.NoError(t, err)
			assert.Equal(t, tt.want, job.CheckCertExpiry)
		})
	}
}
