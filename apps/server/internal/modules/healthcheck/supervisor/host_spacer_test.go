package supervisor

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a clock the test moves
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestHostSpacer_SpacesChecksToOneHostAtTheRate(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)

	var waits []time.Duration
	for range 5 {
		turn := spacer.Reserve([]string{"example.com"}, 10*time.Second)
		require.False(t, turn.Denied)
		waits = append(waits, turn.Wait)
	}

	assert.Equal(t, []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}, waits,
		"60 a minute is one a second, and the first has its turn now")
}

func TestHostSpacer_SkipsACheckWhoseTurnIsBeyondMaxWaitAndGivesTheTurnBack(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for range 5 {
		require.False(t, spacer.Reserve([]string{"example.com"}, 4*time.Second).Denied)
	}

	turn := spacer.Reserve([]string{"example.com"}, 4*time.Second)
	assert.True(t, turn.Denied, "its turn is 5s away")
	assert.Equal(t, "example.com", turn.Host)

	again := spacer.Reserve([]string{"example.com"}, 4*time.Second)
	assert.True(t, again.Denied, "the skipped check took no turn: the next one's is still 5s away, not 6s")

	clock.Advance(time.Second)
	later := spacer.Reserve([]string{"example.com"}, 4*time.Second)
	require.False(t, later.Denied, "a second on, the turn is 4s away")
	assert.Equal(t, 4*time.Second, later.Wait, "and still follows the fifth: the skips left no gap behind them")
}

func TestHostSpacer_HostsDoNotAffectEachOther(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	require.Zero(t, spacer.Reserve([]string{"a.example.com"}, time.Second).Wait)
	require.Equal(t, time.Second, spacer.Reserve([]string{"a.example.com"}, 2*time.Second).Wait)

	turn := spacer.Reserve([]string{"b.example.com"}, time.Second)

	assert.False(t, turn.Denied)
	assert.Zero(t, turn.Wait, "b has its own bucket")
}

func TestHostSpacer_HostsAreCaseInsensitiveAndIgnoreTheTrailingDot(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	require.Zero(t, spacer.Reserve([]string{"Example.COM"}, time.Second).Wait)

	assert.Equal(t, time.Second, spacer.Reserve([]string{"example.com."}, 2*time.Second).Wait)
}

func TestHostSpacer_AMonitorWithTwoHostsWaitsForBoth(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for range 3 {
		require.False(t, spacer.Reserve([]string{"slow.example.com"}, 10*time.Second).Denied)
	}
	require.Zero(t, spacer.Reserve([]string{"fast.example.com"}, time.Second).Wait)

	turn := spacer.Reserve([]string{"fast.example.com", "slow.example.com"}, 10*time.Second)

	assert.False(t, turn.Denied)
	assert.Equal(t, 3*time.Second, turn.Wait, "slow's turn is 3s away and fast's 1s")
}

func TestHostSpacer_ADeniedMonitorTakesNoTurnOnItsOtherHosts(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for range 6 {
		require.False(t, spacer.Reserve([]string{"busy.example.com"}, 10*time.Second).Denied)
	}

	turn := spacer.Reserve([]string{"idle.example.com", "busy.example.com"}, 2*time.Second)
	require.True(t, turn.Denied)
	assert.Equal(t, "busy.example.com", turn.Host, "the host whose turn is too far")

	assert.Zero(t, spacer.Reserve([]string{"idle.example.com"}, time.Second).Wait, "idle was given its turn back")
}

func TestHostSpacer_ListsAHostOnceWhenAMonitorNamesItTwice(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)

	turn := spacer.Reserve([]string{"example.com", "EXAMPLE.com"}, time.Second)

	assert.Zero(t, turn.Wait)
	assert.Equal(t, time.Second, spacer.Reserve([]string{"example.com"}, 2*time.Second).Wait, "one turn was taken, not two")
}

func TestHostSpacer_OffWaitsForNothingAndKeepsNothing(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(0, clock.Now)

	for range 100 {
		turn := spacer.Reserve([]string{"example.com"}, 0)
		require.False(t, turn.Denied)
		require.Zero(t, turn.Wait)
	}

	assert.False(t, spacer.Enabled())
	assert.Zero(t, spacer.tracked(), "it costs no memory either")
}

func TestHostSpacer_ChangingTheRateTakesEffect(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	require.Zero(t, spacer.Reserve([]string{"example.com"}, time.Second).Wait)

	spacer.SetPerMinute(120)
	assert.Equal(t, 120, spacer.PerMinute())
	assert.Equal(t, 500*time.Millisecond, spacer.Reserve([]string{"example.com"}, time.Second).Wait, "120 a minute is one every half second")

	spacer.SetPerMinute(0)
	assert.False(t, spacer.Enabled())
	assert.Zero(t, spacer.Reserve([]string{"example.com"}, 0).Wait, "off")
	assert.Zero(t, spacer.tracked())

	spacer.SetPerMinute(60)
	assert.Zero(t, spacer.Reserve([]string{"example.com"}, time.Second).Wait, "turned on again, a host starts fresh")
	assert.Equal(t, time.Second, spacer.Reserve([]string{"example.com"}, 2*time.Second).Wait)
}

func TestHostSpacer_ForgetsHostsThatHaveBeenIdleAndBoundsWhatItKeeps(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	spacer.maxHosts = 3

	for _, host := range []string{"a", "b", "c"} {
		spacer.Reserve([]string{host}, time.Second)
	}
	assert.Equal(t, 3, spacer.tracked())

	clock.Advance(2 * time.Minute)
	spacer.Reserve([]string{"d"}, time.Second)
	assert.Equal(t, 1, spacer.tracked(), "a, b and c were idle for a while and are gone")

	for _, host := range []string{"e", "f", "g", "h"} {
		spacer.Reserve([]string{host}, time.Second)
	}
	assert.LessOrEqual(t, spacer.tracked(), 3, "the map holds at most maxHosts")
}

func TestHostSpacer_MakingRoomKeepsTheBucketsOfTheCheckItIsFor(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	spacer.maxHosts = 2
	for range 6 {
		require.False(t, spacer.Reserve([]string{"busy.example.com"}, time.Minute).Denied)
	}
	for range 10 {
		require.False(t, spacer.Reserve([]string{"busier.example.com"}, time.Minute).Denied)
	}

	// busy is the bucket idle the soonest, so it is the oldest when new needs room
	turn := spacer.Reserve([]string{"busy.example.com", "new.example.com"}, 2*time.Second)

	require.True(t, turn.Denied)
	assert.Equal(t, "busy.example.com", turn.Host)
	assert.Equal(t, 1, turn.ReportSkipped)
	assert.Equal(t, 6*time.Second, spacer.Reserve([]string{"busy.example.com"}, time.Minute).Wait,
		"busy kept its bucket: busier made room")
}

func TestHostSpacer_ReportsSkippedChecksAtMostOncePerMinutePerHost(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for range 5 {
		spacer.Reserve([]string{"example.com"}, 4*time.Second)
	}

	first := spacer.Reserve([]string{"example.com"}, time.Second)
	require.True(t, first.Denied)
	assert.Equal(t, 1, first.ReportSkipped, "the first skip is reported")

	var reported []int
	for range 5 {
		turn := spacer.Reserve([]string{"example.com"}, time.Second)
		require.True(t, turn.Denied)
		reported = append(reported, turn.ReportSkipped)
	}
	assert.Equal(t, []int{0, 0, 0, 0, 0}, reported, "then none for a minute")

	clock.Advance(59 * time.Second)
	spacer.Reserve([]string{"example.com"}, time.Hour) // keeps the host alive and takes a turn
	clock.Advance(2 * time.Second)
	for range 3 {
		spacer.Reserve([]string{"example.com"}, time.Hour)
	}
	turn := spacer.Reserve([]string{"example.com"}, time.Millisecond)
	require.True(t, turn.Denied)
	assert.Equal(t, 6, turn.ReportSkipped, "a minute on: the 5 skipped since the first, and this one")
}
