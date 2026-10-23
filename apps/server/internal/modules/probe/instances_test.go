package probe_test

import (
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlineInstances_WindowOf60s(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	instances := []*probe.Instance{
		{ID: "c-just-now", LastSeenAt: now},
		{ID: "a-59s", LastSeenAt: now.Add(-59 * time.Second)},
		{ID: "d-60s", LastSeenAt: now.Add(-60 * time.Second)},
		{ID: "e-61s", LastSeenAt: now.Add(-61 * time.Second)},
		{ID: "b-an-hour", LastSeenAt: now.Add(-time.Hour)},
		{ID: "f-ahead", LastSeenAt: now.Add(3 * time.Second)},
	}

	got := probe.OnlineInstances(instances, now, probe.InstanceOnlineWindow)

	assert.Equal(t, []string{"a-59s", "c-just-now", "d-60s", "f-ahead"}, got, "seen within the last 60 s, sorted; a clock a little ahead of ours is online too")
	assert.Equal(t, 60*time.Second, probe.InstanceOnlineWindow)
}

// A retired instance is out of the dealing the moment it says bye, even when the clock reads the
// same instant again: the boundary of the window counts as online, so the retired time is a
// second beyond it.
func TestRetiredAt_IsOutsideTheOnlineWindowAtTheInstantOfBye(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	retired := []*probe.Instance{{ID: "retired", LastSeenAt: probe.RetiredAt(now)}, {ID: "staying", LastSeenAt: now}}

	assert.Equal(t, []string{"staying"}, probe.OnlineInstances(retired, now, probe.InstanceOnlineWindow), "at the instant of bye")
	assert.Equal(t, []string{"staying"}, probe.OnlineInstances(retired, now.Add(time.Nanosecond), probe.InstanceOnlineWindow), "and after it")
	assert.WithinDuration(t, now.Add(-probe.InstanceOnlineWindow), probe.RetiredAt(now), 2*time.Second, "about a window ago")
}

// A graceful restart says bye and then hello again. Bye leaves the instance last seen a window and
// a second ago, so the probe's time to come back is the shortest PROBE_OFFLINE_AFTER less that
// backdate. The sweep must not be able to announce the probe offline in the meantime, so the room
// that is left has to cover two passes of the sweep (less the second RetiredAt adds on top of the
// window, which is why it is not quite 60 s).
func TestRetiredAt_LeavesRoomUnderTheSmallestOfflineAfter(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	backdate := now.Sub(probe.RetiredAt(now))
	require.Equal(t, probe.InstanceOnlineWindow+time.Second, backdate)

	room := probe.MinOfflineAfter - backdate

	assert.GreaterOrEqual(t, room, 2*probe.DefaultSweepInterval-time.Second,
		"a retired instance has %s to come back before the sweep can set its probe offline", room)
}

// The sweep forgets an instance after the retention, so a probe could not stay online on an instance
// older than that. PROBE_OFFLINE_AFTER is capped at the retention so that the sweep never sets a probe
// online on an instance it is about to prune.
func TestInstanceRetention_IsTheCapOfProbeOfflineAfter(t *testing.T) {
	assert.Equal(t, config.MaxProbeOfflineAfter, probe.DefaultInstanceRetention)
}

func TestOnlineInstances_NoneIsAnEmptyList(t *testing.T) {
	require.NotNil(t, probe.OnlineInstances(nil, time.Now(), time.Minute))
}
