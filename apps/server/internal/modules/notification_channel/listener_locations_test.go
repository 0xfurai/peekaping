package notification_channel

import (
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// location is a monitor's location on a probe, online and with the status
func location(probeID, name string, status shared.MonitorStatus) shared.MonitorLocation {
	return shared.MonitorLocation{ProbeID: probeID, ProbeName: name, Online: true, Status: &status}
}

// offline is a location whose probe has no instance connected
func offline(l shared.MonitorLocation) shared.MonitorLocation {
	l.Online = false
	return l
}

// addMonitorOn is a monitor that runs on the locations, which the monitor service lists by probe id
func (f *listenerFixture) addMonitorOn(id string, locations ...shared.MonitorLocation) {
	f.addMonitor(id, "Public API")
	f.monitors.monitors[id].Locations = locations
}

func (f *listenerFixture) notifyWith(t *testing.T, monitorID string, hb *heartbeat.Model) string {
	t.Helper()
	provider := &recordingProvider{}
	f.addProvider("test-slack", provider)
	f.attach(monitorID, testChannel("a", "test-slack"))

	f.publishImportantHeartbeat(hb)

	require.Len(t, provider.sent, 1)
	return provider.sent[0].message
}

func upHeartbeat(monitorID string) *heartbeat.Model {
	hb := downHeartbeat(monitorID)
	hb.Status, hb.Msg = shared.MonitorStatusUp, "200 - OK"
	return hb
}

func TestListener_ADownNotificationNamesTheDownLocations(t *testing.T) {
	f := newListenerFixture(t)
	// The monitor service lists the locations by probe id, which means nothing to a reader: the
	// message has the local probe first, then the others by name
	f.addMonitorOn("mon-1",
		location("0a", "Tokyo", shared.MonitorStatusDown),
		location("1b", "Frankfurt", shared.MonitorStatusDown),
		location("local", "Local", shared.MonitorStatusUp),
	)

	got := f.notifyWith(t, "mon-1", downHeartbeat("mon-1"))

	assert.Equal(t, "Down at Frankfurt, Tokyo (2 of 3 locations): connection refused", got)
}

func TestListener_ADownNotificationPutsTheLocalProbeFirst(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1",
		location("0a", "Amsterdam", shared.MonitorStatusDown),
		location("local", "Local", shared.MonitorStatusDown),
		location("1b", "Zurich", shared.MonitorStatusUp),
	)

	got := f.notifyWith(t, "mon-1", downHeartbeat("mon-1"))

	assert.Equal(t, "Down at Local, Amsterdam (2 of 3 locations): connection refused", got)
}

func TestListener_ADownNotificationLeavesOutALocationThatIsOffline(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1",
		location("0a", "Tokyo", shared.MonitorStatusDown),
		offline(location("1b", "Frankfurt", shared.MonitorStatusDown)),
		location("local", "Local", shared.MonitorStatusUp),
	)

	got := f.notifyWith(t, "mon-1", downHeartbeat("mon-1"))

	assert.Equal(t, "Down at Tokyo (1 of 3 locations): connection refused", got, "an offline probe's status is unknown, not Down")
}

func TestListener_AnUpNotificationSaysWhereTheMonitorIsUp(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1",
		location("0a", "Tokyo", shared.MonitorStatusUp),
		location("1b", "Frankfurt", shared.MonitorStatusUp),
		location("local", "Local", shared.MonitorStatusUp),
	)

	got := f.notifyWith(t, "mon-1", upHeartbeat("mon-1"))

	assert.Equal(t, "Up at all locations: 200 - OK", got)
}

func TestListener_AnUpNotificationCountsTheLocationsStillNotUp(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1",
		location("0a", "Tokyo", shared.MonitorStatusDown),
		location("1b", "Frankfurt", shared.MonitorStatusUp),
		location("local", "Local", shared.MonitorStatusUp),
	)

	got := f.notifyWith(t, "mon-1", upHeartbeat("mon-1"))

	assert.Equal(t, "Up at 2 of 3 locations: 200 - OK", got, "the monitor recovered under an at_least rule, with Tokyo still Down")
}

func TestListener_AMessageWithoutTextIsTheSummary(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1", location("0a", "Tokyo", shared.MonitorStatusDown), location("local", "Local", shared.MonitorStatusUp))
	hb := downHeartbeat("mon-1")
	hb.Msg = ""

	got := f.notifyWith(t, "mon-1", hb)

	assert.Equal(t, "Down at Tokyo (1 of 2 locations)", got)
}

func TestListener_AMonitorOnOneProbeKeepsItsMessage(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1", location("local", "Local", shared.MonitorStatusDown))

	got := f.notifyWith(t, "mon-1", downHeartbeat("mon-1"))

	assert.Equal(t, "connection refused", got)
}

func TestListener_AMonitorWithoutLocationsKeepsItsMessage(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitor("mon-1", "Public API")

	got := f.notifyWith(t, "mon-1", downHeartbeat("mon-1"))

	assert.Equal(t, "connection refused", got)
}

func TestListener_OnlyDownAndUpNotificationsGetTheSummary(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1", location("0a", "Tokyo", shared.MonitorStatusDown), location("local", "Local", shared.MonitorStatusUp))
	hb := downHeartbeat("mon-1")
	hb.Status = shared.MonitorStatusPending

	got := f.notifyWith(t, "mon-1", hb)

	assert.Equal(t, "connection refused", got)
}

func TestListener_TheMonitorTheProvidersGetKeepsItsLocations(t *testing.T) {
	f := newListenerFixture(t)
	f.addMonitorOn("mon-1", location("0a", "Tokyo", shared.MonitorStatusDown), location("local", "Local", shared.MonitorStatusUp))
	provider := &recordingProvider{}
	f.addProvider("test-slack", provider)
	f.attach("mon-1", testChannel("a", "test-slack"))

	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	require.Len(t, provider.sent, 1)
	assert.Equal(t, "connection refused", provider.sent[0].heartbeat.Msg, "the stored heartbeat is not changed")
	assert.Len(t, provider.sent[0].monitor.Locations, 2)
}
