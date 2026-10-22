package protocol

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var epoch = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func TestAcceptedWindow_ExpiresAfterTTL(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	window.Add("probe-1", "result-1", epoch)

	assert.True(t, window.Seen("probe-1", "result-1", epoch), "an id is seen as soon as it is added")
	assert.True(t, window.Seen("probe-1", "result-1", epoch.Add(time.Hour-time.Nanosecond)), "and until the hour is out")
	assert.False(t, window.Seen("probe-1", "result-1", epoch.Add(time.Hour)), "an id is accepted again an hour after it was")
}

func TestAcceptedWindow_KeepsEachProbesIdsApart(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	window.Add("probe-1", "result-1", epoch)

	assert.False(t, window.Seen("probe-2", "result-1", epoch), "the ids are a probe's own, so another probe may use the same one")
	assert.False(t, window.Seen("probe-1", "result-2", epoch))
}

func TestAcceptedWindow_AddingAgainDoesNotExtendTheWindow(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	window.Add("probe-1", "result-1", epoch)
	window.Add("probe-1", "result-1", epoch.Add(30*time.Minute))

	assert.False(t, window.Seen("probe-1", "result-1", epoch.Add(time.Hour)), "the id was accepted at the first add")
}

// A window that held every id it had ever seen would grow for as long as the server runs
func TestAcceptedWindow_ForgetsExpiredIdsAndIdleProbes(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	for i := range 100 {
		window.Add("idle-probe", "result-"+strconv.Itoa(i), epoch)
	}
	window.Add("busy-probe", "old", epoch)

	later := epoch.Add(2 * time.Hour)
	window.Add("busy-probe", "new", later)

	assert.Equal(t, 1, window.probeCount(), "the probe that went quiet is forgotten")
	assert.Equal(t, 1, window.idCount(), "and so is the old id of the one that is not")
	assert.True(t, window.Seen("busy-probe", "new", later))
	assert.False(t, window.Seen("busy-probe", "old", later))
}
