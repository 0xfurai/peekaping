package producer

import (
	"fmt"
	"os"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/stale"
	"peekaping/internal/utils"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers producer dependencies with the DI container: the leader
// election, the producer and its event listener
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLeaderElection, ProvideProducer, NewEventListener)
}

// ProvideProducer provides the producer. dig matches parameter types exactly, so it hands the
// producer the monitor service it provides, and the producer reads it through its narrower
// interface.
func ProvideProducer(
	rdb *redis.Client,
	runner check.Runner,
	monitorService monitor.Service,
	loader *check.Loader,
	heartbeatService heartbeat.Service,
	staleStore stale.Store,
	eventBus events.EventBus,
	probeOffline stale.ProbeOfflineFunc,
	probes stale.ProbeLookup,
	probeSweep *probe.Sweep,
	leaderElection *LeaderElection,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) *Producer {
	return NewProducer(rdb, runner, monitorService, loader, heartbeatService, staleStore, eventBus, leaderElection, cfg, logger,
		WithProbeOffline(probeOffline), WithProbeLookup(probes), WithProbeSweep(probeSweep))
}

// ProvideQueueRunner provides cluster mode's check.Runner. dig matches parameter types exactly,
// so it hands the runner the queue service it provides, and the runner uses it through its
// narrower interface.
func ProvideQueueRunner(queueService queue.Service, logger *zap.SugaredLogger) check.Runner {
	return NewQueueRunner(queueService, logger)
}

// ProvideLeaderElection provides the leader election, as this process's node
func ProvideLeaderElection(client *redis.Client, logger *zap.SugaredLogger) *LeaderElection {
	// Generate a unique node ID (hostname + PID)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	nodeID := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	return NewLeaderElection(client, nodeID, logger)
}
