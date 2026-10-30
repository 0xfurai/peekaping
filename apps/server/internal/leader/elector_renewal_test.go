package leader

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// beforeRenewal is a Redis hook that, once, runs change just before the first of the named
// commands runs: the lock changes hands after this node's attempt to take it failed, and before
// its renewal ran
type beforeRenewal struct {
	commands []string
	change   func()
	once     *sync.Once
}

func (beforeRenewal) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h beforeRenewal) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		for _, name := range h.commands {
			if cmd.Name() == name {
				h.once.Do(h.change)
			}
		}
		return next(ctx, cmd)
	}
}
func (beforeRenewal) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// leaderBeforeRenewal is a node1 that leads, on a Redis where change runs before its renewal. The
// renewal is a GET then an EXPIRE in one version and a single script in the other, so the hook
// fires before whichever of "expire" and "eval" it sends.
func leaderBeforeRenewal(t *testing.T, change func(mr *miniredis.Miniredis)) (*Elector, *miniredis.Miniredis) {
	t.Helper()
	client, mr := setupTestRedis(t)
	t.Cleanup(mr.Close)
	le := New(client, "node1", zap.NewNop().Sugar())
	le.tryBecomeLeader(context.Background())
	require.True(t, le.IsLeader())
	client.AddHook(beforeRenewal{
		commands: []string{"expire", "eval"},
		change:   func() { change(mr) },
		once:     &sync.Once{},
	})
	return le, mr
}

// A leader whose lock changed hands before its renewal ran must not extend the other node's lock
func TestElector_TryBecomeLeader_ALeaderDoesNotExtendALockThatAnotherNodeTookBeforeTheRenewal(t *testing.T) {
	le, mr := leaderBeforeRenewal(t, func(mr *miniredis.Miniredis) {
		mr.Set(Key, "node2")
		mr.SetTTL(Key, 2*time.Second)
	})

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	holder, err := mr.Get(Key)
	require.NoError(t, err)
	assert.Equal(t, "node2", holder)
	assert.Equal(t, 2*time.Second, mr.TTL(Key), "the other node's lock keeps its own TTL")
}

// The lock can run out between the attempt to take it and the renewal: that is no failure, and the
// renewal does not bring the lock back
func TestElector_TryBecomeLeader_AnExpiredLockBeforeTheRenewalStopsLeadingWithoutAnError(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := New(client, "node1", zap.New(core).Sugar())
	le.tryBecomeLeader(context.Background())
	require.True(t, le.IsLeader())
	client.AddHook(beforeRenewal{
		commands: []string{"expire", "eval"},
		change:   func() { mr.Del(Key) },
		once:     &sync.Once{},
	})

	le.tryBecomeLeader(context.Background())

	assert.False(t, le.IsLeader())
	assert.False(t, mr.Exists(Key))
	assert.Empty(t, logs.FilterLevelExact(zapcore.ErrorLevel).All())
}

func TestElector_TryBecomeLeader_ALeaderThatHoldsTheLockExtendsItAndKeepsLeadingInTheSameTerm(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	le := New(client, "node1", zap.NewNop().Sugar())
	le.tryBecomeLeader(context.Background())
	require.True(t, le.IsLeader())
	_, term := le.leadership()
	mr.FastForward(TTL - 3*time.Second)
	require.Equal(t, 3*time.Second, mr.TTL(Key))

	le.tryBecomeLeader(context.Background())

	leads, termAfter := le.leadership()
	assert.True(t, leads)
	assert.Equal(t, term, termAfter)
	assert.Equal(t, TTL, mr.TTL(Key))
	holder, err := mr.Get(Key)
	require.NoError(t, err)
	assert.Equal(t, "node1", holder)
}
