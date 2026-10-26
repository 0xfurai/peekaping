package scheduler

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drain calls Drain on a goroutine of its own. The returned channel is closed when it returns.
func (h *harness) drain(ctx context.Context) <-chan struct{} {
	drained := make(chan struct{})
	go func() {
		h.s.Drain(ctx)
		close(drained)
	}()
	return drained
}

func TestScheduler_DrainLetsRunningChecksFinish(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	tokens := h.holdChecks()
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

	drained := h.drain(context.Background())

	select {
	case <-drained:
		t.Fatal("Drain returned while a check was running")
	case <-time.After(200 * time.Millisecond):
	}
	tokens <- struct{}{} // the check finishes
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("Drain didn't return once the check had finished")
	}
	assert.Equal(t, []string{"check 1"}, h.ingest.messages(), "the finished check's result is ingested")
	assert.Error(t, h.s.RunNow(context.Background(), "mon"), "the scheduler is stopped")
	h.s.Stop() // does nothing, and does not wait
}

func TestScheduler_DrainCancelsAtTheDeadline(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	cancelled := h.holdFirstCheck()
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	h.s.Drain(ctx)

	assert.GreaterOrEqual(t, time.Since(startedAt), 300*time.Millisecond, "Drain waited until its deadline")
	select {
	case <-cancelled:
	default:
		t.Fatal("Drain returned before the running check ended")
	}
	assert.Zero(t, h.exec.runningNow())
	assert.Empty(t, h.ingest.ingested(), "the result of the check Drain cancelled is dropped")
}

func TestScheduler_DrainDoesNotStartWaitingChecks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(cfg *Config) { cfg.Concurrency = 1 })
	tokens := h.holdChecks()
	for i := range 5 {
		h.db.put(monitorWith("mon-"+strconv.Itoa(i), withInterval(day)))
	}
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "one check holds the slot")

	drained := h.drain(context.Background())
	time.Sleep(100 * time.Millisecond)
	tokens <- struct{}{} // the running check finishes and frees its slot

	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("Drain is waiting for a check that was waiting for a slot")
	}
	assert.Equal(t, 1, h.exec.count(), "the waiting checks never ran")
	assert.Len(t, h.ingest.ingested(), 1, "the running check's result is ingested")
}

func TestScheduler_DrainStopsTheTimers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(1)))
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.ingestedFor("mon") == 1 }, "the first check is ingested")

	h.s.Drain(context.Background())

	time.Sleep(2200 * time.Millisecond)
	assert.Equal(t, 1, h.exec.countFor("mon"), "no check at the next two multiples of the interval")
}

func TestScheduler_DrainBeforeItStartsDoesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	h.s.Drain(context.Background())

	require.NoError(t, h.s.Start(context.Background()), "the scheduler can still start")
	h.s.Stop()
}
