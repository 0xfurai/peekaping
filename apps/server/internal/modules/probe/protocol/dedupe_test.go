package protocol

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func TestAcceptedWindow_ExpiresAfterTTL(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	require.True(t, window.Claim("probe-1", "result-1", epoch))

	assert.False(t, window.Claim("probe-1", "result-1", epoch), "an id is taken as soon as it is claimed")
	assert.False(t, window.Claim("probe-1", "result-1", epoch.Add(time.Hour-time.Nanosecond)), "and until the hour is out")
	assert.True(t, window.Claim("probe-1", "result-1", epoch.Add(time.Hour)), "an id is free again an hour after it was claimed")
}

func TestAcceptedWindow_KeepsEachProbesIdsApart(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	require.True(t, window.Claim("probe-1", "result-1", epoch))

	assert.True(t, window.Claim("probe-2", "result-1", epoch), "the ids are a probe's own, so another probe may use the same one")
	assert.True(t, window.Claim("probe-1", "result-2", epoch))
}

func TestAcceptedWindow_ClaimingAgainDoesNotExtendTheWindow(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	require.True(t, window.Claim("probe-1", "result-1", epoch))
	require.False(t, window.Claim("probe-1", "result-1", epoch.Add(30*time.Minute)))

	assert.True(t, window.Claim("probe-1", "result-1", epoch.Add(time.Hour)), "the id was claimed at the first claim")
}

// Two requests that claim an id at once are told apart by the window, before either has stored
// anything
func TestAcceptedWindow_ClaimIsExclusive(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	var won atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if window.Claim("probe-1", "result-1", epoch) {
				won.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.EqualValues(t, 1, won.Load(), "exactly one claim of the id succeeds")
}

func TestAcceptedWindow_ReleasedIdCanBeClaimedAgain(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	require.True(t, window.Claim("probe-1", "failed", epoch))
	require.True(t, window.Claim("probe-1", "stored", epoch))

	window.Release("probe-1", "failed")

	assert.True(t, window.Claim("probe-1", "failed", epoch), "the probe's retry of a result that was not stored is accepted")
	assert.False(t, window.Claim("probe-1", "stored", epoch), "and what was stored stays taken")
}

func TestAcceptedWindow_ReleaseLeavesOthersAlone(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	require.True(t, window.Claim("probe-1", "result-1", epoch))

	window.Release("probe-2", "result-1")
	window.Release("probe-1", "never-claimed")

	assert.False(t, window.Claim("probe-1", "result-1", epoch), "another probe's release and an id nobody claimed change nothing")
	window.Release("probe-1", "result-1")
	assert.Zero(t, window.probeCount(), "a probe left with no ids is forgotten")
}

// A window that held every id it had ever seen would grow for as long as the server runs
func TestAcceptedWindow_ForgetsExpiredIdsAndIdleProbes(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	for i := range 100 {
		window.Claim("idle-probe", "result-"+strconv.Itoa(i), epoch)
	}
	window.Claim("busy-probe", "old", epoch)

	later := epoch.Add(2 * time.Hour)
	window.Claim("busy-probe", "new", later)

	assert.Equal(t, 1, window.probeCount(), "the probe that went quiet is forgotten")
	assert.Equal(t, 1, window.idCount(), "and so is the old id of the one that is not")
	assert.False(t, window.Claim("busy-probe", "new", later))
	assert.True(t, window.Claim("busy-probe", "old", later))
}
