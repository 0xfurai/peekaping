package producer

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
)

// MonitorFinder is the part of the monitor service the producer reads. The watchdog reads
// the same two methods.
type MonitorFinder interface {
	FindByID(ctx context.Context, id string) (*shared.Monitor, error)
	FindActivePaginated(ctx context.Context, page int, limit int) ([]*shared.Monitor, error)
}

// Producer is responsible for scheduling monitor health checks
type Producer struct {
	rdb                     *redis.Client
	runner                  check.Runner // enqueues the checks for the worker
	monitorService          MonitorFinder
	loader                  *check.Loader
	logger                  *zap.SugaredLogger
	ctx                     context.Context
	cancel                  context.CancelFunc
	syncCtx                 context.Context    // context for monitor syncing (leader-only tasks)
	syncCancel              context.CancelFunc // cancel function for monitor syncing
	wg                      sync.WaitGroup
	mu                      sync.RWMutex
	monitorIntervals        map[string]int // monitor_id -> interval in seconds
	scheduleRefreshInterval time.Duration
	reclaimInterval         time.Duration // how often expired leases are swept back to the due set
	leadershipCheckInterval time.Duration // how often syncing is started or stopped to follow leadership
	leaderElection          *LeaderElection
	concurrency             int // number of concurrent producer goroutines
	watchdog                *stale.Watchdog
	probeSweep              *probe.Sweep // nil when the producer has none to run
}
