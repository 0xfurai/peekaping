package ingester

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type memorySlowResponseStore struct {
	states map[string]*SlowResponseState
}

func newMemorySlowResponseStore() *memorySlowResponseStore {
	return &memorySlowResponseStore{states: map[string]*SlowResponseState{}}
}

func (s *memorySlowResponseStore) key(monitorID, probeID string) string {
	return monitorID + ":" + probeID
}

func (s *memorySlowResponseStore) Get(_ context.Context, monitorID, probeID string) (*SlowResponseState, error) {
	st, ok := s.states[s.key(monitorID, probeID)]
	if !ok {
		return nil, nil
	}
	cp := *st
	return &cp, nil
}

func (s *memorySlowResponseStore) Upsert(_ context.Context, state *SlowResponseState) error {
	cp := *state
	s.states[s.key(state.MonitorID, state.ProbeID)] = &cp
	return nil
}

func (s *memorySlowResponseStore) Reset(_ context.Context, monitorID, probeID string) error {
	delete(s.states, s.key(monitorID, probeID))
	return nil
}

type fakeBaselineStats struct {
	summary *stats.Stats
	err     error
}

func (f *fakeBaselineStats) Summarize(_ context.Context, _, _ string, _, _ time.Time) (*stats.Stats, error) {
	return f.summary, f.err
}

func slowIntPtr(v int) *int           { return &v }
func slowFloatPtr(v float64) *float64 { return &v }

func newSlowTestIngester(baseline *stats.Stats, probes ...string) (Service, *fakeHeartbeatService, *eventstest.Bus) {
	if len(probes) == 0 {
		probes = []string{shared.LocalProbeID}
	}
	bus := eventstest.NewBus()
	heartbeats := &fakeHeartbeatService{}
	locations := &fakeLocations{
		probes:  probes,
		offline: map[string]bool{},
		states:  map[string]shared.LocationState{},
	}
	store := newMemorySlowResponseStore()
	svc := NewService(
		heartbeats,
		&fakeCertificateService{},
		locations,
		bus,
		zap.NewNop().Sugar(),
		WithSlowResponse(store, &fakeBaselineStats{summary: baseline}),
	)
	return svc, heartbeats, bus
}

func TestIngester_SlowResponseFixedModeAlertAndRecovery(t *testing.T) {
	svc, heartbeats, bus := newSlowTestIngester(nil)
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	send := func(status shared.MonitorStatus, pingMs int, step int) {
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorName:                    "Checkout API",
			MonitorType:                    "http",
			MonitorSlowResponseMode:        shared.SlowResponseModeFixed,
			MonitorSlowResponseThresholdMs: slowIntPtr(500),
			MonitorSlowResponseConsecutive: 3,
			Status:                         status,
			Message:                        "OK",
			PingMs:                         pingMs,
			StartTime:                      now.Add(time.Duration(step) * time.Minute),
			EndTime:                        now.Add(time.Duration(step) * time.Minute),
		}))
	}

	// Two slow checks followed by a normal one reset the streak and send nothing
	send(up, 600, 1)
	send(up, 650, 2)
	send(up, 120, 3)
	assert.Empty(t, eventstest.Published(bus, events.SlowResponse))
	assert.Empty(t, eventstest.Published(bus, events.SlowResponseRecovered))

	// Three consecutive slow Up checks fire exactly one alert while status stays Up
	send(up, 550, 4)
	send(up, 700, 5)
	assert.Empty(t, eventstest.Published(bus, events.SlowResponse))
	send(up, 820, 6)

	alerts := eventstest.Published(bus, events.SlowResponse)
	require.Len(t, alerts, 1)
	assert.Equal(t, monitorID, alerts[0].MonitorID)
	assert.Equal(t, 820, alerts[0].PingMs)
	assert.Equal(t, 500, alerts[0].ThresholdMs)
	assert.Equal(t, 3, alerts[0].ConsecutiveCount)
	assert.Equal(t, up, heartbeats.latest().Status, "monitor status stays Up during slow response")

	// Additional slow checks while already alerting do not spam
	send(up, 900, 7)
	send(up, 950, 8)
	assert.Len(t, eventstest.Published(bus, events.SlowResponse), 1)

	// Returning below threshold sends one recovery notification
	send(up, 200, 9)
	recs := eventstest.Published(bus, events.SlowResponseRecovered)
	require.Len(t, recs, 1)
	assert.Equal(t, monitorID, recs[0].MonitorID)
	assert.Equal(t, 200, recs[0].PingMs)
	assert.Equal(t, 500, recs[0].ThresholdMs)

	// Subsequent normal checks do not re-send recovery
	send(up, 150, 10)
	assert.Len(t, eventstest.Published(bus, events.SlowResponseRecovered), 1)
}

func TestIngester_SlowResponseDownResetsStateWithoutSpuriousRecovery(t *testing.T) {
	svc, _, bus := newSlowTestIngester(nil)
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	send := func(status shared.MonitorStatus, pingMs int, step int) {
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorName:                    "Checkout API",
			MonitorType:                    "http",
			MonitorSlowResponseMode:        shared.SlowResponseModeFixed,
			MonitorSlowResponseThresholdMs: slowIntPtr(300),
			MonitorSlowResponseConsecutive: 1,
			Status:                         status,
			PingMs:                         pingMs,
			StartTime:                      now.Add(time.Duration(step) * time.Minute),
			EndTime:                        now.Add(time.Duration(step) * time.Minute),
		}))
	}

	send(up, 450, 1)
	require.Len(t, eventstest.Published(bus, events.SlowResponse), 1)

	// Monitor goes Down, then comes back Up fast -> should NOT fire slow response recovery
	send(down, 0, 2)
	send(up, 100, 3)
	assert.Empty(t, eventstest.Published(bus, events.SlowResponseRecovered))
}

func TestIngester_SlowResponseTracksProbesIndependently(t *testing.T) {
	svc, _, bus := newSlowTestIngester(nil, shared.LocalProbeID, "probe-eu")
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	send := func(probeID string, pingMs int, step int) {
		require.NoError(t, svc.Ingest(ctx, &Result{
			ProbeID:                        probeID,
			MonitorID:                      monitorID,
			MonitorName:                    "Global API",
			MonitorType:                    "http",
			MonitorSlowResponseMode:        shared.SlowResponseModeFixed,
			MonitorSlowResponseThresholdMs: slowIntPtr(300),
			MonitorSlowResponseConsecutive: 2,
			Status:                         up,
			PingMs:                         pingMs,
			StartTime:                      now.Add(time.Duration(step) * time.Minute),
			EndTime:                        now.Add(time.Duration(step) * time.Minute),
		}))
	}

	// One slow check on local and one on probe-eu should NOT combine into 2
	send(shared.LocalProbeID, 400, 1)
	send("probe-eu", 450, 2)
	assert.Empty(t, eventstest.Published(bus, events.SlowResponse))

	// Second slow check on probe-eu triggers alert for probe-eu
	send("probe-eu", 460, 3)
	alerts := eventstest.Published(bus, events.SlowResponse)
	require.Len(t, alerts, 1)
	assert.Equal(t, "probe-eu", alerts[0].ProbeID)
}

func TestIngester_SlowResponseBaselineModeSafeguards(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	t.Run("does not alert with fewer than MinBaselineSamples checks", func(t *testing.T) {
		avg := 50.0
		svc, _, bus := newSlowTestIngester(&stats.Stats{AvgPing: &avg, PingCount: MinBaselineSamples - 1})
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorSlowResponseMode:        shared.SlowResponseModeBaseline,
			MonitorSlowResponseMultiplier:  slowFloatPtr(2.0),
			MonitorSlowResponseMinMs:       slowIntPtr(100),
			MonitorSlowResponseConsecutive: 1,
			Status:                         up,
			PingMs:                         500,
			StartTime:                      now,
			EndTime:                        now,
		}))
		assert.Empty(t, eventstest.Published(bus, events.SlowResponse))
	})

	t.Run("respects minimum floor when 24h average is very small", func(t *testing.T) {
		avg := 5.0
		svc, _, bus := newSlowTestIngester(&stats.Stats{AvgPing: &avg, PingCount: 50})

		// 2x 5ms = 10ms, but min floor is 100ms -> 50ms must NOT alert
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorName:                    "Fast Local",
			MonitorSlowResponseMode:        shared.SlowResponseModeBaseline,
			MonitorSlowResponseMultiplier:  slowFloatPtr(2.0),
			MonitorSlowResponseMinMs:       slowIntPtr(100),
			MonitorSlowResponseConsecutive: 1,
			Status:                         up,
			PingMs:                         50,
			StartTime:                      now,
			EndTime:                        now,
		}))
		assert.Empty(t, eventstest.Published(bus, events.SlowResponse))

		// 150ms exceeds the 100ms floor -> alerts with threshold=100ms
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorName:                    "Fast Local",
			MonitorSlowResponseMode:        shared.SlowResponseModeBaseline,
			MonitorSlowResponseMultiplier:  slowFloatPtr(2.0),
			MonitorSlowResponseMinMs:       slowIntPtr(100),
			MonitorSlowResponseConsecutive: 1,
			Status:                         up,
			PingMs:                         150,
			StartTime:                      now.Add(time.Minute),
			EndTime:                        now.Add(time.Minute),
		}))
		alerts := eventstest.Published(bus, events.SlowResponse)
		require.Len(t, alerts, 1)
		assert.Equal(t, 100, alerts[0].ThresholdMs)
		assert.Equal(t, 150, alerts[0].PingMs)
		require.NotNil(t, alerts[0].BaselineAvgMs)
		assert.InDelta(t, 5.0, *alerts[0].BaselineAvgMs, 1e-9)
	})

	t.Run("uses multiplier x 24h average when above floor", func(t *testing.T) {
		avg := 200.0
		svc, _, bus := newSlowTestIngester(&stats.Stats{AvgPing: &avg, PingCount: 100})

		// 2.5 x 200ms = 500ms (> 100ms floor)
		require.NoError(t, svc.Ingest(ctx, &Result{
			MonitorID:                      monitorID,
			MonitorName:                    "EU API",
			MonitorSlowResponseMode:        shared.SlowResponseModeBaseline,
			MonitorSlowResponseMultiplier:  slowFloatPtr(2.5),
			MonitorSlowResponseMinMs:       slowIntPtr(100),
			MonitorSlowResponseConsecutive: 1,
			Status:                         up,
			PingMs:                         550,
			StartTime:                      now,
			EndTime:                        now,
		}))
		alerts := eventstest.Published(bus, events.SlowResponse)
		require.Len(t, alerts, 1)
		assert.Equal(t, 500, alerts[0].ThresholdMs)
		assert.Equal(t, 550, alerts[0].PingMs)
	})
}
