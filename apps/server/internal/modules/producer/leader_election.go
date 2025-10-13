package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// LeaderKey is the Redis key used for leader election
	LeaderKey = "peekaping:producer:leader"
	// LeaderTTL is how long the leader lock is valid
	LeaderTTL = 10 * time.Second
	// LeaderRenewalInterval is how often the leader renews its lock
	LeaderRenewalInterval = 5 * time.Second
)

// LeaderElection handles distributed leader election using Redis
type LeaderElection struct {
	client   *redis.Client
	logger   *zap.SugaredLogger
	nodeID   string
	isLeader bool
	stopChan chan struct{}
	doneChan chan struct{}
}

// NewLeaderElection creates a new leader election instance
func NewLeaderElection(client *redis.Client, nodeID string, logger *zap.SugaredLogger) *LeaderElection {
	return &LeaderElection{
		client:   client,
		logger:   logger.With("component", "leader_election"),
		nodeID:   nodeID,
		isLeader: false,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Start begins the leader election process
func (le *LeaderElection) Start(ctx context.Context) {
	le.logger.Infof("Starting leader election for node: %s", le.nodeID)

	go func() {
		defer close(le.doneChan)

		ticker := time.NewTicker(LeaderRenewalInterval)
		defer ticker.Stop()

		// Try to become leader immediately
		le.tryBecomeLeader(ctx)

		for {
			select {
			case <-ticker.C:
				le.tryBecomeLeader(ctx)
			case <-le.stopChan:
				le.logger.Info("Stopping leader election")
				le.releaseLeadership(ctx)
				return
			case <-ctx.Done():
				le.logger.Info("Context cancelled, stopping leader election")
				// Use a fresh context for cleanup since the original context is canceled
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				le.releaseLeadership(cleanupCtx)
				return
			}
		}
	}()
}

// Stop stops the leader election process
func (le *LeaderElection) Stop() {
	close(le.stopChan)
	<-le.doneChan
}

// IsLeader returns true if this node is currently the leader
func (le *LeaderElection) IsLeader() bool {
	return le.isLeader
}

// tryBecomeLeader attempts to acquire or renew leadership
func (le *LeaderElection) tryBecomeLeader(ctx context.Context) {
	// Try to set the key with NX (only if not exists) and EX (expiration)
	success, err := le.client.SetNX(ctx, LeaderKey, le.nodeID, LeaderTTL).Result()
	if err != nil {
		le.logger.Errorw("Failed to acquire leadership", "error", err)
		le.setLeaderStatus(false)
		return
	}

	if success {
		// We successfully became the leader
		if !le.isLeader {
			le.logger.Infow("Became leader", "node_id", le.nodeID)
		}
		le.setLeaderStatus(true)
		return
	}

	// Key already exists, check if we are the current leader
	currentLeader, err := le.client.Get(ctx, LeaderKey).Result()
	if err != nil {
		if err != redis.Nil {
			le.logger.Errorw("Failed to check current leader", "error", err)
		}
		le.setLeaderStatus(false)
		return
	}

	if currentLeader == le.nodeID {
		// We are already the leader, renew the lock
		err = le.client.Expire(ctx, LeaderKey, LeaderTTL).Err()
		if err != nil {
			le.logger.Errorw("Failed to renew leadership", "error", err)
			le.setLeaderStatus(false)
			return
		}
		le.setLeaderStatus(true)
	} else {
		// Another node is the leader
		if le.isLeader {
			le.logger.Warnw("Lost leadership", "current_leader", currentLeader)
		}
		le.setLeaderStatus(false)
	}
}

// releaseLeadership releases the leadership if this node is the leader
func (le *LeaderElection) releaseLeadership(ctx context.Context) {
	if !le.isLeader {
		return
	}

	// Use a Lua script to ensure we only delete if we are the current leader
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	_, err := le.client.Eval(ctx, script, []string{LeaderKey}, le.nodeID).Result()
	if err != nil {
		le.logger.Errorw("Failed to release leadership", "error", err)
	} else {
		le.logger.Infow("Released leadership", "node_id", le.nodeID)
	}

	le.setLeaderStatus(false)
}

// setLeaderStatus updates the leader status
func (le *LeaderElection) setLeaderStatus(status bool) {
	le.isLeader = status
}

// WaitForLeadership blocks until this node becomes the leader or context is cancelled
func (le *LeaderElection) WaitForLeadership(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if le.IsLeader() {
			return nil
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for leadership")
		}
	}
}
