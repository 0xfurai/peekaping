package supervisor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostSpacer_HostsThatAreBlankTakeNoTurn(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)

	for _, hosts := range [][]string{nil, {}, {""}, {"  ", "."}, {"[]"}} {
		turn := spacer.Reserve(hosts, time.Second)

		assert.Equal(t, Turn{}, turn, "%q", hosts)
	}
	assert.Zero(t, spacer.tracked(), "no bucket was made")
}

func TestHostSpacer_BlankHostsAreLeftOutOfAMonitorWithOthers(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	require.Zero(t, spacer.Reserve([]string{"", "example.com", " . "}, time.Second).Wait)

	assert.Equal(t, 1, spacer.tracked())
	assert.Equal(t, time.Second, spacer.Reserve([]string{"example.com"}, 2*time.Second).Wait, "one turn was taken on it")
}

func TestHostSpacer_NamesTheHostWhoseTurnIsFarthest(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for range 3 {
		require.False(t, spacer.Reserve([]string{"slow.example.com"}, 10*time.Second).Denied)
	}
	for range 2 {
		require.False(t, spacer.Reserve([]string{"medium.example.com"}, 10*time.Second).Denied)
	}

	for _, hosts := range [][]string{
		{"slow.example.com", "medium.example.com"},
		{"medium.example.com", "slow.example.com"},
		{"medium.example.com", "idle.example.com", "SLOW.example.com."},
	} {
		turn := spacer.Reserve(hosts, time.Second)

		assert.True(t, turn.Denied, "%q", hosts)
		assert.Equal(t, "slow.example.com", turn.Host, "%q", hosts)
	}
}

func TestHostSpacer_NamesTheFirstOfTheHostsWhoseTurnsAreEquallyFar(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	for _, host := range []string{"a.example.com", "b.example.com"} {
		for range 3 {
			require.False(t, spacer.Reserve([]string{host}, 10*time.Second).Denied)
		}
	}

	assert.Equal(t, "b.example.com", spacer.Reserve([]string{"b.example.com", "a.example.com"}, time.Second).Host)
	assert.Equal(t, "a.example.com", spacer.Reserve([]string{"a.example.com", "b.example.com"}, time.Second).Host)
}

func TestHostSpacer_AWaitExactlyAsLongAsMaxWaitIsAllowed(t *testing.T) {
	clock := newFakeClock()
	spacer := newHostSpacer(60, clock.Now)
	require.False(t, spacer.Reserve([]string{"example.com"}, time.Second).Denied)

	turn := spacer.Reserve([]string{"example.com"}, time.Second)

	assert.False(t, turn.Denied)
	assert.Equal(t, time.Second, turn.Wait)
}
