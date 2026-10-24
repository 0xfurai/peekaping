package protocol_test

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"

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

func TestMonitorProbeOffline_TheLocalProbeIsNeverOffline(t *testing.T) {
	probes := &statusProbes{byID: map[string]*probe.Model{probe.LocalID: {ID: probe.LocalID, Status: probe.StatusOffline}}}
	// Nothing speaks the protocol on most installs, so the local probe reads offline there; its
	// monitors run in the process and are watched all the same
	offline := protocol.MonitorProbeOffline(probes)

	for name, m := range map[string]*monitor.Model{
		"assigned to local":              monitorOn("http-1", probe.LocalID),
		"read without its probes":        monitorOn("http-2"),
		"on local and a remote one":      monitorOn("http-3", "probe-1", probe.LocalID),
		"on local, as a push monitor is": monitorOn("push-1", probe.LocalID),
	} {
		got, err := offline(context.Background(), m)

		require.NoError(t, err, name)
		assert.False(t, got, name)
	}
	assert.Empty(t, probes.finds, "no probe is read for a monitor the local probe runs")
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
