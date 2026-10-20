package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reconcileEvery sets how often the scheduler compares its timers with the database
func reconcileEvery(interval time.Duration) func(*Config) {
	return func(cfg *Config) { cfg.ReconcileInterval = interval }
}

func TestScheduler_ReconcileChecksAnActiveMonitorWhoseEventItMissed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, reconcileEvery(50*time.Millisecond))
	h.db.put(monitorWith("known", withInterval(day)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("known") == 1 }, "the first check")

	h.db.put(monitorWith("missed", withInterval(day))) // no event

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("missed") == 1 }, "the monitor is found and checked right away")
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("missed"), "once")
	assert.Equal(t, 1, h.exec.countFor("known"), "a monitor that hasn't changed is left alone")
}

func TestScheduler_ReconcileStopsCheckingAMonitorThatIsGoneOrPausedWithoutAnEvent(t *testing.T) {
	t.Parallel()
	tests := map[string]func(h *harness){
		"deleted": func(h *harness) { h.db.remove("mon") },
		"paused": func(h *harness) {
			m := h.db.get("mon")
			m.Active = false
			h.db.put(m)
		},
	}
	for name, stop := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, reconcileEvery(50*time.Millisecond))
			h.db.put(monitorWith("mon", withInterval(1)))
			cancelled := h.holdFirstCheck()
			h.start()
			eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

			stop(h) // no event

			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("the running check wasn't cancelled")
			}
			time.Sleep(2200 * time.Millisecond)
			assert.Equal(t, 1, h.exec.countFor("mon"), "no check at the next two multiples of the interval")
			assert.Empty(t, h.ingest.ingested(), "the cancelled check's result is dropped")
		})
	}
}

func TestScheduler_ReconcileTakesAChangedCheckIntervalFromTheNextCheck(t *testing.T) {
	t.Parallel()
	h := newHarness(t, reconcileEvery(50*time.Millisecond))
	h.db.put(monitorWith("mon", withInterval(day)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")

	sleepUntilSecondFraction(500 * time.Millisecond)
	changedAt := time.Now()
	m := h.db.get("mon")
	m.Interval = 1
	h.db.put(m) // no event

	eventually(t, 3*time.Second, func() bool { return h.exec.countFor("mon") >= 3 }, "checks on the new interval")
	calls := h.exec.callsFor("mon")[1:]
	require.Greater(t, calls[0].at.Sub(changedAt), 300*time.Millisecond, "not checked right away")
	assert.Less(t, intoSecond(calls[0].at), lateness, "the next check is at the start of a second")
	assert.Less(t, intoSecond(calls[1].at), lateness, "and so is the one after")
	assert.InDelta(t, float64(time.Second), float64(calls[1].at.Sub(calls[0].at)), float64(lateness))
}

func TestScheduler_ReconcileKeepsAnActiveMonitorTheListOfActiveMonitorsMissed(t *testing.T) {
	t.Parallel()
	h := newHarness(t, reconcileEvery(50*time.Millisecond))
	h.db.put(monitorWith("mon", withInterval(1)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") >= 1 }, "the first check")

	h.db.unlist("mon")

	time.Sleep(2300 * time.Millisecond)
	assert.GreaterOrEqual(t, h.exec.countFor("mon"), 3, "still checked on each multiple of its interval")
}
