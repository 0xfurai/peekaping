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

// stubMonitors is the part of the monitor service that lists the active monitors
type stubMonitors struct {
	monitor.Service
	active []*monitor.Model
	err    error
	calls  int
}

func (s *stubMonitors) FindActive(context.Context) ([]*monitor.Model, error) {
	s.calls++
	return s.active, s.err
}

func TestLocalOnlySource_TheLocalProbeOwnsEveryActiveMonitor(t *testing.T) {
	monitors := &stubMonitors{active: []*monitor.Model{monitorWith("http-1", "http"), monitorWith("push-1", "push")}}
	source := protocol.NewLocalOnlySource(monitors)

	got, err := source.FindActiveByProbe(context.Background(), probe.LocalID)

	require.NoError(t, err)
	assert.Equal(t, []string{"http-1", "push-1"}, idsOf(got), "it lists them all: telling which can be dealt is Dealable's job")
}

func TestLocalOnlySource_ARemoteProbeOwnsNone(t *testing.T) {
	monitors := &stubMonitors{active: []*monitor.Model{monitorWith("http-1", "http")}}
	source := protocol.NewLocalOnlySource(monitors)

	got, err := source.FindActiveByProbe(context.Background(), "probe-in-frankfurt")

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
	assert.Zero(t, monitors.calls, "and there is no need to read the database to know")
}

func TestLocalOnlySource_PassesOnAFailure(t *testing.T) {
	failure := errors.New("database is down")
	source := protocol.NewLocalOnlySource(&stubMonitors{err: failure})

	_, err := source.FindActiveByProbe(context.Background(), probe.LocalID)

	assert.ErrorIs(t, err, failure)
}
