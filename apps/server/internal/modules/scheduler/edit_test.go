package scheduler

import (
	"testing"
	"time"

	"peekaping/internal/modules/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// edit saves the monitor with a new name, the way the monitor service does, and publishes the event
func (h *harness) edit(id string, changes ...monitorOption) {
	h.t.Helper()
	m := h.db.get(id)
	m.Name += " (edited)"
	for _, change := range changes {
		change(&m)
	}
	h.db.put(m)
	h.publishMonitor(events.MonitorUpdated, id)
}

func TestScheduler_ChecksANonPushMonitorRightAwayWhenItIsEdited(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")

	h.edit("mon")

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 2 }, "the edit is checked right away")
}

func TestScheduler_DoesNotCheckAPushMonitorWhenItIsEdited(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day), ofType("push")))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")

	h.edit("mon")

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("mon"), "a push monitor waits for its pushes: a check now would find none")
}

func TestScheduler_ACheckIntervalChangedByAnEditTakesEffectFromTheNextCheck(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		monitorType string
		// checkedNow is whether the edit is checked right away
		checkedNow bool
	}{
		"a push monitor":     {monitorType: "push"},
		"a non-push monitor": {monitorType: "http", checkedNow: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.db.put(monitorWith("mon", withInterval(day), ofType(tt.monitorType)))
			h.start()
			eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") == 1 }, "the first check")

			sleepUntilSecondFraction(500 * time.Millisecond)
			editedAt := time.Now()
			h.edit("mon", withInterval(1))

			wantChecks := 3
			if !tt.checkedNow {
				wantChecks = 2
			}
			eventually(t, 3*time.Second, func() bool { return h.exec.countFor("mon") >= 1+wantChecks }, "checks after the edit")
			calls := h.exec.callsFor("mon")[1:]
			if tt.checkedNow {
				require.Less(t, calls[0].at.Sub(editedAt), lateness, "the edit is checked right away")
				calls = calls[1:]
			}
			assert.Greater(t, calls[0].at.Sub(editedAt), 300*time.Millisecond, "the new interval counts from the next check, not from the edit")
			assert.Less(t, intoSecond(calls[0].at), lateness, "the next check is at the start of a second")
			assert.Less(t, intoSecond(calls[1].at), lateness, "and so is the one after")
			assert.InDelta(t, float64(time.Second), float64(calls[1].at.Sub(calls[0].at)), float64(lateness))
		})
	}
}
