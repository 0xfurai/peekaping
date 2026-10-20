package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/ingester"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// ingesterFunc is an ingester.Service made of a function
type ingesterFunc func(ctx context.Context, r *ingester.Result) error

func (f ingesterFunc) Ingest(ctx context.Context, r *ingester.Result) error { return f(ctx, r) }

func TestScheduler_APanicEndsOnlyTheCheckItHappensIn(t *testing.T) {
	t.Parallel()
	// Each panics in the first check of monitor "bad" and works from then on. They reach the
	// harness through a pointer, since it is built with them.
	tests := map[string]func(h **harness, panicked *atomic.Bool) func(*Config){
		"in the load": func(h **harness, panicked *atomic.Bool) func(*Config) {
			return func(cfg *Config) {
				cfg.Loader = loaderFunc(func(ctx context.Context, monitorID string) (*check.Job, error) {
					if monitorID == "bad" && panicked.CompareAndSwap(false, true) {
						panic("loader bug")
					}
					return (*h).db.Load(ctx, monitorID)
				})
			}
		},
		"in the ingest": func(h **harness, panicked *atomic.Bool) func(*Config) {
			return func(cfg *Config) {
				cfg.Ingester = ingesterFunc(func(ctx context.Context, r *ingester.Result) error {
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

// ingestedFor counts the monitor's ingested results
func (h *harness) ingestedFor(monitorID string) int {
	n := 0
	for _, r := range h.ingest.ingested() {
		if r.MonitorID == monitorID {
			n++
		}
	}
	return n
}
