package scheduler

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_MonitorMovedToRemoteIsUnscheduledAndCancelled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(1)))
	cancelled := h.holdFirstCheck()
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

	h.edit("mon", onProbes("probe-1"))

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the running check wasn't cancelled")
	}
	time.Sleep(2200 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("mon"), "no check at the next two multiples of the interval")
	assert.Empty(t, h.ingest.ingested(), "the cancelled check's result is dropped")
}

func TestScheduler_MovedBackIsCheckedAtOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day), onProbes("probe-1")))
	h.start()
	time.Sleep(300 * time.Millisecond)
	require.Zero(t, h.exec.countFor("mon"), "a monitor on a remote probe isn't checked here")

	sleepUntilSecondFraction(500 * time.Millisecond)
	movedAt := time.Now()
	h.edit("mon", onProbes(shared.LocalProbeID), withInterval(1))

	eventually(t, 3*time.Second, func() bool { return h.exec.countFor("mon") >= 3 }, "the monitor is checked now and on its interval")
	calls := h.exec.callsFor("mon")
	assert.Less(t, calls[0].at.Sub(movedAt), lateness, "the first check is right away")
	assert.Less(t, intoSecond(calls[1].at), lateness, "the next check is at the start of a second")
	assert.Less(t, intoSecond(calls[2].at), lateness, "and so is the one after")
}

func TestScheduler_ReconcileSkipsRemote(t *testing.T) {
	t.Parallel()
	t.Run("a monitor on a remote probe is never scheduled", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, reconcileEvery(50*time.Millisecond))
		h.db.put(monitorWith("remote", withInterval(1), onProbes("probe-1")))
		h.db.put(monitorWith("local", withInterval(day), onProbes(shared.LocalProbeID)))
		h.start()
		eventually(t, 2*time.Second, func() bool { return h.exec.countFor("local") == 1 }, "the local monitor is checked")

		time.Sleep(2200 * time.Millisecond)

		assert.Zero(t, h.exec.countFor("remote"))
	})
	t.Run("a monitor moved to a remote probe without an event loses its timer", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, reconcileEvery(50*time.Millisecond))
		h.db.put(monitorWith("mon", withInterval(1)))
		cancelled := h.holdFirstCheck()
		h.start()
		eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

		m := h.db.get("mon")
		m.ProbeIDs = []string{"probe-1"}
		h.db.put(m) // no event

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

func TestScheduler_RunNowRefusesRemote(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day), onProbes("probe-1")))
	h.start()

	assert.Error(t, h.s.RunNow(context.Background(), "mon"))

	time.Sleep(200 * time.Millisecond)
	assert.Zero(t, h.exec.countFor("mon"))
}

func TestScheduler_AMonitorOnLocalAndARemoteProbeIsCheckedHere(t *testing.T) {
	t.Parallel()
	h := newHarness(t, reconcileEvery(50*time.Millisecond))
	h.db.put(monitorWith("both", withInterval(day), onProbes("probe-1", shared.LocalProbeID)))
	h.db.put(monitorWith("remote-only", withInterval(day), onProbes("probe-1", "probe-2")))
	h.start()

	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("both") == 1 }, "the monitor that runs on local too is checked here")

	assert.NoError(t, h.s.RunNow(context.Background(), "both"))
	assert.Error(t, h.s.RunNow(context.Background(), "remote-only"))
	assert.Zero(t, h.exec.countFor("remote-only"))
}
