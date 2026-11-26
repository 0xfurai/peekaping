package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/healthcheck/executor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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

// loaderFunc is a Loader made of a function
type loaderFunc func(ctx context.Context, monitorID string) (*check.Job, error)

func (f loaderFunc) Load(ctx context.Context, monitorID string) (*check.Job, error) {
	return f(ctx, monitorID)
}

func TestScheduler_LogsNoErrorForACheckCancelledWhileItLoads(t *testing.T) {
	t.Parallel()
	tests := map[string]func(h *harness){
		"by an update": func(h *harness) { h.edit("mon") },
		"by a stop":    func(h *harness) { h.s.Stop() },
	}
	for name, cancel := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			core, logs := observer.New(zap.WarnLevel)
			loading := make(chan struct{}, 1)
			var h *harness
			h = newHarness(t, func(cfg *Config) { cfg.Logger = zap.New(core).Sugar() },
				// The first load waits for its check to be cancelled, then fails as the
				// database does on a cancelled context
				withLoader(loaderFunc(func(ctx context.Context, monitorID string) (*check.Job, error) {
					select {
					case loading <- struct{}{}:
						<-ctx.Done()
						return nil, fmt.Errorf("failed to find monitor: %w", ctx.Err())
					default:
						return h.db.Load(ctx, monitorID)
					}
				})))
			h.db.put(monitorWith("mon", withInterval(day)))
			h.start()
			<-loading

			cancel(h)

			time.Sleep(200 * time.Millisecond)
			assert.Empty(t, logs.All(), "a cancelled check is no failure")
		})
	}
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
	h.publishDeleted("mon") // its handler must not panic or hang: the bus's close checks
}

func TestScheduler_DropsTheResultOfACheckThatEndsWhileAnUpdatePauseOrDeleteWaitsItsTurn(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stop func(h *harness)
		// want is what the monitor's ingested results say
		want []string
	}{
		"updated": {stop: func(h *harness) { h.edit("mon") }, want: []string{"check 2"}},
		"paused":  {stop: func(h *harness) { h.edit("mon", paused) }},
		"deleted": {stop: func(h *harness) { h.db.remove("mon"); h.publishDeleted("mon") }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.db.put(monitorWith("mon", withInterval(day)))
			// The first check ends when it is cancelled or released, and reports Up either way
			cancelled, release := make(chan struct{}), make(chan struct{})
			h.exec.run = func(ctx context.Context, call checkCall) *executor.Result {
				if call.monitorID == "mon" && call.n == 1 {
					select {
					case <-ctx.Done():
						close(cancelled)
					case <-release:
					}
				}
				return up("check " + strconv.Itoa(call.n))
			}
			h.start()
			eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 1 }, "the first check is running")
			// Another monitor's event keeps the scheduler busy reading that monitor
			h.db.put(monitorWith("other", withInterval(day)))
			reading, done := h.db.holdFind("other")
			t.Cleanup(done)
			h.publishMonitor(events.MonitorCreated, "other")
			<-reading

			tt.stop(h)

			select {
			case <-cancelled:
			case <-time.After(500 * time.Millisecond):
				close(release) // the check ends on its own while the event waits its turn
				eventually(t, 2*time.Second, func() bool { return h.exec.runningNow() == 0 }, "the check has ended")
				time.Sleep(100 * time.Millisecond)
			}
			done()
			eventually(t, 2*time.Second, func() bool { return h.ingestedFor("other") == 1 }, "the other monitor's event is done")
			if len(tt.want) > 0 {
				eventually(t, 2*time.Second, func() bool { return h.ingestedFor("mon") >= len(tt.want) }, "the edit is checked")
			}
			time.Sleep(200 * time.Millisecond)
			assert.Equal(t, tt.want, h.ingestedMessagesFor("mon"), "the result of the check that ended after the event is dropped")
		})
	}
}
