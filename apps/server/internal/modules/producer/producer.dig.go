package producer

import (
	"fmt"
	"os"

	"peekaping/internal/utils"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers producer dependencies with the DI container: the leader
// election, the producer and its event listener
func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, ProvideLeaderElection, NewProducer, NewEventListener)
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
