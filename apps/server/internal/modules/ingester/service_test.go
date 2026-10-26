package ingester

import (
	"context"
	"testing"

	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_IngestStoresTheResultAndPublishesItsEvents(t *testing.T) {
	ti := newTestIngester()
	result := checkResult(monitorSettings{}, down)

	require.NoError(t, ti.service.Ingest(context.Background(), &result))

	got := ti.heartbeats.latest()
	require.NotNil(t, got)
	assert.Equal(t, monitorID, got.MonitorID)
	assert.Equal(t, down, got.Status)
	assert.Equal(t, "check message", got.Msg)
	assert.Equal(t, 12, got.Ping)
	assert.Equal(t, result.StartTime, got.Time)
	assert.Equal(t, result.EndTime, got.EndTime)
	assert.Equal(t, []string{events.MonitorStatusChanged.Name(), events.ImportantHeartbeat.Name()}, ti.bus.TakeTopics())
}

func TestService_IngestRecordsTheProbeThatRanTheCheck(t *testing.T) {
	ti := newTestIngester()

	fromLocal := checkResult(monitorSettings{}, up)
	require.NoError(t, ti.service.Ingest(context.Background(), &fromLocal))
	assert.Equal(t, "local", ti.heartbeats.latest().ProbeID, "a result with no probe came from the local probe")

	fromRemote := checkResult(monitorSettings{}, up)
	fromRemote.ProbeID = "probe-frankfurt"
	require.NoError(t, ti.service.Ingest(context.Background(), &fromRemote))
	assert.Equal(t, "probe-frankfurt", ti.heartbeats.latest().ProbeID)
}

func TestService_IngestSeesThePreviousResult(t *testing.T) {
	ti := newTestIngester()
	monitor := monitorSettings{maxRetries: 1}

	first, second := checkResult(monitor, up), checkResult(monitor, down)
	require.NoError(t, ti.service.Ingest(context.Background(), &first))
	require.NoError(t, ti.service.Ingest(context.Background(), &second))

	require.Len(t, ti.heartbeats.stored, 2)
	assert.Equal(t, pending, ti.heartbeats.latest().Status, "the first failure is Pending while retries are left")
}

func TestService_IngestReturnsAFailedWrite(t *testing.T) {
	ti := newTestIngester()
	ti.heartbeats.createErr = assert.AnError
	result := checkResult(monitorSettings{}, up)

	assert.ErrorIs(t, ti.service.Ingest(context.Background(), &result), assert.AnError)
	assert.Empty(t, ti.bus.TakeTopics())
}
