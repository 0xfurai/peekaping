package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The topics' names are what travels between processes, so a rename breaks a rolling upgrade
func TestProbeTopics_KeepTheirNamesOnTheWire(t *testing.T) {
	assert.Equal(t, "probe.offline", probe.ProbeOffline.Name())
	assert.Equal(t, "probe.online", probe.ProbeOnline.Name())
}

func TestProbeEvent_ArrivesAsItWasPublishedInAnotherProcess(t *testing.T) {
	bus := eventstest.NewBus()
	var got []probe.Event
	subscriber := events.NewSubscriber(bus, "test")
	events.Subscribe(subscriber, probe.ProbeOffline, func(_ context.Context, e probe.Event) { got = append(got, e) })

	require.NoError(t, bus.PublishJSON("probe.offline", `{"probe_id":"p-1","probe_name":"Frankfurt","since":"2026-10-22T08:30:00Z"}`))

	require.Len(t, got, 1)
	assert.Equal(t, probe.Event{ProbeID: "p-1", ProbeName: "Frankfurt", Since: time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)}, got[0])
}
