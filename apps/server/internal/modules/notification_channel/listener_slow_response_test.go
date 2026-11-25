package notification_channel

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListener_SlowResponseAndRecoveryNotifications(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-slack"))

	ctx := context.Background()
	now := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	avg := 120.0
	mult := 2.5

	require.NoError(t, events.Publish(ctx, f.bus, events.SlowResponse, shared.SlowResponseEvent{
		MonitorID:        "mon-1",
		MonitorName:      "Public API",
		ProbeID:          shared.LocalProbeID,
		Mode:             shared.SlowResponseModeBaseline,
		PingMs:           420,
		ThresholdMs:      300,
		BaselineAvgMs:    &avg,
		Multiplier:       &mult,
		ConsecutiveCount: 3,
		Time:             now,
	}))

	require.Len(t, slack.sent, 1)
	sent := slack.sent[0]
	assert.Contains(t, sent.message, "Slow Response Warning")
	assert.Contains(t, sent.message, "Public API")
	assert.Contains(t, sent.message, "420 ms")
	assert.Contains(t, sent.message, "threshold: 300 ms")
	assert.Contains(t, sent.message, "2.5x 24h avg of 120 ms")
	assert.Contains(t, sent.message, "3 consecutive slow checks")
	assert.Nil(t, sent.heartbeat)

	require.NoError(t, events.Publish(ctx, f.bus, events.SlowResponseRecovered, shared.SlowResponseEvent{
		MonitorID:   "mon-1",
		MonitorName: "Public API",
		ProbeID:     shared.LocalProbeID,
		Mode:        shared.SlowResponseModeBaseline,
		PingMs:      110,
		ThresholdMs: 300,
		Time:        now.Add(time.Minute),
	}))

	require.Len(t, slack.sent, 2)
	rec := slack.sent[1]
	assert.Contains(t, rec.message, "Response Time Recovered")
	assert.Contains(t, rec.message, "Public API")
	assert.Contains(t, rec.message, "110 ms")
	assert.Contains(t, rec.message, "threshold: 300 ms")
	assert.Nil(t, rec.heartbeat)
}

func TestFormatSlowResponseMessage_IncludesLocationForMultiProbeMonitor(t *testing.T) {
	m := &monitor.Model{
		ID:   "mon-1",
		Name: "Checkout",
		Locations: []shared.MonitorLocation{
			{ProbeID: shared.LocalProbeID, ProbeName: "Local"},
			{ProbeID: "probe-eu", ProbeName: "Frankfurt"},
		},
	}

	msg := formatSlowResponseMessage(&shared.SlowResponseEvent{
		MonitorID:        "mon-1",
		MonitorName:      "Checkout",
		ProbeID:          "probe-eu",
		Mode:             shared.SlowResponseModeFixed,
		PingMs:           750,
		ThresholdMs:      500,
		ConsecutiveCount: 2,
	}, m, false)

	assert.Equal(t, "⚠️ Slow Response Warning: Checkout at Frankfurt took 750 ms (threshold: 500 ms, 2 consecutive slow checks)", msg)
}
