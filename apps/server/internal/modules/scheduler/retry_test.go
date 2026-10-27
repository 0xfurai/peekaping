package scheduler

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withRetries(maxRetries, retryInterval int) monitorOption {
	return func(m *shared.Monitor) {
		m.MaxRetries = maxRetries
		m.RetryInterval = retryInterval
	}
}

// startMidSecond starts the scheduler half a second into a second, so that a check at the retry
// interval, which is not on the grid of the interval, is told apart from one on the grid
func (h *harness) startMidSecond() {
	h.t.Helper()
	sleepUntilSecondFraction(500 * time.Millisecond)
	h.start()
}

// failing makes the monitor's checks fail except those the pattern marks as up. The pattern is
// the outcome of the monitor's first checks, true for up; later checks repeat its last outcome.
func (h *harness) failing(pattern ...bool) {
	h.exec.run = func(_ context.Context, call checkCall) *executor.Result {
		if len(pattern) > 0 && pattern[min(call.n, len(pattern))-1] {
			return up("up")
		}
		return down("down")
	}
}

// gap is the time between two checks
func gap(calls []checkCall, i int) time.Duration {
	return calls[i].at.Sub(calls[i-1].at)
}

// onGrid asserts that the check ran at the start of a multiple of the interval, in seconds
func onGrid(t *testing.T, call checkCall, interval int64) {
	t.Helper()
	assert.Zero(t, call.at.Unix()%interval, "check %d is at a multiple of %ds", call.n, interval)
	assert.Less(t, intoSecond(call.at), lateness, "check %d is at the start of its second", call.n)
}

func TestScheduler_ChecksAFailingMonitorAtItsRetryIntervalWhileItHasRetriesLeft(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(3), withRetries(2, 1)))
	h.failing()

	h.startMidSecond()

	eventually(t, 20*time.Second, func() bool { return h.exec.countFor("mon") >= 5 }, "five checks")
	calls := h.exec.callsFor("mon")
	// Checks 1 to 3 fail. The first two are Pending, so each is followed by a retry interval.
	assert.InDelta(t, float64(time.Second), float64(gap(calls, 1)), float64(lateness), "check 2 is a retry interval after check 1")
	assert.InDelta(t, float64(time.Second), float64(gap(calls, 2)), float64(lateness), "check 3 is a retry interval after check 2")
	// Check 3 is Down, so the monitor is back on the grid of its interval
	onGrid(t, calls[3], 3)
	assert.InDelta(t, float64(3*time.Second), float64(gap(calls, 4)), float64(lateness), "and a Down monitor is checked every interval")
}

func TestScheduler_ChecksAMonitorAtItsIntervalOnceACheckSucceeds(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(3), withRetries(2, 1)))
	// Two failures, which use up the retries; a success; a failure, which starts the count over
	h.failing(false, false, true, false, true)

	h.startMidSecond()

	eventually(t, 20*time.Second, func() bool { return h.exec.countFor("mon") >= 6 }, "six checks")
	calls := h.exec.callsFor("mon")
	assert.InDelta(t, float64(time.Second), float64(gap(calls, 1)), float64(lateness), "check 2 is a retry interval after check 1")
	assert.InDelta(t, float64(time.Second), float64(gap(calls, 2)), float64(lateness), "check 3 is a retry interval after check 2")
	onGrid(t, calls[3], 3) // check 3 is up
	assert.InDelta(t, float64(time.Second), float64(gap(calls, 4)), float64(lateness),
		"check 4 failed, and the failures before the success don't count: check 5 is a retry interval after it")
	onGrid(t, calls[5], 3) // check 5 is up
}

func TestScheduler_ChecksAtTheIntervalWhenThereAreNoRetries(t *testing.T) {
	t.Parallel()
	tests := map[string]monitorOption{
		"max retries is 0":      withRetries(0, 1),
		"retry interval is 0":   withRetries(3, 0),
		"neither is set (zero)": withRetries(0, 0),
	}
	for name, retries := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.db.put(monitorWith("mon", withInterval(2), retries))
			h.failing()

			h.startMidSecond()

			eventually(t, 8*time.Second, func() bool { return h.exec.countFor("mon") >= 3 }, "three checks")
			calls := h.exec.callsFor("mon")
			onGrid(t, calls[1], 2)
			assert.InDelta(t, float64(2*time.Second), float64(gap(calls, 2)), float64(lateness), "an interval apart")
		})
	}
}

func TestScheduler_ChecksAMonitorUnderMaintenanceAtItsInterval(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.maintained = true // the checks report Maintenance without running the executor
	h.db.put(monitorWith("mon", withInterval(2), withRetries(3, 1)))

	h.startMidSecond()

	eventually(t, 8*time.Second, func() bool { return h.ingestedFor("mon") >= 3 }, "three results")
	results := h.ingest.ingested()
	assert.Equal(t, shared.MonitorStatusMaintenance, results[0].Status)
	assert.InDelta(t, float64(2*time.Second), float64(results[2].StartTime.Sub(results[1].StartTime)), float64(lateness),
		"a Maintenance result isn't a failure: an interval apart")
}

func TestScheduler_AnEditKeepsTheCountOfFailedChecks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(3), withRetries(1, 1)))
	h.failing()
	h.startMidSecond()
	eventually(t, 2*time.Second, func() bool { return h.ingestedFor("mon") == 1 }, "the first check fails")
	time.Sleep(100 * time.Millisecond) // for the scheduler to count it

	// The edit checks the monitor right away. That is its second failure in a row, and it has
	// one retry, so it is Down: the next check is on the grid, not a retry interval later.
	h.edit("mon")

	eventually(t, 20*time.Second, func() bool { return h.exec.countFor("mon") >= 3 }, "a third check")
	calls := h.exec.callsFor("mon")
	require.Equal(t, 2, calls[1].n)
	onGrid(t, calls[2], 3)
}
