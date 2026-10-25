package protocol

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// windowFixture is a Window with a clock the test moves: the memory window measures time by the
// `now` its callers pass, the Redis window by the time of the Redis server.
type windowFixture struct {
	window Window
	now    time.Time
	// advance moves the clock on
	advance func(d time.Duration)
}

func (f *windowFixture) claim(t testing.TB, probeID, id string) bool {
	t.Helper()
	claimed, err := f.window.Claim(context.Background(), probeID, id, f.now)
	require.NoError(t, err)
	return claimed
}

func (f *windowFixture) release(t testing.TB, probeID, id string) {
	t.Helper()
	require.NoError(t, f.window.Release(context.Background(), probeID, id))
}

// windowImplementations are the windows the results route can be given, each as a fixture with a
// window of an hour
var windowImplementations = map[string]func(t *testing.T) *windowFixture{
	"memory": func(t *testing.T) *windowFixture {
		f := &windowFixture{window: newAcceptedWindow(time.Hour), now: epoch}
		f.advance = func(d time.Duration) { f.now = f.now.Add(d) }
		return f
	},
	"redis": func(t *testing.T) *windowFixture {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		f := &windowFixture{window: NewRedisWindow(client), now: epoch}
		f.advance = func(d time.Duration) { server.FastForward(d); f.now = f.now.Add(d) }
		return f
	},
}

// forEachWindow runs the test on every implementation of Window: they have to answer alike
func forEachWindow(t *testing.T, test func(t *testing.T, f *windowFixture)) {
	t.Helper()
	for name, build := range windowImplementations {
		t.Run(name, func(t *testing.T) { test(t, build(t)) })
	}
}

func TestWindow_ExpiresAfterTTL(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))

		assert.False(t, f.claim(t, "probe-1", "result-1"), "an id is taken as soon as it is claimed")
		f.advance(time.Hour - time.Second)
		assert.False(t, f.claim(t, "probe-1", "result-1"), "and until the hour is out")
		f.advance(time.Second)
		assert.True(t, f.claim(t, "probe-1", "result-1"), "an id is free again an hour after it was claimed")
	})
}

func TestWindow_KeepsEachProbesIdsApart(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))

		assert.True(t, f.claim(t, "probe-2", "result-1"), "the ids are a probe's own, so another probe may use the same one")
		assert.True(t, f.claim(t, "probe-1", "result-2"))
	})
}

func TestWindow_ClaimingAgainDoesNotExtendTheWindow(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))
		f.advance(30 * time.Minute)
		require.False(t, f.claim(t, "probe-1", "result-1"))
		f.advance(30 * time.Minute)

		assert.True(t, f.claim(t, "probe-1", "result-1"), "the id was claimed at the first claim")
	})
}

// Two requests that claim an id at once are told apart by the window, before either has stored
// anything
func TestWindow_ClaimIsExclusive(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		var won atomic.Int64
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 64 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if f.claim(t, "probe-1", "result-1") {
					won.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()

		assert.EqualValues(t, 1, won.Load(), "exactly one claim of the id succeeds")
	})
}

func TestWindow_ReleasedIdCanBeClaimedAgain(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "failed"))
		require.True(t, f.claim(t, "probe-1", "stored"))

		f.release(t, "probe-1", "failed")

		assert.True(t, f.claim(t, "probe-1", "failed"), "the probe's retry of a result that was not stored is accepted")
		assert.False(t, f.claim(t, "probe-1", "stored"), "and what was stored stays taken")
	})
}

func TestWindow_ReleaseLeavesOthersAlone(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))

		f.release(t, "probe-2", "result-1")
		f.release(t, "probe-1", "never-claimed")

		assert.False(t, f.claim(t, "probe-1", "result-1"), "another probe's release and an id nobody claimed change nothing")
	})
}

// A window that held every id it had ever seen would grow for as long as the server runs
func TestAcceptedWindow_ForgetsExpiredIdsAndIdleProbes(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	claim := func(probeID, id string, now time.Time) bool {
		claimed, err := window.Claim(context.Background(), probeID, id, now)
		require.NoError(t, err)
		return claimed
	}
	for i := range 100 {
		claim("idle-probe", "result-"+strconv.Itoa(i), epoch)
	}
	claim("busy-probe", "old", epoch)

	later := epoch.Add(2 * time.Hour)
	claim("busy-probe", "new", later)

	assert.Equal(t, 1, window.probeCount(), "the probe that went quiet is forgotten")
	assert.Equal(t, 1, window.idCount(), "and so is the old id of the one that is not")
	assert.False(t, claim("busy-probe", "new", later))
	assert.True(t, claim("busy-probe", "old", later))
}

func TestAcceptedWindow_ReleasingTheLastIdForgetsTheProbe(t *testing.T) {
	window := newAcceptedWindow(time.Hour)
	_, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)

	require.NoError(t, window.Release(context.Background(), "probe-1", "result-1"))

	assert.Zero(t, window.probeCount(), "a probe left with no ids is forgotten")
}

// The Redis window keeps an id under peekaping:probe:accepted:<probe id>:<result id> for an hour
func TestRedisWindow_KeepsTheIdInRedisForAnHour(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	window := NewRedisWindow(client)

	claimed, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.True(t, claimed)

	key := "peekaping:probe:accepted:probe-1:result-1"
	assert.True(t, server.Exists(key))
	assert.Equal(t, time.Hour, server.TTL(key))
	require.NoError(t, window.Release(context.Background(), "probe-1", "result-1"))
	assert.False(t, server.Exists(key), "a release deletes the key")
}

// Two API replicas on one Redis share the window: what one claimed, the other finds taken
func TestRedisWindow_IsSharedByReplicasOfOneRedis(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, first.Close()); assert.NoError(t, second.Close()) })

	claimed, err := NewRedisWindow(first).Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = NewRedisWindow(second).Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	assert.False(t, claimed, "the other replica finds the id taken")
}

// A claim that can't ask Redis is an error, not a free id: the batch is answered 503 and the probe
// sends it again
func TestRedisWindow_ClaimFailsWhenRedisCannotBeReached(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	window := NewRedisWindow(client)
	server.Close()

	claimed, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)

	require.Error(t, err)
	assert.False(t, claimed)
	assert.False(t, errors.Is(err, context.Canceled))
}

// A release goes through even when the request that made it was cancelled: that is the request that
// gave up halfway, and its ids have to be free for the probe's retry
func TestRedisWindow_ReleaseSurvivesACancelledRequest(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	window := NewRedisWindow(client)
	claimed, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.True(t, claimed)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, window.Release(cancelled, "probe-1", "result-1"))

	claimed, err = window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	assert.True(t, claimed)
}
