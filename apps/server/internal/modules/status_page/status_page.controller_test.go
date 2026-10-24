package status_page

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// locationsService is the monitor service as publicLocations reads it. Any other method panics.
type locationsService struct {
	monitor.Service
	uptimes     map[string]float64
	uptimeErr   map[string]error
	uptimeCalls []string
	// statsCalls counts the reads of the four-period uptime stats, which a public page doesn't need
	statsCalls int
	// heartbeats and heartbeatsErr are what GetHeartbeats answers for each probe
	heartbeats    map[string][]*heartbeat.Model
	heartbeatsErr map[string]error
}

func (s *locationsService) GetHeartbeats(_ context.Context, _, probeID string, _, _ int, _ *bool, _ bool) ([]*heartbeat.Model, error) {
	if err := s.heartbeatsErr[probeID]; err != nil {
		return nil, err
	}
	return s.heartbeats[probeID], nil
}

func (s *locationsService) GetUptime24h(_ context.Context, _, probeID string) (float64, error) {
	s.uptimeCalls = append(s.uptimeCalls, probeID)
	if err := s.uptimeErr[probeID]; err != nil {
		return 0, err
	}
	return s.uptimes[probeID], nil
}

func (s *locationsService) GetUptimeStats(context.Context, string, string) (*monitor.CustomUptimeStatsDto, error) {
	s.statsCalls++
	return nil, errors.New("stats were not meant to be read")
}

func TestPublicLocations_ReadOnlyTheDayOfEachLocationWithOneStatsRead(t *testing.T) {
	up := shared.MonitorStatusUp
	svc := &locationsService{uptimes: map[string]float64{shared.LocalProbeID: 100, "fra": 50}}
	core, _ := observer.New(zap.ErrorLevel)
	c := &Controller{monitorService: svc, logger: zap.New(core).Sugar()}
	m := &shared.Monitor{ID: "m1", Locations: []shared.MonitorLocation{
		{ProbeID: "fra", ProbeName: "Frankfurt", Status: &up},
		{ProbeID: shared.LocalProbeID, ProbeName: "Local", Status: &up},
	}}

	got, err := c.publicLocations(context.Background(), m, 10)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Local", got[0].Name)
	require.NotNil(t, got[0].Uptime24h)
	assert.Equal(t, 100.0, *got[0].Uptime24h)
	require.NotNil(t, got[1].Uptime24h)
	assert.Equal(t, 50.0, *got[1].Uptime24h)
	assert.Equal(t, []string{shared.LocalProbeID, "fra"}, svc.uptimeCalls, "one read for each location, filtered by its probe")
	assert.Zero(t, svc.statsCalls)
}

func TestPublicLocations_ALocationWhoseUptimeCantBeReadIsListedWithNoUptime(t *testing.T) {
	up := shared.MonitorStatusUp
	svc := &locationsService{
		uptimes:   map[string]float64{shared.LocalProbeID: 100},
		uptimeErr: map[string]error{"fra": errors.New("stats are down")},
	}
	core, logs := observer.New(zap.ErrorLevel)
	c := &Controller{monitorService: svc, logger: zap.New(core).Sugar()}
	m := &shared.Monitor{ID: "m1", Locations: []shared.MonitorLocation{
		{ProbeID: shared.LocalProbeID, ProbeName: "Local", Status: &up},
		{ProbeID: "fra", ProbeName: "Frankfurt", Status: &up},
	}}

	got, err := c.publicLocations(context.Background(), m, 10)

	require.NoError(t, err, "one location's stats don't take the page down")
	require.Len(t, got, 2)
	require.NotNil(t, got[0].Uptime24h)
	assert.Equal(t, 100.0, *got[0].Uptime24h)
	assert.Equal(t, "Frankfurt", got[1].Name)
	assert.Nil(t, got[1].Uptime24h, "null, not a 0 that reads as an outage")
	assert.Equal(t, 1, logs.FilterMessageSnippet("uptime").Len(), "and the error is logged")
}

func TestPublicLocations_ALocationWhoseHeartbeatsCantBeReadIsListedWithNone(t *testing.T) {
	up := shared.MonitorStatusUp
	svc := &locationsService{
		uptimes:       map[string]float64{shared.LocalProbeID: 100, "fra": 50},
		heartbeats:    map[string][]*heartbeat.Model{shared.LocalProbeID: {{ID: "hb1", Status: up}}},
		heartbeatsErr: map[string]error{"fra": errors.New("heartbeats are down")},
	}
	core, logs := observer.New(zap.ErrorLevel)
	c := &Controller{monitorService: svc, logger: zap.New(core).Sugar()}
	m := &shared.Monitor{ID: "m1", Locations: []shared.MonitorLocation{
		{ProbeID: shared.LocalProbeID, ProbeName: "Local", Status: &up},
		{ProbeID: "fra", ProbeName: "Frankfurt", Status: &up},
	}}

	got, err := c.publicLocations(context.Background(), m, 10)

	require.NoError(t, err, "one location's heartbeats don't take the page down, as the monitor's own don't")
	require.Len(t, got, 2)
	require.Len(t, got[0].Heartbeats, 1)
	assert.Equal(t, "hb1", got[0].Heartbeats[0].ID)
	assert.Equal(t, "Frankfurt", got[1].Name)
	assert.Empty(t, got[1].Heartbeats)
	require.NotNil(t, got[1].Uptime24h, "its uptime is still read")
	assert.Equal(t, 50.0, *got[1].Uptime24h)
	assert.Equal(t, 1, logs.FilterMessageSnippet("heartbeats").Len(), "and the error is logged")
}
