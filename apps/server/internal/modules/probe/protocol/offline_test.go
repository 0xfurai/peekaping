package protocol_test

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusProbes is the part of the probe service that finds a probe by its id
type statusProbes struct {
	probe.Service
	byID  map[string]*probe.Model
	err   error
	finds []string
}

func (s *statusProbes) FindByID(_ context.Context, id string) (*probe.Model, error) {
	s.finds = append(s.finds, id)
	return s.byID[id], s.err
}

// monitorOn is a monitor assigned to the probes
func monitorOn(id string, probeIDs ...string) *monitor.Model {
	m := monitorWith(id, "http")
	m.ProbeIDs = probeIDs
	return m
}

// pushMonitorOn is a push monitor assigned to the probes
func pushMonitorOn(id string, probeIDs ...string) *monitor.Model {
	m := monitorOn(id, probeIDs...)
	m.Type = "push"
	return m
}

func TestMonitorProbeOffline_TheLocalProbeIsNeverOffline(t *testing.T) {
	probes := &statusProbes{byID: map[string]*probe.Model{probe.LocalID: {ID: probe.LocalID, Status: probe.StatusOffline}}}
	// Nothing speaks the protocol on most installs, so the local probe reads offline there; its
	// monitors run in the process and are watched all the same
	offline := protocol.MonitorProbeOffline(probes)

	for name, m := range map[string]*monitor.Model{
		"assigned to local":         monitorOn("http-1", probe.LocalID),
		"read without its probes":   monitorOn("http-2"),
		"on local and a remote one": monitorOn("http-3", "probe-1", probe.LocalID),
		"a push monitor":            pushMonitorOn("push-1", probe.LocalID),
	} {
		got, err := offline(context.Background(), m)

		require.NoError(t, err, name)
		assert.False(t, got, name)
	}
	assert.Empty(t, probes.finds, "no probe is read for a monitor the local probe runs")
}

// AC4: in cluster mode on SQLite and PostgreSQL no process of Peekaping's own runs checks, so the
// local probe is its instances, like a remote probe: offline when they stopped, and its monitors
// are left alone while it is
func TestMonitorProbeOffline_InClusterModeTheLocalProbeFollowsItsInstances(t *testing.T) {
	offlineOf := func(status probe.Status) (stale.ProbeOfflineFunc, *statusProbes) {
		probes := &statusProbes{byID: map[string]*probe.Model{
			probe.LocalID: {ID: probe.LocalID, Status: status},
			"probe-1":     {ID: "probe-1", Status: probe.StatusOnline},
		}}
		return protocol.MonitorProbeOfflineWith(probes, shared.LocalProbeIsItsInstances), probes
	}

	t.Run("offline when its instances stopped", func(t *testing.T) {
		offline, _ := offlineOf(probe.StatusOffline)
		for name, m := range map[string]*monitor.Model{
			"assigned to local":                      monitorOn("http-1", probe.LocalID),
			"read without its probes":                monitorOn("http-2"),
			"on local and a remote probe that is up": monitorOn("http-3", "probe-1", probe.LocalID),
		} {
			got, err := offline(context.Background(), m)

			require.NoError(t, err, name)
			// The last one has a probe that is up: the monitor is checked there
			assert.Equal(t, name != "on local and a remote probe that is up", got, name)
		}
	})

	// A push monitor runs no checks: its pushes arrive at the api, with or without an instance of the
	// local probe. The watchdog is what finds out that the job that judges missing pushes has failed
	// or stalled, so it must not skip a push monitor because the local probe has no instance.
	t.Run("never for a push monitor, whatever its probe's status", func(t *testing.T) {
		offline, probes := offlineOf(probe.StatusOffline)

		for name, m := range map[string]*monitor.Model{
			"assigned to local":       pushMonitorOn("push-1", probe.LocalID),
			"read without its probes": pushMonitorOn("push-2"),
		} {
			got, err := offline(context.Background(), m)

			require.NoError(t, err, name)
			assert.False(t, got, name)
		}
		assert.Empty(t, probes.finds, "no probe is read for a push monitor")
	})

	t.Run("online while an instance is connected", func(t *testing.T) {
		offline, _ := offlineOf(probe.StatusOnline)

		got, err := offline(context.Background(), monitorOn("http-1", probe.LocalID))

		require.NoError(t, err)
		assert.False(t, got)
	})
}

func TestMonitorProbeOffline_AMonitorOfARemoteProbeFollowsTheProbesStatus(t *testing.T) {
	probes := &statusProbes{byID: map[string]*probe.Model{
		"probe-1": {ID: "probe-1", Status: probe.StatusOffline},
		"probe-2": {ID: "probe-2", Status: probe.StatusOnline},
	}}
	offline := protocol.MonitorProbeOffline(probes)

	for _, tc := range []struct {
		monitor *monitor.Model
		want    bool
	}{
		{monitorOn("away", "probe-1"), true},
		{monitorOn("home", "probe-2"), false},
		{monitorOn("orphan", "probe-3"), false}, // its probe is gone, which is no reason to stop watching the monitor
	} {
		got, err := offline(context.Background(), tc.monitor)

		require.NoError(t, err, tc.monitor.ID)
		assert.Equal(t, tc.want, got, tc.monitor.ID)
	}
	assert.Equal(t, []string{"probe-1", "probe-2", "probe-3"}, probes.finds, "one read for each monitor, of its probe")
}

func TestMonitorProbeOffline_AMonitorOnSeveralProbesIsOfflineOnlyWhenEveryProbeIs(t *testing.T) {
	probes := &statusProbes{byID: map[string]*probe.Model{
		"probe-1": {ID: "probe-1", Status: probe.StatusOffline},
		"probe-2": {ID: "probe-2", Status: probe.StatusOffline},
		"probe-3": {ID: "probe-3", Status: probe.StatusOnline},
	}}
	offline := protocol.MonitorProbeOffline(probes)

	for _, tc := range []struct {
		monitor *monitor.Model
		want    bool
	}{
		{monitorOn("all-away", "probe-1", "probe-2"), true},
		{monitorOn("one-home", "probe-1", "probe-3"), false},
		{monitorOn("one-gone", "probe-1", "probe-4"), false}, // a probe that is gone is no reason to stop watching
	} {
		got, err := offline(context.Background(), tc.monitor)

		require.NoError(t, err, tc.monitor.ID)
		assert.Equal(t, tc.want, got, tc.monitor.ID)
	}
}

func TestMonitorProbeOffline_PassesOnAFailure(t *testing.T) {
	failure := errors.New("database is down")

	_, err := protocol.MonitorProbeOffline(&statusProbes{err: failure})(context.Background(), monitorOn("away", "probe-1"))

	assert.ErrorIs(t, err, failure, "the probe could not be read")
}
