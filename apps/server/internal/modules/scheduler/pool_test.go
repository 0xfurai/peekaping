package scheduler

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdChecks makes every check wait for a token, or for its cancel. It returns the channel of tokens.
func (h *harness) holdChecks() chan struct{} {
	tokens := make(chan struct{})
	h.exec.run = func(ctx context.Context, call checkCall) *executor.Result {
		select {
		case <-tokens:
		case <-ctx.Done():
		}
		return up("check " + strconv.Itoa(call.n))
	}
	return tokens
}

func TestScheduler_RunsNoMoreChecksThanThePoolHoldsAndDropsNone(t *testing.T) {
	t.Parallel()
	const poolSize, monitors = 3, 10
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = poolSize })
	tokens := h.holdChecks()
	for i := range monitors {
		h.db.put(monitorWith("mon-"+strconv.Itoa(i), withInterval(day)))
	}

	h.start()

	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == poolSize }, "the pool fills")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, poolSize, h.exec.runningNow(), "the checks that are due wait for a slot")
	assert.Equal(t, poolSize, h.exec.count())

	tokens <- struct{}{} // one check finishes
	eventually(t, 2*time.Second, func() bool { return h.exec.count() == poolSize+1 }, "a waiting check takes the free slot")
	assert.Equal(t, poolSize, h.exec.runningNow())

	close(tokens) // the rest finish
	eventually(t, 5*time.Second, func() bool { return len(h.ingest.ingested()) == monitors }, "every due check ran and was ingested")
	assert.Equal(t, monitors, h.exec.count())
	assert.Equal(t, poolSize, h.exec.mostRunning(), "never more than the pool holds at once")
}

func TestScheduler_TheDefaultPoolHolds128Checks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = 0 })
	h.holdChecks()
	for i := range 200 {
		h.db.put(monitorWith("mon-"+strconv.Itoa(i), withInterval(day)))
	}

	h.start()

	eventually(t, 5*time.Second, func() bool { return h.exec.runningNow() == 128 }, "the pool fills")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 128, h.exec.runningNow())
	assert.Equal(t, 128, h.exec.count())
}

// checkOf is the monitor's check as the scheduler holds it, nil when none is running or waiting
func (h *harness) checkOf(id string) *checkRun {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.checks[id]
}

// checksLeft is how many monitors have a check running or waiting for a slot
func (h *harness) checksLeft() int {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return len(h.s.checks)
}

// cancelled reports whether the check was cancelled, or has ended and been replaced since
func (h *harness) cancelled(id string, check *checkRun) bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	now := h.s.checks[id]
	return now != check || now.cancelled
}

func TestScheduler_ACheckWaitingForASlotIsCancelledLikeARunningOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = 1 })
	tokens := h.holdChecks()
	// The checks are held for longer than the waits below together: a check that timed out would
	// free the slot before the edit cancels the waiting one
	h.db.put(monitorWith("a", withInterval(day), withTimeout(120)))
	h.db.put(monitorWith("b", withInterval(day), withTimeout(120)))
	h.start()
	eventually(t, 10*time.Second, func() bool { return h.exec.count() == 1 }, "one check holds the slot")
	waiting := "a"
	if h.exec.countFor("a") == 1 {
		waiting = "b"
	}
	waited := h.checkOf(waiting)
	require.NotNil(t, waited, "the other check waits for the slot")

	h.edit(waiting)
	// The scheduler gets the edit on a goroutine of its own. The slot must still be taken
	// when the edit cancels the waiting check, or the check would take it and run.
	eventually(t, 10*time.Second, func() bool { return h.cancelled(waiting, waited) }, "the edit reaches the waiting check")
	close(tokens) // the checks finish

	eventually(t, 10*time.Second, func() bool { return len(h.ingest.ingested()) == 2 && h.checksLeft() == 0 },
		"the running check and the edit's check are ingested, and no check is left to run")
	assert.Equal(t, 1, h.exec.countFor(waiting), "the check that waited when the monitor was edited never ran, only the edit's check did")
	for _, result := range h.ingest.ingested() {
		if result.MonitorID == waiting {
			assert.Equal(t, "Monitor "+waiting+" (edited)", result.MonitorName)
		}
	}
}

func TestScheduler_StopDoesNotWaitForASlotForTheChecksThatAreWaiting(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = 1 })
	h.holdChecks()
	for i := range 5 {
		h.db.put(monitorWith("mon-"+strconv.Itoa(i), withInterval(day)))
	}
	require.NoError(t, h.s.Start(context.Background()))
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "one check holds the slot")

	done := make(chan struct{})
	go func() {
		h.s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop is waiting for the checks that never got a slot")
	}
	assert.Equal(t, 1, h.exec.count(), "the waiting checks never ran")
	assert.Empty(t, h.ingest.ingested())
}

func TestScheduler_ACheckWaitingForASlotCountsAsRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = 1 })
	release := make(chan struct{})
	var first atomic.Bool
	h.exec.run = func(_ context.Context, call checkCall) *executor.Result {
		if first.CompareAndSwap(false, true) {
			<-release
		}
		return up("check " + strconv.Itoa(call.n))
	}
	h.db.put(monitorWith("a", withInterval(1)))
	h.db.put(monitorWith("b", withInterval(1)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.count() == 1 }, "one check holds the slot")

	time.Sleep(2300 * time.Millisecond) // both timers fire at least twice
	close(release)

	// The check that waited ran once. If each of its skipped timers had queued another, it would run three times.
	time.Sleep(150 * time.Millisecond)
	assert.LessOrEqual(t, h.exec.countFor("a"), 2)
	assert.LessOrEqual(t, h.exec.countFor("b"), 2)
}
