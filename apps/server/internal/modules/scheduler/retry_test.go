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

// The tests tell a retry from a check on the interval by a wide margin, not by an exact time, so
// that a slow machine doesn't fail them. Their retry interval is 1s and their interval 2s or 3s.

// retryGap asserts that the check came a retry interval after the one before it: at least 0.8s,
// and less than 2s, the midpoint between a retry interval and the interval of 3s
func retryGap(t *testing.T, calls []checkCall, i int, message string) {
	t.Helper()
	assert.GreaterOrEqual(t, gap(calls, i), 800*time.Millisecond, message)
	assert.Less(t, gap(calls, i), 2*time.Second, message)
}

// intervalGap asserts that the check came an interval after the one before it: at least the
// midpoint between a retry interval and the interval
func intervalGap(t *testing.T, calls []checkCall, i int, midpoint time.Duration, message string) {
	t.Helper()
	assert.GreaterOrEqual(t, gap(calls, i), midpoint, message)
}

// onGrid asserts that the check ran at a multiple of the interval, in seconds, and not at the
// retry timers' half second: it is nearer to a whole second than to the middle of one, and the
// second is a multiple of the interval
func onGrid(t *testing.T, call checkCall, interval int64) {
	t.Helper()
	nearest := call.at.Round(time.Second)
	assert.Zero(t, nearest.Unix()%interval, "check %d is at a multiple of %ds", call.n, interval)
	assert.Less(t, call.at.Sub(nearest).Abs(), 250*time.Millisecond, "check %d is at the start of its second", call.n)
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
	retryGap(t, calls, 1, "check 2 is a retry interval after check 1")
	retryGap(t, calls, 2, "check 3 is a retry interval after check 2")
	// Check 3 is Down, so check 4 is back on the grid of the interval
	onGrid(t, calls[3], 3)
	intervalGap(t, calls, 4, 2*time.Second, "and a Down monitor is checked every interval")
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
	retryGap(t, calls, 1, "check 2 is a retry interval after check 1")
	retryGap(t, calls, 2, "check 3 is a retry interval after check 2")
	onGrid(t, calls[3], 3) // check 4 is back on the grid after the success of check 3
	retryGap(t, calls, 4,
		"check 4 failed, and the failures before the success don't count: check 5 is a retry interval after it")
	onGrid(t, calls[5], 3) // and check 6 after the success of check 5
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
			intervalGap(t, calls, 2, 1500*time.Millisecond, "an interval apart")
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
	assert.GreaterOrEqual(t, results[2].StartTime.Sub(results[1].StartTime), 1500*time.Millisecond,
		"a Maintenance result isn't a failure: an interval apart, not a retry interval")
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
