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

// claim claims the id and reports whether the claim won
func (f *windowFixture) claim(t testing.TB, probeID, id string) bool {
	t.Helper()
	return f.outcome(t, probeID, id) == ClaimWon
}

// outcome claims the id and reports what the claim found
func (f *windowFixture) outcome(t testing.TB, probeID, id string) ClaimOutcome {
	t.Helper()
	outcome, err := f.window.Claim(context.Background(), probeID, id, f.now)
	require.NoError(t, err)
	return outcome
}

// keep marks the ids as stored, as a request does once it has stored their results
func (f *windowFixture) keep(t testing.TB, probeID string, ids ...string) {
	t.Helper()
	require.NoError(t, f.window.Keep(context.Background(), probeID, ids...))
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
		f.keep(t, "probe-1", "result-1")

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
		f.keep(t, "probe-1", "result-1")
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

func TestWindow_AKeptIdStaysTakenForTheHour(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))
		require.True(t, f.claim(t, "probe-1", "result-2"))
		require.True(t, f.claim(t, "probe-2", "result-1"))

		f.keep(t, "probe-1", "result-1", "result-2")
		f.advance(ClaimTTL + time.Second)

		assert.False(t, f.claim(t, "probe-1", "result-1"), "past the time a claim lasts, a stored result is still taken")
		assert.False(t, f.claim(t, "probe-1", "result-2"), "all the ids of the call")
		f.advance(time.Hour - ClaimTTL - 2*time.Second)
		assert.False(t, f.claim(t, "probe-1", "result-1"), "until the hour is out")
		f.advance(time.Second)
		assert.True(t, f.claim(t, "probe-1", "result-1"), "and then it is free")
	})
}

// A claim says whether the result is stored or its request may still be storing it, or may have
// died: a retry that finds the second is not told the result is a duplicate, which the probe drops
// for good
func TestWindow_AClaimedIdIsPendingUntilItIsKept(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.Equal(t, ClaimWon, f.outcome(t, "probe-1", "result-1"))

		assert.Equal(t, ClaimPending, f.outcome(t, "probe-1", "result-1"), "claimed, and its result is not stored yet")
		assert.Equal(t, ClaimPending, f.outcome(t, "probe-1", "result-1"), "however often it is asked")
		f.keep(t, "probe-1", "result-1")
		assert.Equal(t, ClaimStored, f.outcome(t, "probe-1", "result-1"), "its result is stored")
		assert.Equal(t, ClaimWon, f.outcome(t, "probe-2", "result-1"), "another probe's id is its own")
	})
}

func TestWindow_AReleasedPendingIdIsFree(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.Equal(t, ClaimWon, f.outcome(t, "probe-1", "result-1"))
		require.Equal(t, ClaimPending, f.outcome(t, "probe-1", "result-1"))

		f.release(t, "probe-1", "result-1")

		assert.Equal(t, ClaimWon, f.outcome(t, "probe-1", "result-1"))
	})
}

// Asking about a pending id does not hold it any longer: a retry every few seconds must not keep
// the claim of a request that died alive
func TestRedisWindow_AskingAboutAPendingClaimDoesNotExtendIt(t *testing.T) {
	f := windowImplementations["redis"](t)
	require.Equal(t, ClaimWon, f.outcome(t, "probe-1", "died"))

	f.advance(ClaimTTL - time.Second)
	assert.Equal(t, ClaimPending, f.outcome(t, "probe-1", "died"))
	f.advance(time.Second)

	assert.Equal(t, ClaimWon, f.outcome(t, "probe-1", "died"), "the claim ran out at ClaimTTL after it was made")
}

// A key that an older server wrote with the hour (value 1, before a claim said what it was) is a
// stored id: the safe reading
func TestRedisWindow_AnIdWrittenByAnOlderServerIsStored(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	require.NoError(t, server.Set("peekaping:probe:accepted:probe-1:old", "1"))
	server.SetTTL("peekaping:probe:accepted:probe-1:old", time.Hour)

	outcome, err := NewRedisWindow(client).Claim(context.Background(), "probe-1", "old", epoch)

	require.NoError(t, err)
	assert.Equal(t, ClaimStored, outcome)
}

func TestWindow_KeepingNoIdsDoesNothing(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))

		f.keep(t, "probe-1")

		assert.False(t, f.claim(t, "probe-1", "result-1"))
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

// A release gives back a claim whose result was not stored. An id that was kept is a stored result,
// and nothing a request does with the id afterwards may forget it.
func TestWindow_ReleaseLeavesAKeptIdAlone(t *testing.T) {
	forEachWindow(t, func(t *testing.T, f *windowFixture) {
		require.True(t, f.claim(t, "probe-1", "result-1"))
		f.keep(t, "probe-1", "result-1")

		f.release(t, "probe-1", "result-1")

		assert.Equal(t, ClaimStored, f.outcome(t, "probe-1", "result-1"))
	})
}

// A request that ran past its claim (which ran out) releases its ids when it ends. Another request
// may have claimed, stored and kept the same ids by then: that stored mark is not the first
// request's to remove, or the probe's next retry would store the result a third time.
func TestRedisWindow_ARequestThatOutlivedItsClaimCannotReleaseAnotherRequestsStoredId(t *testing.T) {
	f := windowImplementations["redis"](t)
	require.True(t, f.claim(t, "probe-1", "result-1"), "request A")
	f.advance(ClaimTTL + time.Second)
	require.True(t, f.claim(t, "probe-1", "result-1"), "request B, after A's claim ran out")
	f.keep(t, "probe-1", "result-1")

	f.release(t, "probe-1", "result-1") // A ends, and gives back what it claimed

	assert.Equal(t, ClaimStored, f.outcome(t, "probe-1", "result-1"), "B's result is still stored")
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
		outcome, err := window.Claim(context.Background(), probeID, id, now)
		require.NoError(t, err)
		return outcome == ClaimWon
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

// The Redis window holds a claim under peekaping:probe:accepted:<probe id>:<result id> for ClaimTTL,
// and an id that was kept for an hour
func TestRedisWindow_HoldsAClaimForClaimTTLAndAKeptIdForAnHour(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	window := NewRedisWindow(client)

	outcome, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, outcome)

	key := "peekaping:probe:accepted:probe-1:result-1"
	assert.True(t, server.Exists(key))
	assert.Equal(t, ClaimTTL, server.TTL(key), "a request may die before it stores the result, so the claim is short")
	value, err := server.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "pending", value, "and says that its result is not stored yet")
	require.NoError(t, window.Keep(context.Background(), "probe-1", "result-1"))
	assert.Equal(t, time.Hour, server.TTL(key), "a stored result is remembered for the hour")
	value, err = server.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "stored", value, "and says that it is stored")
	require.NoError(t, window.Release(context.Background(), "probe-1", "result-1"))
	assert.True(t, server.Exists(key), "a release leaves a kept id")

	outcome, err = window.Claim(context.Background(), "probe-1", "result-2", epoch)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, outcome)
	require.NoError(t, window.Release(context.Background(), "probe-1", "result-2"))
	assert.False(t, server.Exists("peekaping:probe:accepted:probe-1:result-2"), "and deletes a claim")
}

// A request that dies (OOM, SIGKILL, eviction of the pod) between its claim and the end of the
// request neither releases nor keeps its ids. The probe's retry, on another replica, has to be
// accepted soon, not an hour later.
func TestRedisWindow_ForgetsAClaimThatWasNeitherReleasedNorKept(t *testing.T) {
	f := windowImplementations["redis"](t)
	require.True(t, f.claim(t, "probe-1", "died"))
	require.True(t, f.claim(t, "probe-1", "stored"))
	f.keep(t, "probe-1", "stored")

	f.advance(ClaimTTL - time.Second)
	assert.False(t, f.claim(t, "probe-1", "died"), "while a request could still be storing it, the id is taken")

	f.advance(time.Second)
	assert.True(t, f.claim(t, "probe-1", "died"), "the claim of a request that died runs out")
	assert.False(t, f.claim(t, "probe-1", "stored"), "and what was stored is not forgotten")
}

// A request that ran longer than a claim lasts stored its results all the same: keeping the ids
// takes them again
func TestRedisWindow_KeepingAnIdWhoseClaimRanOutTakesItAgain(t *testing.T) {
	f := windowImplementations["redis"](t)
	require.True(t, f.claim(t, "probe-1", "slow"))
	f.advance(ClaimTTL + time.Second)

	f.keep(t, "probe-1", "slow")

	assert.False(t, f.claim(t, "probe-1", "slow"))
	f.advance(time.Hour - time.Second)
	assert.False(t, f.claim(t, "probe-1", "slow"), "for the hour")
}

// Keeping goes through even when the request that made the call was cancelled, as a release does:
// the results are stored, and an id left to run out would store them again
func TestRedisWindow_KeepSurvivesACancelledRequest(t *testing.T) {
	f := windowImplementations["redis"](t)
	require.True(t, f.claim(t, "probe-1", "result-1"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, f.window.Keep(cancelled, "probe-1", "result-1"))

	f.advance(ClaimTTL + time.Second)
	assert.False(t, f.claim(t, "probe-1", "result-1"))
}

// A Keep that can't ask Redis is an error, for the caller to log: the result is stored already
func TestRedisWindow_KeepFailsWhenRedisCannotBeReached(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	window := NewRedisWindow(client)
	server.Close()

	require.Error(t, window.Keep(context.Background(), "probe-1", "result-1"))
	assert.NoError(t, window.Keep(context.Background(), "probe-1"), "no ids, nothing to ask Redis")
}

// Two API replicas on one Redis share the window: what one claimed, the other finds taken
func TestRedisWindow_IsSharedByReplicasOfOneRedis(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, first.Close()); assert.NoError(t, second.Close()) })

	outcome, err := NewRedisWindow(first).Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, outcome)

	outcome, err = NewRedisWindow(second).Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	assert.Equal(t, ClaimPending, outcome, "the other replica finds the id taken, by a request that has not stored its result yet")
}

// A claim that can't ask Redis is an error, not a free id: the batch is answered 503 and the probe
// sends it again
func TestRedisWindow_ClaimFailsWhenRedisCannotBeReached(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	window := NewRedisWindow(client)
	server.Close()

	outcome, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)

	require.Error(t, err)
	assert.Zero(t, outcome, "no outcome: the id is not claimed")
	assert.False(t, errors.Is(err, context.Canceled))
}

// A release goes through even when the request that made it was cancelled: that is the request that
// gave up halfway, and its ids have to be free for the probe's retry
func TestRedisWindow_ReleaseSurvivesACancelledRequest(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	window := NewRedisWindow(client)
	outcome, err := window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	require.Equal(t, ClaimWon, outcome)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, window.Release(cancelled, "probe-1", "result-1"))

	outcome, err = window.Claim(context.Background(), "probe-1", "result-1", epoch)
	require.NoError(t, err)
	assert.Equal(t, ClaimWon, outcome)
}
