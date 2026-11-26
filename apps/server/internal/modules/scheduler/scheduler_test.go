package scheduler

import (
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_ChecksEveryActiveMonitorWhenItStarts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, id := range []string{"a", "b", "c"} {
		h.db.put(monitorWith(id, withInterval(day)))
	}
	h.db.put(monitorWith("paused", withInterval(day), paused))

	h.start()

	eventually(t, 2*time.Second, func() bool { return h.exec.count() == 3 }, "the active monitors are checked right away")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, []string{"a", "b", "c"}, h.exec.monitorIDs(), "a paused monitor isn't checked")
}

func TestScheduler_ChecksActiveMonitorsBeyondTheFirstPageWhenItStarts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const monitors = 250
	for i := range monitors {
		h.db.put(monitorWith("mon-"+strconv.Itoa(i), withInterval(day)))
	}

	h.start()

	eventually(t, 5*time.Second, func() bool { return h.exec.count() == monitors }, "every active monitor is checked")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, monitors, h.exec.count(), "once each")
}

func TestScheduler_ChecksAMonitorRightAwayWhenItIsCreated(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.start()
	require.Zero(t, h.exec.count())

	h.db.put(monitorWith("new", withInterval(day)))
	h.publishMonitor(events.MonitorCreated, "new")

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("new") == 1 }, "the new monitor is checked right away")
	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "its result is ingested")
	assert.Equal(t, "new", h.ingest.ingested()[0].MonitorID)
}

func TestScheduler_DoesNotCheckAMonitorThatIsCreatedPaused(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.start()

	h.db.put(monitorWith("new", withInterval(1), paused))
	h.publishMonitor(events.MonitorCreated, "new")

	time.Sleep(1200 * time.Millisecond)
	assert.Zero(t, h.exec.count())
}

func TestScheduler_ChecksAgainOnEachNextMultipleOfTheInterval(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("every-2s", withInterval(2)))
	startedAt := time.Now()

	h.start()

	eventually(t, 7*time.Second, func() bool { return h.exec.countFor("every-2s") >= 3 }, "three checks")
	calls := h.exec.callsFor("every-2s")
	assert.Less(t, calls[0].at.Sub(startedAt), lateness, "the first check is right away")
	for _, call := range calls[1:3] {
		assert.Zero(t, call.at.Unix()%2, "check %d is in an even second", call.n)
		assert.Less(t, intoSecond(call.at), lateness, "check %d is at the start of its second", call.n)
	}
	assert.InDelta(t, float64(2*time.Second), float64(calls[2].at.Sub(calls[1].at)), float64(lateness), "an interval apart")
}

func TestScheduler_StopsCheckingAMonitorThatIsPausedOrDeleted(t *testing.T) {
	t.Parallel()
	tests := map[string]func(h *harness){
		"paused": func(h *harness) {
			m := h.db.get("mon")
			m.Active = false
			h.db.put(m)
			h.publishMonitor(events.MonitorUpdated, "mon")
		},
		"deleted": func(h *harness) {
			h.db.remove("mon")
			h.publishDeleted("mon")
		},
	}
	for name, stop := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.db.put(monitorWith("mon", withInterval(1)))
			h.start()
			eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") >= 1 }, "the first check")

			stop(h)

			time.Sleep(100 * time.Millisecond)
			checked := h.exec.countFor("mon")
			time.Sleep(2200 * time.Millisecond)
			assert.Equal(t, checked, h.exec.countFor("mon"), "no check at the next two multiples of the interval")
		})
	}
}

func TestScheduler_IgnoresEventsBeforeItStarts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))

	h.publishMonitor(events.MonitorCreated, "mon")

	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, h.exec.count())
}
