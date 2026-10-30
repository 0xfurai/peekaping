package leader

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// failCommands is a Redis hook that fails the commands fail names an error for, and lets the others through
type failCommands struct{ fail func(cmd redis.Cmder) error }

func (failCommands) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h failCommands) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.fail(cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func (failCommands) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

var errRedisDown = errors.New("redis is down")

// leaderThatLeads is a node1 that leads, on a Redis whose commands named by fail then fail
func leaderThatLeads(t *testing.T, fail func(cmd redis.Cmder) error) (*Elector, *observer.ObservedLogs) {
	t.Helper()
	client, mr := setupTestRedis(t)
	t.Cleanup(mr.Close)
	core, logs := observer.New(zapcore.DebugLevel)
	le := New(client, "node1", zap.New(core).Sugar())
	le.tryBecomeLeader(context.Background())
	require.True(t, le.IsLeader())
	require.Equal(t, 1, logs.FilterMessage("Became leader").Len())
	logs.TakeAll()
	client.AddHook(failCommands{fail})
	return le, logs
}

func named(name string) func(redis.Cmder) error {
	return func(cmd redis.Cmder) error {
		if cmd.Name() == name {
			return errRedisDown
		}
		return nil
	}
}

func TestElector_TryBecomeLeader_ALeaderThatCantReachRedisStopsLeading(t *testing.T) {
	le, logs := leaderThatLeads(t, named("set"))

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	failures := logs.FilterMessage("Failed to acquire leadership").All()
	require.Len(t, failures, 1)
	assert.Equal(t, zapcore.ErrorLevel, failures[0].Level)
	assert.Equal(t, "redis is down", failures[0].ContextMap()["error"])
}

func TestElector_TryBecomeLeader_ALeaderThatCantReadTheLockStopsLeading(t *testing.T) {
	le, logs := leaderThatLeads(t, named("get"))

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	failures := logs.FilterMessage("Failed to check current leader").All()
	require.Len(t, failures, 1)
	assert.Equal(t, zapcore.ErrorLevel, failures[0].Level)
	assert.Equal(t, "redis is down", failures[0].ContextMap()["error"])
}

// The lock can run out between the attempt to take it and the look at who holds it: that is no failure
func TestElector_TryBecomeLeader_AnExpiredLockBetweenTheTwoCommandsStopsLeadingWithoutAnError(t *testing.T) {
	le, logs := leaderThatLeads(t, func(cmd redis.Cmder) error {
		if cmd.Name() == "get" {
			return redis.Nil
		}
		return nil
	})

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	assert.Empty(t, logs.FilterLevelExact(zapcore.ErrorLevel).All())
}

func TestElector_TryBecomeLeader_ALeaderThatCantRenewTheLockStopsLeading(t *testing.T) {
	le, logs := leaderThatLeads(t, named("expire"))

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	failures := logs.FilterMessage("Failed to renew leadership").All()
	require.Len(t, failures, 1)
	assert.Equal(t, zapcore.ErrorLevel, failures[0].Level)
	assert.Equal(t, "redis is down", failures[0].ContextMap()["error"])
}

// A command that fails because the election is stopping says nothing of the leadership: the release
// that follows needs the status as it was
func TestElector_TryBecomeLeader_ACommandThatFailsWhileStoppingKeepsTheStatus(t *testing.T) {
	for _, command := range []string{"set", "get", "expire"} {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			le, logs := leaderThatLeads(t, func(cmd redis.Cmder) error {
				if cmd.Name() == command {
					cancel()
					return context.Canceled
				}
				return nil
			})

			le.tryBecomeLeader(ctx)

			assert.True(t, le.IsLeader())
			assert.Zero(t, logs.Len(), "nothing is logged")
		})
	}
}
