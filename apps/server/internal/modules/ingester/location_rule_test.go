package ingester

import (
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const probeC = "probe-c"

var (
	allDown       = monitorSettings{locationRule: shared.LocationRuleAll}
	twoOfThree    = monitorSettings{locationRule: shared.LocationRuleAtLeast, locationThreshold: 2}
	statusChanged = []string{changed, notified}
)

// bringUp reports an Up result from each probe, so that the monitor is Up on all of them
func (ti *testIngester) bringUp(t *testing.T, m monitorSettings, probes ...string) {
	t.Helper()
	first := ti.from(t, probes[0], m, up)
	require.Equal(t, statusChanged, first.events, "the monitor's first heartbeat")
	for _, probeID := range probes[1:] {
		require.Empty(t, ti.from(t, probeID, m, up).events)
	}
}

func TestIngester_AtLeastTwoOfThree_OneDownLocationLeavesTheMonitorUp(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, twoOfThree, probeA, probeB, probeC)

	one := ti.from(t, probeA, twoOfThree, down)

	assert.Equal(t, down, one.status, "the heartbeat keeps the location's status")
	assert.Empty(t, one.events, "one Down location of the two it takes")
	assert.False(t, one.important)
	assert.False(t, one.notified)
	require.NotNil(t, ti.locations.recorded)
	assert.Equal(t, up, *ti.locations.recorded, "the monitor is Up")
}

func TestIngester_AtLeastTwoOfThree_TwoDownLocationsTakeTheMonitorDownWithOneNotification(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, twoOfThree, probeA, probeB, probeC)
	ti.from(t, probeA, twoOfThree, down)

	second := ti.from(t, probeB, twoOfThree, down)
	third := ti.from(t, probeC, twoOfThree, down)

	assert.Equal(t, statusChanged, second.events, "one Down notification when the second location goes Down")
	assert.True(t, second.important)
	assert.True(t, second.notified)
	assert.Equal(t, down, second.published[len(second.published)-1].Status)
	assert.Empty(t, third.events, "and none for the third")
	assert.False(t, third.notified)

	recovered := ti.from(t, probeA, twoOfThree, up)
	assert.Empty(t, recovered.events, "two Down locations are still two")
	back := ti.from(t, probeB, twoOfThree, up)
	assert.Equal(t, statusChanged, back.events, "one Up notification when only one is left")
	assert.Equal(t, up, back.published[len(back.published)-1].Status)
}

func TestIngester_AtLeastTwoOfThree_AnOfflineDownLocationDoesNotCount(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, twoOfThree, probeA, probeB, probeC)
	ti.from(t, probeB, twoOfThree, down)
	ti.locations.setOffline(probeB)

	oneOnline := ti.from(t, probeA, twoOfThree, down)

	assert.Empty(t, oneOnline.events, "A is Down, B is Down but offline: one counts, and two are needed")
	assert.Equal(t, up, *ti.locations.recorded)

	twoOnline := ti.from(t, probeC, twoOfThree, down)
	assert.Equal(t, statusChanged, twoOnline.events, "A and C are Down and online")
}

func TestIngester_AtLeastThreeOfTwo_NeverGoesDownBecauseTooFewLocationsCanReport(t *testing.T) {
	m := monitorSettings{locationRule: shared.LocationRuleAtLeast, locationThreshold: 3}
	ti := newTestIngester(runningOn(probeA, probeB))
	ti.bringUp(t, m, probeA, probeB)

	ti.from(t, probeA, m, down)
	both := ti.from(t, probeB, m, down)

	assert.False(t, both.notified, "N is fixed: two locations can't make three")
	assert.NotContains(t, both.events, notified)
	require.NotNil(t, ti.locations.recorded)
	assert.Equal(t, pending, *ti.locations.recorded, "no location is Up and the rule isn't met: Pending")
}

func TestIngester_All_TheMonitorIsDownOnlyWhenEveryLocationIs(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, allDown, probeA, probeB, probeC)

	aDown := ti.from(t, probeA, allDown, down)
	bDown := ti.from(t, probeB, allDown, down)
	cDown := ti.from(t, probeC, allDown, down)

	assert.Empty(t, aDown.events)
	assert.Empty(t, bDown.events)
	assert.Equal(t, statusChanged, cDown.events, "one Down notification when the last location goes Down")
	assert.True(t, cDown.notified)

	aUp := ti.from(t, probeA, allDown, up)
	assert.Equal(t, statusChanged, aUp.events, "one Up notification when any location is Up again")
	assert.Equal(t, up, aUp.published[len(aUp.published)-1].Status)
}

func TestIngester_All_ALocationThatHasNotReportedDoesNotCount(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.from(t, probeA, allDown, up)
	ti.from(t, probeB, allDown, up)

	aDown := ti.from(t, probeA, allDown, down)
	bDown := ti.from(t, probeB, allDown, down)

	assert.Empty(t, aDown.events, "B is Up")
	assert.Equal(t, statusChanged, bDown.events, "A and B are every location that has reported, and C hasn't")

	cUp := ti.from(t, probeC, allDown, up)
	assert.Equal(t, statusChanged, cUp.events, "C reports Up: not every location is Down any more")
}

func TestIngester_All_AnOfflineLocationDoesNotCount(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	ti.bringUp(t, allDown, probeA, probeB)
	ti.locations.setOffline(probeB)

	aDown := ti.from(t, probeA, allDown, down)

	assert.Equal(t, statusChanged, aDown.events, "A is the only location that counts, and it is Down")
}

func TestIngester_All_OtherStatusesFollowTheUsualOrder(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	ti.bringUp(t, allDown, probeA, probeB)

	ti.from(t, probeA, allDown, down)
	inMaintenance := ti.from(t, probeB, allDown, maintenance)

	assert.Equal(t, []string{changed}, inMaintenance.events, "A is Down and B is in maintenance: not all Down, so Maintenance")
	assert.Equal(t, maintenance, inMaintenance.published[len(inMaintenance.published)-1].Status)
	assert.False(t, inMaintenance.notified)
}

func TestIngester_AnEmptyRuleIsAny(t *testing.T) {
	ti := newTestIngester(runningOn(probeA, probeB))
	explicit := monitorSettings{locationRule: shared.LocationRuleAny}
	ti.bringUp(t, explicit, probeA, probeB)

	aDown := ti.from(t, probeA, explicit, down)

	assert.Equal(t, statusChanged, aDown.events, "one Down location is enough")
}

func TestIngester_AtLeastTwoOfThree_ADownLocationDoesNotResendWhileTheMonitorIsUp(t *testing.T) {
	m := monitorSettings{locationRule: shared.LocationRuleAtLeast, locationThreshold: 2, resendInterval: 3}
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, m, probeA, probeB, probeC)

	for i := 1; i <= 7; i++ {
		beat := ti.from(t, probeA, m, down)

		assert.Empty(t, beat.events, "Down heartbeat %d of A: the monitor is Up, so there is nothing to resend", i)
		assert.False(t, beat.notified, "Down heartbeat %d of A", i)
	}
	require.NotNil(t, ti.locations.recorded)
	assert.Equal(t, up, *ti.locations.recorded)
}

func TestIngester_All_ADownLocationDoesNotResendWhileTheMonitorIsPending(t *testing.T) {
	m := monitorSettings{maxRetries: 1, resendInterval: 1, locationRule: shared.LocationRuleAll}
	ti := newTestIngester(runningOn(probeA, probeB))
	ti.bringUp(t, m, probeA, probeB)
	ti.from(t, probeB, m, down) // B retries
	ti.from(t, probeA, m, down) // A retries
	require.NotNil(t, ti.locations.recorded)
	require.Equal(t, pending, *ti.locations.recorded, "A and B are both retrying: the monitor is Pending")

	for i := 1; i <= 4; i++ {
		beat := ti.from(t, probeA, m, down)

		assert.Equal(t, down, beat.status, "Down heartbeat %d of A: A's retries are used up", i)
		assert.Equal(t, 0, beat.downCount, "Down heartbeat %d of A: the monitor is not Down, so it counts nothing", i)
		assert.False(t, beat.notified, "Down heartbeat %d of A", i)
		assert.Empty(t, beat.events, "Down heartbeat %d of A: B is still retrying, so the monitor is Pending and there is nothing to resend", i)
	}
	assert.Equal(t, pending, *ti.locations.recorded, "the monitor is still Pending")
}

func TestIngester_AtLeastTwoOfThree_ResendsFollowTheFirstDownLocationWhileTheMonitorIsDown(t *testing.T) {
	m := monitorSettings{locationRule: shared.LocationRuleAtLeast, locationThreshold: 2, resendInterval: 2}
	ti := newTestIngester(runningOn(probeA, probeB, probeC))
	ti.bringUp(t, m, probeA, probeB, probeC)
	ti.from(t, probeA, m, down)
	goesDown := ti.from(t, probeB, m, down)
	require.Equal(t, statusChanged, goesDown.events, "A and B are Down: the monitor is Down")

	aFirst := ti.from(t, probeA, m, down)
	bFirst := ti.from(t, probeB, m, down)
	aSecond := ti.from(t, probeA, m, down)
	bSecond := ti.from(t, probeB, m, down)

	assert.Empty(t, aFirst.events)
	assert.Empty(t, bFirst.events, "B is not the first Down location, so its heartbeats don't count")
	assert.Equal(t, []string{notified}, aSecond.events, "A resends on its second Down heartbeat")
	assert.Empty(t, bSecond.events)
}
