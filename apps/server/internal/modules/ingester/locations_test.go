package ingester

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	probeA = "probe-a"
	probeB = "probe-b"
)

// shortOf names the events a step published
var (
	changed  = events.MonitorStatusChanged.Name()
	notified = events.ImportantHeartbeat.Name()
)

// from is a check result of the probe. Each call is a minute later than the last.
func (ti *testIngester) from(t *testing.T, probeID string, m monitorSettings, check shared.MonitorStatus) *storedBeat {
	t.Helper()
	result := checkResult(m, check)
	result.ProbeID = probeID
	result.StartTime = result.StartTime.Add(time.Duration(len(ti.heartbeats.stored)) * time.Minute)
	result.EndTime = result.StartTime.Add(12 * time.Millisecond)
	require.NoError(t, ti.ingest(t, result))
	got := ti.heartbeats.latest()
	ti.statusEvents = append(ti.statusEvents, eventstest.Published(ti.bus, events.MonitorStatusChanged)...)
	return &storedBeat{
		status: got.Status, retries: got.Retries, downCount: got.DownCount, important: got.Important, notified: got.Notified,
		events: ti.bus.TakeTopics(), published: ti.statusEvents,
	}
}

// storedBeat is the heartbeat a result produced, the events it published, and every status change
// published so far
type storedBeat struct {
	status    shared.MonitorStatus
	retries   int
	downCount int
	important bool
	notified  bool
	events    []string
	published []shared.HeartBeatModel
}

func TestIngester_TheMonitorIsDownWhileAnyLocationIsDown(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 0}

	first := ti.from(t, probeB, m, up)
	assert.Equal(t, []string{changed, notified}, first.events, "the monitor's first heartbeat, as before")
	assert.True(t, first.important)

	second := ti.from(t, probeA, m, up)
	assert.Equal(t, up, second.status)
	assert.Empty(t, second.events, "a second location that is Up changes nothing for the monitor")
	assert.False(t, second.important, "and its first heartbeat is not the monitor's first")
	assert.False(t, second.notified)

	aDown := ti.from(t, probeA, m, down)
	assert.Equal(t, down, aDown.status, "the heartbeat keeps the location's status")
	assert.Equal(t, 1, aDown.retries)
	assert.True(t, aDown.important)
	assert.True(t, aDown.notified)
	assert.Equal(t, []string{changed, notified}, aDown.events, "one Down notification when the monitor goes Down")

	bUp := ti.from(t, probeB, m, up)
	assert.Equal(t, up, bUp.status)
	assert.Empty(t, bUp.events, "B being Up doesn't end A's outage")
	assert.False(t, bUp.important)

	aStillDown := ti.from(t, probeA, m, down)
	assert.Empty(t, aStillDown.events)
	assert.Equal(t, 2, aStillDown.retries)

	recovered := ti.from(t, probeA, m, up)
	assert.Equal(t, up, recovered.status)
	assert.True(t, recovered.important)
	assert.True(t, recovered.notified)
	assert.Equal(t, []string{changed, notified}, recovered.events, "one Up notification when no location is Down")
	require.Len(t, recovered.published, 3)
	assert.Equal(t, up, recovered.published[2].Status)
}

func TestIngester_ALocationIsJudgedOnItsOwnHeartbeats(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 2}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)

	for step, want := range []struct {
		probe   string
		status  shared.MonitorStatus
		retries int
	}{
		{probeA, pending, 1},
		{probeB, pending, 1}, // A's failure is not B's
		{probeA, pending, 2},
		{probeB, pending, 2},
		{probeA, down, 3},
		{probeB, down, 3},
	} {
		beat := ti.from(t, want.probe, m, down)

		assert.Equal(t, want.status, beat.status, "step %d: %s", step+1, want.probe)
		assert.Equal(t, want.retries, beat.retries, "step %d: %s", step+1, want.probe)
	}
}

func TestIngester_ALocationsFailuresAreNotAnotherLocationsRetries(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 1}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	for range 3 {
		ti.from(t, probeA, m, down)
	}

	beat := ti.from(t, probeB, m, down)

	assert.Equal(t, pending, beat.status, "B has retries left whatever A has been through")
	assert.Equal(t, 1, beat.retries)
}

func TestIngester_AnOfflineProbeIsUnknownNotDown(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	down1 := ti.from(t, probeA, m, down)
	require.Equal(t, []string{changed, notified}, down1.events)
	ti.locations.setOffline(probeA)

	beat := ti.from(t, probeB, m, up)

	assert.Equal(t, up, beat.status)
	require.Len(t, beat.published, 3, "the monitor's status changed")
	assert.Equal(t, up, beat.published[2].Status, "A counts as unknown, and B is Up")
	assert.Equal(t, []string{changed, notified}, beat.events, "Down to Up: the monitor had been Down")
	assert.True(t, beat.important)
	assert.True(t, beat.notified)
}

// The monitor's status before a result is the one last recorded for it, not what its locations add
// up to now: a probe that went offline since then must not hide a change
func TestIngester_AnOfflineProbeThatWasDownIsAnnouncedWhenAnotherReportsUp(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeB, m, up)
	down1 := ti.from(t, probeA, m, down)
	require.Equal(t, []string{changed, notified}, down1.events, "the monitor is Down")
	ti.locations.setOffline(probeA)

	recovered := ti.from(t, probeB, m, up)

	assert.Equal(t, up, recovered.status)
	assert.True(t, recovered.important)
	assert.True(t, recovered.notified, "the monitor went Down to Up: one Up notification")
	assert.Equal(t, []string{changed, notified}, recovered.events)
	require.Len(t, recovered.published, 3)
	assert.Equal(t, up, recovered.published[2].Status, "and the monitor's stored status follows")

	again := ti.from(t, probeB, m, up)
	assert.Empty(t, again.events, "B being Up again changes nothing")
	assert.False(t, again.important)
}

func TestIngester_AnOfflineProbeThatWasUpIsLeftOutWhenAnotherGoesDown(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.locations.setOffline(probeA)

	wentDown := ti.from(t, probeB, m, down)
	stillDown := ti.from(t, probeB, m, down)

	assert.Equal(t, []string{changed, notified}, wentDown.events, "the monitor went Up to Down: one Down notification")
	assert.Equal(t, down, wentDown.published[len(wentDown.published)-1].Status)
	assert.Empty(t, stillDown.events, "and not another")
}

func TestIngester_AProbeThatSendsAResultIsOnlineForIt(t *testing.T) {
	ti := newTestIngester(runningOn(probeA))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.locations.setOffline(probeA) // the probe says it is offline, and its result says otherwise

	down1 := ti.from(t, probeA, m, down)
	repeated := ti.from(t, probeA, m, down)

	assert.Equal(t, []string{changed, notified}, down1.events, "Up to Down, as for any monitor")
	assert.Empty(t, repeated.events)
}

func TestIngester_TwoDownLocationsDoNotDoubleTheResends(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{resendInterval: 2}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)

	steps := []struct {
		probe     string
		downCount int
		events    []string
	}{
		{probeA, 0, []string{changed, notified}}, // the monitor goes Down
		{probeB, 0, []string{}},                  // B is Down too: not the one that counts
		{probeA, 1, []string{}},
		{probeB, 0, []string{}},
		{probeA, 0, []string{notified}}, // the second Down heartbeat of the counting location resends
		{probeB, 0, []string{}},
		{probeA, 1, []string{}},
		{probeA, 0, []string{notified}},
	}
	for i, want := range steps {
		beat := ti.from(t, want.probe, m, down)

		assert.Equal(t, want.downCount, beat.downCount, "step %d: down count", i+1)
		assert.Equal(t, want.events, beat.events, "step %d: events", i+1)
	}
}

func TestIngester_ResendsFollowTheFirstDownLocationWhenTheOthersRecover(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{resendInterval: 2}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.from(t, probeB, m, down) // the monitor goes Down
	ti.from(t, probeA, m, down)
	ti.from(t, probeA, m, up) // A recovers; B is Down alone

	first := ti.from(t, probeB, m, down)
	second := ti.from(t, probeB, m, down)

	assert.Equal(t, 1, first.downCount)
	assert.Empty(t, first.events)
	assert.Equal(t, []string{notified}, second.events, "B resends on its own")
}

func TestIngester_RecordsTheLocationsState(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	result := checkResult(monitorSettings{maxRetries: 1}, down)
	result.ProbeID = probeA
	result.PingMs = 321

	require.NoError(t, ti.ingest(t, result))

	require.Len(t, ti.locations.records, 1)
	got := ti.locations.records[0]
	assert.Equal(t, probeA, got.ProbeID)
	require.NotNil(t, got.Status)
	assert.Equal(t, down, *got.Status, "the first heartbeat of a location skips Pending, as before")
	require.NotNil(t, got.LastPing)
	assert.Equal(t, 321, *got.LastPing)
	require.NotNil(t, got.LastHeartbeatAt)
	assert.True(t, result.StartTime.Equal(*got.LastHeartbeatAt))
}

func TestIngester_TheLocationKeepsThePendingStatusOfItsHeartbeat(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 3}
	ti.from(t, probeA, m, up)

	beat := ti.from(t, probeA, m, down)

	assert.Equal(t, pending, beat.status)
	require.NotNil(t, ti.locations.states[probeA].Status)
	assert.Equal(t, pending, *ti.locations.states[probeA].Status, "the row says what the heartbeat says")
}

func TestIngester_AMonitorIsInMaintenanceWhileAnyLocationIs(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)

	steps := []struct {
		probe  string
		check  shared.MonitorStatus
		events []string
		status shared.MonitorStatus // the monitor's, when it changed
	}{
		{probeA, maintenance, []string{changed}, maintenance}, // Up to Maintenance, though B is Up
		{probeB, up, []string{}, 0},                           // B has not been told of the window yet
		{probeB, maintenance, []string{}, 0},
		{probeA, up, []string{}, 0}, // the window ended, but B has not found out
		{probeB, up, []string{changed}, up},
	}
	for i, step := range steps {
		beat := ti.from(t, step.probe, m, step.check)

		assert.Equal(t, step.check, beat.status, "step %d: the heartbeat keeps the location's status", i+1)
		assert.Equal(t, step.events, beat.events, "step %d", i+1)
		if len(step.events) > 0 {
			assert.Equal(t, step.status, beat.published[len(beat.published)-1].Status, "step %d", i+1)
		}
		assert.False(t, beat.notified, "step %d: a maintenance window raises no notification", i+1)
	}
}

// A location that is Down stays Down on a Pending result, and so does the monitor: the other
// location being Up does not make a Pending result an Up one.
func TestIngester_APendingResultFromADownLocationKeepsTheMonitorDown(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.from(t, probeA, m, down) // the monitor is Down

	beat := ti.from(t, probeA, m, pending)

	assert.Equal(t, down, beat.status, "A is Down until it is Up")
	assert.Equal(t, 2, beat.retries, "and the Pending result is a failed check")
	assert.False(t, beat.important)
	assert.False(t, beat.notified)
	assert.Empty(t, beat.events, "the monitor stays Down")
	assert.Len(t, beat.published, 2, "and no status change is published for it")
	require.NotNil(t, ti.locations.states[probeA].Status)
	assert.Equal(t, down, *ti.locations.states[probeA].Status)

	recovered := ti.from(t, probeA, m, up)

	assert.True(t, recovered.important, "the Up result takes the monitor Up")
	assert.True(t, recovered.notified, "and that Down to Up is the one notified")
	assert.Equal(t, []string{changed, notified}, recovered.events)
}

// The event, which sets the monitor's status, carries the monitor's status, whatever the heartbeat
// of the result that moved it says.
func TestIngester_StatusChangedCarriesTheMonitorsStatus(t *testing.T) {
	const probeC = "probe-c"
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.from(t, probeC, m, up)
	ti.from(t, probeB, m, down) // the monitor is Down
	ti.locations.setOffline(probeB)

	// A's next result is Pending, and C is Up: the monitor is Up, whatever A's heartbeat says
	beat := ti.from(t, probeA, m, pending)

	assert.Equal(t, pending, beat.status, "the heartbeat keeps the location's status")
	require.Len(t, beat.published, 3)
	assert.Equal(t, up, beat.published[2].Status, "while the event carries the monitor's")
	assert.Equal(t, probeA, beat.published[2].ProbeID)
}

// A Down monitor leaves Down for Up or Maintenance only, whatever its locations add up to. Here the
// location rule is edited so that it is no longer met, and the locations add up to Pending: the
// monitor stays Down until an Up result takes it Up, and that Down to Up is the one notified.
func TestIngester_ADownMonitorNeverGoesPendingWhenItsLocationsAddUpToPending(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 1}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.from(t, probeA, m, down)
	aDown := ti.from(t, probeA, m, down)
	require.Equal(t, []string{changed, notified}, aDown.events, "the monitor is Down")
	bRetrying := ti.from(t, probeB, m, down)
	require.Equal(t, pending, bRetrying.status, "B has a retry left")
	require.Empty(t, bRetrying.events, "and the monitor stays Down")

	m.locationRule = shared.LocationRuleAll // A is Down, B is Pending: not all of them are Down
	aStillDown := ti.from(t, probeA, m, down)

	assert.Equal(t, down, aStillDown.status)
	assert.False(t, aStillDown.important)
	assert.False(t, aStillDown.notified)
	assert.Empty(t, aStillDown.events, "the monitor does not go Pending")
	require.Len(t, aStillDown.published, 2)

	bUp := ti.from(t, probeB, m, up)

	assert.True(t, bUp.important, "the monitor went Down to Up")
	assert.True(t, bUp.notified)
	assert.Equal(t, []string{changed, notified}, bUp.events)
	require.Len(t, bUp.published, 3)
	assert.Equal(t, up, bUp.published[2].Status)
}

func TestIngester_ALocationWithNoRowIsStillJudged(t *testing.T) {
	// A monitor can have lost its row for the probe by being moved
	ti := newTestIngester(runningOn())
	m := monitorSettings{maxRetries: 1}

	steps := []struct {
		check  shared.MonitorStatus
		want   shared.MonitorStatus
		events []string
	}{
		{up, up, []string{changed, notified}},
		{down, pending, []string{changed}},
		{down, down, []string{changed, notified}},
		{down, down, []string{}},
		{up, up, []string{changed, notified}},
	}
	for i, step := range steps {
		beat := ti.from(t, "", m, step.check)

		assert.Equal(t, step.want, beat.status, "step %d", i+1)
		assert.Equal(t, step.events, beat.events, "step %d", i+1)
	}
}

func TestIngester_AFailedRecordFailsTheIngestBeforeAnythingIsStored(t *testing.T) {
	ti := newTestIngester()
	ti.locations.err = assert.AnError
	result := checkResult(monitorSettings{}, down)

	err := ti.service.Ingest(context.Background(), &result)

	assert.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, ti.heartbeats.stored)
	assert.Empty(t, ti.bus.TakeTopics())
}

func TestIngester_AFailedHeartbeatLeavesTheLocationAsItWas(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.heartbeats.createErr = assert.AnError
	result := checkResult(m, down)
	result.ProbeID = probeA

	err := ti.service.Ingest(context.Background(), &result)
	require.ErrorIs(t, err, assert.AnError)
	ti.heartbeats.createErr = nil
	retried := ti.from(t, probeA, m, down)

	require.NotNil(t, ti.locations.states[probeA].Status)
	assert.Equal(t, down, *ti.locations.states[probeA].Status)
	assert.Equal(t, []string{changed, notified}, retried.events, "the retry still finds the monitor going Down, so its notification is not lost")
}

// A result moves the monitor to Down only when its own location is Down, and to Up only when its
// own location is Up: it is the heartbeat of that result that is saved as important and notified,
// and it must say what the notification says.
func TestIngester_AnUpResultNeverWentDownTheMonitor(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeB, m, up)
	aDown := ti.from(t, probeA, m, down)
	require.Equal(t, []string{changed, notified}, aDown.events, "the monitor is Down")
	ti.locations.setOffline(probeA)
	recovered := ti.from(t, probeB, m, up)
	require.Equal(t, []string{changed, notified}, recovered.events, "A is unknown, and B is Up: the monitor is Up")
	delete(ti.locations.offline, probeA) // A comes back, with the Down it had

	bUp := ti.from(t, probeB, m, up)

	assert.Equal(t, up, bUp.status)
	assert.False(t, bUp.important, "an Up heartbeat is not the monitor going Down")
	assert.False(t, bUp.notified)
	assert.Empty(t, bUp.events, "the monitor is still Up as far as anyone was told")
	require.Len(t, bUp.published, 3)

	aStillDown := ti.from(t, probeA, m, down)

	assert.Equal(t, down, aStillDown.status)
	assert.True(t, aStillDown.important, "A's own Down result carries the change")
	assert.True(t, aStillDown.notified)
	assert.Equal(t, []string{changed, notified}, aStillDown.events)
	require.Len(t, aStillDown.published, 4)
	assert.Equal(t, down, aStillDown.published[3].Status)
}

func TestIngester_TheMonitorRecoversOnlyOnAnUpResult(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{}
	ti.from(t, probeA, m, up)
	ti.from(t, probeB, m, up)
	ti.from(t, probeA, m, down)
	ti.from(t, probeB, m, down)
	ti.locations.setOffline(probeA)

	bStillDown := ti.from(t, probeB, m, down)
	assert.Empty(t, bStillDown.events, "A being offline doesn't end B's outage")

	delete(ti.locations.offline, probeA)
	aBack := ti.from(t, probeA, m, up)
	assert.Empty(t, aBack.events, "A is Up, and B is still Down")

	bUp := ti.from(t, probeB, m, up)

	assert.Equal(t, up, bUp.status)
	assert.True(t, bUp.important, "the Up result is the one that recovers the monitor")
	assert.True(t, bUp.notified)
	assert.Equal(t, []string{changed, notified}, bUp.events)
	assert.Equal(t, up, bUp.published[len(bUp.published)-1].Status)
}

// Another location's retries don't take a Down monitor to Pending: it stays Down until an Up
// result recovers it, and that recovery is the one that is notified.
func TestIngester_ADownMonitorStaysDownWhileAnotherLocationRetries(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	m := monitorSettings{maxRetries: 1}
	ti.from(t, probeB, m, up)
	ti.from(t, probeA, m, up)
	ti.from(t, probeA, m, down)
	aDown := ti.from(t, probeA, m, down)
	require.Equal(t, []string{changed, notified}, aDown.events, "the monitor is Down")
	ti.locations.setOffline(probeA)

	bRetrying := ti.from(t, probeB, m, down)

	assert.Equal(t, pending, bRetrying.status, "B has a retry left")
	assert.Empty(t, bRetrying.events, "B's retry doesn't move the Down monitor")
	require.Len(t, bRetrying.published, 2)

	bUp := ti.from(t, probeB, m, up)

	assert.Equal(t, up, bUp.status)
	assert.True(t, bUp.important, "the monitor went Down to Up")
	assert.True(t, bUp.notified)
	assert.Equal(t, []string{changed, notified}, bUp.events)
	require.Len(t, bUp.published, 3)
	assert.Equal(t, up, bUp.published[2].Status)
}
