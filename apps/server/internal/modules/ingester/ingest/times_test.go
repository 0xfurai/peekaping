package ingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUTCSpan_EndIsTheStartPlusTheMeasuredDuration(t *testing.T) {
	kyiv := time.FixedZone("EEST", 3*60*60)
	start := time.Date(2026, 10, 17, 15, 0, 0, 0, kyiv)

	gotStart, gotEnd := UTCSpan(start, start.Add(12*time.Millisecond))

	assert.Equal(t, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), gotStart)
	assert.Equal(t, time.Date(2026, 10, 17, 12, 0, 0, 12_000_000, time.UTC), gotEnd)
}

func TestUTCSpan_ATimeThatWasNeverSetPassesThroughUnchanged(t *testing.T) {
	now := time.Now()

	zeroStart, endOfZeroStart := UTCSpan(time.Time{}, now)
	start, zeroEnd := UTCSpan(now, time.Time{})
	bothStart, bothEnd := UTCSpan(time.Time{}, time.Time{})

	assert.True(t, zeroStart.IsZero(), "a zero start stays zero")
	assert.True(t, endOfZeroStart.Equal(now), "the end stays the end it was")
	assert.True(t, start.Equal(now))
	assert.True(t, zeroEnd.IsZero(), "a zero end stays zero, not 292 years before the start")
	assert.True(t, bothStart.IsZero())
	assert.True(t, bothEnd.IsZero())
}
