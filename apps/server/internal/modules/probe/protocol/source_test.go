package protocol_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubMonitors is the part of the monitor service that finds the monitors of a probe, as the
// repository pages them: by probe, a page of limit at a time
type stubMonitors struct {
	monitor.Service
	// byProbe are the active monitors of each probe
	byProbe map[string][]*monitor.Model
	err     error
	// pages are the pages asked for, as "probe/page"
	pages []string
}

func (s *stubMonitors) FindActiveByProbe(_ context.Context, probeID string, page int, limit int) ([]*monitor.Model, error) {
	s.pages = append(s.pages, fmt.Sprintf("%s/%d", probeID, page))
	if s.err != nil {
		return nil, s.err
	}
	all := s.byProbe[probeID]
	start := min(page*limit, len(all))
	end := min(start+limit, len(all))
	return all[start:end], nil
}

func TestMonitorSource_ListsTheActiveMonitorsOfTheProbe(t *testing.T) {
	monitors := &stubMonitors{byProbe: map[string][]*monitor.Model{
		probe.LocalID: {monitorWith("http-1", "http"), monitorWith("push-1", "push")},
		"frankfurt":   {monitorWith("tcp-1", "tcp")},
	}}
	source := protocol.NewMonitorSource(monitors)

	local, err := source.FindActiveByProbe(context.Background(), probe.LocalID)
	require.NoError(t, err)
	remote, err := source.FindActiveByProbe(context.Background(), "frankfurt")
	require.NoError(t, err)
	none, err := source.FindActiveByProbe(context.Background(), "paris")
	require.NoError(t, err)

	assert.Equal(t, []string{"http-1", "push-1"}, idsOf(local), "it lists them all: telling which can be dealt is Dealable's job")
	assert.Equal(t, []string{"tcp-1"}, idsOf(remote))
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestMonitorSource_PagesThroughAllTheMonitors(t *testing.T) {
	var all []*monitor.Model
	for i := range 450 {
		all = append(all, monitorWith(fmt.Sprintf("monitor-%03d", i), "http"))
	}
	monitors := &stubMonitors{byProbe: map[string][]*monitor.Model{"frankfurt": all}}

	got, err := protocol.NewMonitorSource(monitors).FindActiveByProbe(context.Background(), "frankfurt")

	require.NoError(t, err)
	assert.Equal(t, idsOf(all), idsOf(got), "every monitor, once, in the order of the pages")
	assert.Equal(t, []string{"frankfurt/0", "frankfurt/1", "frankfurt/2"}, monitors.pages, "until a page comes up short")
}

func TestMonitorSource_PassesOnAFailure(t *testing.T) {
	failure := errors.New("database is down")
	source := protocol.NewMonitorSource(&stubMonitors{err: failure})

	_, err := source.FindActiveByProbe(context.Background(), probe.LocalID)
	assert.ErrorIs(t, err, failure)
}
