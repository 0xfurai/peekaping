package scheduler

import (
	"context"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdFirstCheck makes a monitor's first check wait until it is cancelled, then report Up, as a check
// that finishes just as it is cancelled would. The returned channel is closed when that happens.
func (h *harness) holdFirstCheck() <-chan struct{} {
	cancelled := make(chan struct{})
	h.exec.run = func(ctx context.Context, call checkCall) *executor.Result {
		if call.n == 1 {
			<-ctx.Done()
			close(cancelled)
		}
		return up("check " + strconv.Itoa(call.n))
	}
	return cancelled
}

func TestScheduler_AnUpdateCancelsTheRunningCheckAndDropsItsResult(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	cancelled := h.holdFirstCheck()
	h.start()
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

	h.edit("mon")

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the running check wasn't cancelled")
	}
	eventually(t, 2*time.Second, func() bool { return len(h.ingest.ingested()) == 1 }, "the edit's check is ingested")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, []string{"check 2"}, h.ingest.messages(), "the cancelled check's result is dropped")
}

func TestScheduler_APauseOrDeleteCancelsTheRunningCheckAndDropsItsResult(t *testing.T) {
	t.Parallel()
	tests := map[string]func(h *harness){
		"paused":  func(h *harness) { h.edit("mon", paused) },
		"deleted": func(h *harness) { h.db.remove("mon"); h.publishDeleted("mon") },
	}
	for name, stop := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.db.put(monitorWith("mon", withInterval(day)))
			cancelled := h.holdFirstCheck()
			h.start()
			eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

			stop(h)

			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("the running check wasn't cancelled")
			}
			time.Sleep(200 * time.Millisecond)
			assert.Empty(t, h.ingest.ingested(), "the cancelled check's result is dropped")
			assert.Equal(t, 1, h.exec.countFor("mon"), "and no check follows")
		})
	}
}

func TestScheduler_StopCancelsTheRunningChecksAndWaitsForThem(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(day)))
	cancelled := h.holdFirstCheck()
	require.NoError(t, h.s.Start(context.Background()))
	eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")

	h.s.Stop()

	select {
	case <-cancelled:
	default:
		t.Fatal("Stop returned before the running check ended")
	}
	assert.Empty(t, h.ingest.ingested(), "the result of a check cancelled by Stop is dropped")
	assert.Zero(t, h.exec.runningNow())
}

func TestScheduler_StopsWhenItsContextIsDone(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.db.put(monitorWith("mon", withInterval(1)))
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, h.s.Start(ctx))
	t.Cleanup(h.s.Stop)
	eventually(t, 2*time.Second, func() bool { return h.exec.countFor("mon") >= 1 }, "the first check")

	cancel()

	time.Sleep(100 * time.Millisecond)
	checked := h.exec.countFor("mon")
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, checked, h.exec.countFor("mon"), "no check at the next multiple of the interval")
	h.bus.Publish(events.Event{Type: events.MonitorDeleted, Payload: "mon"}) // must not panic or hang
}
