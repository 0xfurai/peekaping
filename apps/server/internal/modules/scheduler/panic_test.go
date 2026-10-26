package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestScheduler_APanicEndsOnlyTheCheckItHappensIn(t *testing.T) {
	t.Parallel()
	// Each panics in the first check of monitor "bad" and works from then on. They reach the
	// harness through a pointer, since it is built with them.
	tests := map[string]func(h **harness, panicked *atomic.Bool) func(*Config){
		"in the load": func(h **harness, panicked *atomic.Bool) func(*Config) {
			return withLoader(loaderFunc(func(ctx context.Context, monitorID string) (*check.Job, error) {
				if monitorID == "bad" && panicked.CompareAndSwap(false, true) {
					panic("loader bug")
				}
				return (*h).db.Load(ctx, monitorID)
			}))
		},
		"in the ingest": func(h **harness, panicked *atomic.Bool) func(*Config) {
			return func(cfg *Config) {
				cfg.Ingester = ingestFunc(func(ctx context.Context, r *ingester.Result) error {
					if r.MonitorID == "bad" && panicked.CompareAndSwap(false, true) {
						panic("ingest bug")
					}
					return (*h).ingest.Ingest(ctx, r)
				})
			}
		},
	}
	for name, panicIn := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			core, logs := observer.New(zap.ErrorLevel)
			var panicked atomic.Bool
			var h *harness
			h = newHarness(t, func(cfg *Config) {
				cfg.Logger = zap.New(core).Sugar()
				// One slot: a panic that kept its slot would stop every check after it
				cfg.Concurrency = 1
				panicIn(&h, &panicked)(cfg)
			})
			h.db.put(monitorWith("bad", withInterval(1)))
			h.db.put(monitorWith("good", withInterval(1)))

			h.start()

			eventually(t, 3*time.Second, panicked.Load, "the first check of bad panics")
			eventually(t, 3*time.Second, func() bool { return h.ingestedFor("good") >= 2 }, "the other monitor is still checked")
			eventually(t, 3*time.Second, func() bool { return h.ingestedFor("bad") >= 1 }, "and so is the one whose check panicked, at its next check")
			assert.Equal(t, 1, logs.FilterMessage("Check panicked").FilterField(zap.String("monitor_id", "bad")).Len(), "the panic is logged with its monitor")
		})
	}
}

// panicsOnceInTheLoad makes the first check of monitor "bad" panic
func panicsOnceInTheLoad(h **harness, panicked *atomic.Bool) func(*Config) {
	loader := withLoader(loaderFunc(func(ctx context.Context, monitorID string) (*check.Job, error) {
		if monitorID == "bad" && panicked.CompareAndSwap(false, true) {
			panic("loader bug")
		}
		return (*h).db.Load(ctx, monitorID)
	}))
	return func(cfg *Config) {
		cfg.Concurrency = 1
		loader(cfg)
	}
}

func TestScheduler_ReportsTheCheckThatPanicked(t *testing.T) {
	recorder := reportingtest.Install(t)
	var panicked atomic.Bool
	var h *harness
	h = newHarness(t, panicsOnceInTheLoad(&h, &panicked))
	h.db.put(monitorWith("bad", withInterval(1)))

	h.start()

	events := recorder.Wait(t, 1)
	assert.Equal(t, "scheduler", events[0].Tags["component"])
	assert.Equal(t, "string", events[0].Exception[0].Type)
	assert.Contains(t, functionsOf(events[0]), "(*Scheduler).runCheck")
	eventually(t, 3*time.Second, func() bool { return h.ingestedFor("bad") >= 1 }, "the monitor is checked again, as without reporting")
	assert.Len(t, recorder.Events(), 1)
}

func TestScheduler_ReportsNothingForACheckThatPanickedWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)
	var panicked atomic.Bool
	var h *harness
	h = newHarness(t, panicsOnceInTheLoad(&h, &panicked))
	h.db.put(monitorWith("bad", withInterval(1)))

	h.start()

	eventually(t, 3*time.Second, panicked.Load, "the first check of bad panics")
	eventually(t, 3*time.Second, func() bool { return h.ingestedFor("bad") >= 1 }, "and the next one is made")
	assert.Empty(t, recorder.Events())
}

// functionsOf are the function names in the frames of the event's exception
func functionsOf(event *sentry.Event) []string {
	var names []string
	for _, frame := range event.Exception[0].Stacktrace.Frames {
		names = append(names, frame.Function)
	}
	return names
}
