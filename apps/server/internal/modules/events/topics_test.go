package events_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A topic's name is the Redis channel suffix, which old and new versions of Peekaping must
// agree on during a rolling upgrade. The literals are the names the wire has always carried.
func TestTopicsKeepTheirNamesOnTheWire(t *testing.T) {
	assert.Equal(t, "monitor.created", events.MonitorCreated.Name())
	assert.Equal(t, "monitor.updated", events.MonitorUpdated.Name())
	assert.Equal(t, "monitor.deleted", events.MonitorDeleted.Name())
	assert.Equal(t, "heartbeat", events.HeartbeatEvent.Name())
	assert.Equal(t, "monitor.status.changed", events.MonitorStatusChanged.Name())
	assert.Equal(t, "important.heartbeat", events.ImportantHeartbeat.Name())
	assert.Equal(t, "certificate.expiry", certificate.CertificateExpiry.Name())
	assert.Equal(t, "monitor.stale", stale.MonitorStale.Name())
	assert.Equal(t, "monitor.stale.recovered", stale.MonitorStaleRecovered.Name())
	assert.Equal(t, "probe.instances.changed", probe.InstancesChanged.Name())
}

// A probe's instances belong to no monitor, so their events are handled with no order between them.
func TestInstancesChangedBelongsToNoMonitor(t *testing.T) {
	assert.Empty(t, orderedBy(t, probe.InstancesChanged, probe.InstancesChangedEvent{ProbeID: "probe-1"}))
}

// Every topic orders its events by the monitor they are about.
func TestTopicsOrderEventsByMonitor(t *testing.T) {
	assert.Equal(t, "mon-1", orderedBy(t, events.MonitorCreated, shared.Monitor{ID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, events.MonitorUpdated, shared.Monitor{ID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, events.MonitorDeleted, "mon-1"))
	assert.Equal(t, "mon-1", orderedBy(t, events.HeartbeatEvent, shared.HeartBeatModel{MonitorID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, events.MonitorStatusChanged, shared.HeartBeatModel{MonitorID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, events.ImportantHeartbeat, shared.HeartBeatModel{MonitorID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, certificate.CertificateExpiry, certificate.CertificateExpiryEvent{MonitorID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, stale.MonitorStale, stale.Event{MonitorID: "mon-1"}))
	assert.Equal(t, "mon-1", orderedBy(t, stale.MonitorStaleRecovered, stale.Event{MonitorID: "mon-1"}))
}

// handlerCapture is an event bus that keeps the handler registered on it.
type handlerCapture struct {
	events.EventBus
	handler events.Handler
}

func (c *handlerCapture) AddHandler(_ string, handler events.Handler) { c.handler = handler }

// orderedBy is the monitor ID that a subscriber orders a payload of topic by.
func orderedBy[T any](t *testing.T, topic events.Topic[T], payload T) string {
	t.Helper()
	capture := &handlerCapture{}
	events.Subscribe(events.NewSubscriber(capture, "test"), topic, func(context.Context, T) {})
	monitorID, _, err := capture.handler.Bind(events.Message{Topic: topic.Name(), Value: payload})
	require.NoError(t, err)
	return monitorID
}
