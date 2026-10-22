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

// ownedSource is a MonitorSource that knows which probe each monitor belongs to, as the one that
// reads monitor_probes (#34) will
type ownedSource struct {
	owners map[string]string
	err    error
}

func (s *ownedSource) FindActiveByProbe(context.Context, string) ([]*monitor.Model, error) {
	return []*monitor.Model{}, nil
}

func (s *ownedSource) ProbeOf(_ context.Context, monitorID string) (string, error) {
	return s.owners[monitorID], s.err
}

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

func TestMonitorProbeOffline_TheLocalProbeIsNeverOffline(t *testing.T) {
	probes := &statusProbes{byID: map[string]*probe.Model{probe.LocalID: {ID: probe.LocalID, Status: probe.StatusOffline}}}
	// Nothing speaks the protocol on most installs, so the local probe reads offline there; its
	// monitors run in the process and are watched all the same
	offline := protocol.MonitorProbeOffline(protocol.NewLocalOnlySource(&stubMonitors{}), probes)

	got, err := offline(context.Background(), "http-1")

	require.NoError(t, err)
	assert.False(t, got)
	assert.Empty(t, probes.finds, "the local probe's status is not even read")
}

func TestMonitorProbeOffline_AMonitorOfARemoteProbeFollowsTheProbesStatus(t *testing.T) {
	source := &ownedSource{owners: map[string]string{"away": "probe-1", "home": "probe-2", "orphan": "probe-3"}}
	probes := &statusProbes{byID: map[string]*probe.Model{
		"probe-1": {ID: "probe-1", Status: probe.StatusOffline},
		"probe-2": {ID: "probe-2", Status: probe.StatusOnline},
	}}
	offline := protocol.MonitorProbeOffline(source, probes)

	for monitorID, want := range map[string]bool{
		"away":   true,
		"home":   false,
		"orphan": false, // its probe is gone, which is no reason to stop watching the monitor
	} {
		got, err := offline(context.Background(), monitorID)

		require.NoError(t, err, monitorID)
		assert.Equal(t, want, got, monitorID)
	}
}

func TestMonitorProbeOffline_PassesOnAFailure(t *testing.T) {
	failure := errors.New("database is down")

	_, err := protocol.MonitorProbeOffline(&ownedSource{err: failure}, &statusProbes{})(context.Background(), "away")
	assert.ErrorIs(t, err, failure, "the source failed")

	source := &ownedSource{owners: map[string]string{"away": "probe-1"}}
	_, err = protocol.MonitorProbeOffline(source, &statusProbes{err: failure})(context.Background(), "away")
	assert.ErrorIs(t, err, failure, "the probe could not be read")
}
