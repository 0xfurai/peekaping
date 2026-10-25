package producer

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"peekaping/internal/leader"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
)

// MonitorFinder is the part of the monitor service the producer reads. The watchdog reads
// FindByID and FindActivePaginated: it watches the monitors of every probe, the producer
// schedules only those of the local probe.
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error)
	FindActiveByProbe(ctx context.Context, probeID string, page int, limit int) ([]*shared.Monitor, error)
}

// Producer is responsible for scheduling monitor health checks
type Producer struct {
	rdb                     *redis.Client
	runner                  check.Runner // enqueues the checks for the worker
	monitorService          MonitorFinder
	loader                  *check.Loader
	logger                  *zap.SugaredLogger
	ctx                     context.Context // set by Start, which makes it end with the caller's ctx or with Stop
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup // the goroutines that schedule: claim loops, reclaimer, schedule refresher, probe sweep and the leadership monitor
	watchdogWg              sync.WaitGroup // the watchdog's, apart from wg so that Stop can tell when each has stopped
	mu                      sync.RWMutex
	monitorIntervals        map[string]int // monitor_id -> interval in seconds
	scheduleRefreshInterval time.Duration
	reclaimInterval         time.Duration // how often expired leases are swept back to the due set
	leadershipCheckInterval time.Duration // how often syncing is started or stopped to follow leadership
	leaderElection          *leader.Elector
	electionCancel          context.CancelFunc // ends the election, which releases the leadership; Stop does that last. Nil until Start
	electionDone            <-chan struct{}    // closes once the election has released the leadership; nil until Start
	stopOnce                sync.Once          // runs the stop once, whether Stop or the end of Start's ctx asks first
	concurrency             int                // number of concurrent producer goroutines
	watchdog                *stale.Watchdog
	probeSweep              *probe.Sweep // nil when the producer has none to run
}
