package producer

import (
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// RegisterDependencies registers producer dependencies with the DI container
func RegisterDependencies(container *dig.Container) {
	// Provide leader election
	container.Provide(func(client *redis.Client, logger *zap.SugaredLogger) *LeaderElection {
		// Generate a unique node ID (hostname + PID)
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		nodeID := fmt.Sprintf("%s-%d", hostname, os.Getpid())

		return NewLeaderElection(client, nodeID, logger)
	})

	// Provide producer
	container.Provide(NewProducer)

	// Provide event listener
	container.Provide(NewEventListener)
}
