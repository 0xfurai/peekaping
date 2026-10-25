// Package leader elects one leader among the processes of a cluster-mode install. The leader holds
// a lock in Redis: SET NX with a 10 second TTL, renewed every 5 seconds. A process that stops
// releases the lock, and one that dies loses it when the TTL runs out. Single mode has no
// election: its one process is always the leader.
package leader

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"peekaping/internal/reporting"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// Key is the Redis key that holds the leader lock. It keeps the name it had when only the
	// producer elected a leader: during a rolling upgrade, the producers still running the old
	// version and the processes running the new one contest the same lock, so the cluster has one
	// leader in all of them. A new name would let one of each lead at once.
	Key = "peekaping:producer:leader"
	// TTL is how long the leader lock is valid
	TTL = 10 * time.Second
	// RenewalInterval is how often the leader renews its lock, and a follower tries to take it
	RenewalInterval = 5 * time.Second
)

// Elector elects a leader among the processes that use one Redis, by a lock with a TTL
type Elector struct {
	client          *redis.Client
	logger          *zap.SugaredLogger
	nodeID          string
	state           atomic.Uint64 // the leadership term and whether the node leads: see leadership
	renewalInterval time.Duration // how often the lock is taken or renewed
	stopChan        chan struct{}
	stopOnce        sync.Once
	doneChan        chan struct{}
}

// Option changes how an Elector behaves, for a caller that has a reason to
type Option func(*Elector)

// WithRenewalInterval sets how often the elector takes or renews the lock, instead of
// RenewalInterval. An interval of TTL or more lets a leader lose the lock between renewals, so
// only a test that has to wait for an election sets it, to less.
func WithRenewalInterval(interval time.Duration) Option {
	return func(e *Elector) { e.renewalInterval = interval }
}

// New creates an elector for a node. The node ID tells the nodes apart in the lock, so each
// process needs its own: NodeID gives one.
func New(client *redis.Client, nodeID string, logger *zap.SugaredLogger, opts ...Option) *Elector {
	e := &Elector{
		client:          client,
		logger:          logger.With("component", "leader_election"),
		nodeID:          nodeID,
		renewalInterval: RenewalInterval,
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NodeID is this process's ID in the lock: its host name and PID
func NodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

// ProvideElector provides the elector, as this process's node
func ProvideElector(client *redis.Client, logger *zap.SugaredLogger) *Elector {
	return New(client, NodeID(), logger)
}

// Start begins the leader election process
func (e *Elector) Start(ctx context.Context) {
	e.logger.Infof("Starting leader election for node: %s", e.nodeID)

	reporting.Go("leader-election", func() {
		defer close(e.doneChan)

		ticker := time.NewTicker(e.renewalInterval)
		defer ticker.Stop()

		// Try to become leader immediately
		e.tryBecomeLeader(ctx)

		for {
			select {
			case <-ticker.C:
				e.tryBecomeLeader(ctx)
			case <-e.stopChan:
				e.logger.Info("Stopping leader election")
				e.releaseLeadership(ctx)
				return
			case <-ctx.Done():
				e.logger.Info("Context cancelled, stopping leader election")
				// Use a fresh context for cleanup since the original context is canceled
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				e.releaseLeadership(cleanupCtx)
				return
			}
		}
	})
}

// Done returns a channel that closes once the election has stopped, after it released the
// leadership it held. It stops when the context given to Start is done, or on Stop.
func (e *Elector) Done() <-chan struct{} {
	return e.doneChan
}

// Stop stops the leader election process, and returns once it released the leadership it held. It
// may be called again.
func (e *Elector) Stop() {
	e.stopOnce.Do(func() { close(e.stopChan) })
	<-e.doneChan
}

// leadingBit is the bit of the elector's state that says the node leads. The rest of the state
// counts the terms of leadership, so that one read says both.
const (
	leadingBit = 1
	termUnit   = 2
)

// IsLeader returns true if this node is currently the leader. It may be called from any goroutine.
func (e *Elector) IsLeader() bool {
	leads, _ := e.leadership()
	return leads
}

// leadership says whether this node leads, and in which term. A term is one stretch of
// leadership: it starts when the node becomes the leader and ends when it loses the lock, so a
// node that lost the leadership and got it back leads in a new term. The term is how a job that
// runs only while the node leads tells that it lost the leadership, when the node got it back
// before the job looked: it counts the terms the node has led in, so it doesn't change while the
// node follows. It may be called from any goroutine.
func (e *Elector) leadership() (leads bool, term uint64) {
	state := e.state.Load()
	return state&leadingBit != 0, state / termUnit
}

// tryBecomeLeader attempts to acquire or renew leadership
func (e *Elector) tryBecomeLeader(ctx context.Context) {
	// Try to set the key with NX (only if not exists) and EX (expiration)
	success, err := e.client.SetNX(ctx, Key, e.nodeID, TTL).Result()
	if err != nil {
		if ctx.Err() != nil {
			return // stopping: the release that follows needs the status as it was
		}
		e.logger.Errorw("Failed to acquire leadership", "error", err)
		e.setLeaderStatus(false)
		return
	}

	if success {
		// We successfully became the leader
		if !e.IsLeader() {
			e.logger.Infow("Became leader", "node_id", e.nodeID)
		}
		e.setLeaderStatus(true)
		return
	}

	// Key already exists, check if we are the current leader
	currentLeader, err := e.client.Get(ctx, Key).Result()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if err != redis.Nil {
			e.logger.Errorw("Failed to check current leader", "error", err)
		}
		e.setLeaderStatus(false)
		return
	}

	if currentLeader == e.nodeID {
		// We are already the leader, renew the lock
		err = e.client.Expire(ctx, Key, TTL).Err()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.logger.Errorw("Failed to renew leadership", "error", err)
			e.setLeaderStatus(false)
			return
		}
		e.setLeaderStatus(true)
	} else {
		// Another node is the leader
		if e.IsLeader() {
			e.logger.Warnw("Lost leadership", "current_leader", currentLeader)
		}
		e.setLeaderStatus(false)
	}
}

// releaseLeadership releases the leadership if this node is the leader
func (e *Elector) releaseLeadership(ctx context.Context) {
	if !e.IsLeader() {
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

	_, err := e.client.Eval(ctx, script, []string{Key}, e.nodeID).Result()
	if err != nil {
		e.logger.Errorw("Failed to release leadership", "error", err)
	} else {
		e.logger.Infow("Released leadership", "node_id", e.nodeID)
	}

	e.setLeaderStatus(false)
}

// setLeaderStatus updates the leader status. A node that was not the leader and now is starts a
// new term.
func (e *Elector) setLeaderStatus(status bool) {
	for {
		old := e.state.Load()
		next := old &^ leadingBit
		if status {
			next = old | leadingBit
			if old&leadingBit == 0 {
				next += termUnit
			}
		}
		if e.state.CompareAndSwap(old, next) {
			return
		}
	}
}

// WaitForLeadership blocks until this node becomes the leader or context is cancelled
func (e *Elector) WaitForLeadership(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if e.IsLeader() {
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
