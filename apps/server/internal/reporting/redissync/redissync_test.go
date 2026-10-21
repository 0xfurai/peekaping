package redissync_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"
	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestKey(t *testing.T) {
	assert.Equal(t, "peekaping:error_reporting:enabled", redissync.Key)
}

func TestPublish_WritesTheChoiceAsTrueOrFalse(t *testing.T) {
	server, client := newRedis(t)

	require.NoError(t, redissync.Publish(context.Background(), client, true))
	value, err := server.Get(redissync.Key)
	require.NoError(t, err)
	assert.Equal(t, "true", value)

	require.NoError(t, redissync.Publish(context.Background(), client, false))
	value, err = server.Get(redissync.Key)
	require.NoError(t, err)
	assert.Equal(t, "false", value)
	assert.Zero(t, server.TTL(redissync.Key), "the key stays, so a process that starts later reads it too")
}

func TestPublish_FailsWhenRedisIsDown(t *testing.T) {
	server, client := newRedis(t)
	server.Close()

	assert.Error(t, redissync.Publish(context.Background(), client, true))
}

func TestRefresh_FollowsTheKey(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	server, client := newRedis(t)
	refresher := redissync.NewRefresher(client, time.Minute)

	require.NoError(t, server.Set(redissync.Key, "true"))
	refresher.Refresh(context.Background())
	assert.True(t, reporting.Enabled())

	require.NoError(t, server.Set(redissync.Key, "false"))
	refresher.Refresh(context.Background())
	assert.False(t, reporting.Enabled())
}

func TestRefresh_KeepsTheStateWhenThereIsNothingToRead(t *testing.T) {
	for name, tc := range map[string]struct {
		before  bool
		prepare func(*miniredis.Miniredis)
	}{
		"a missing key, while off": {false, func(*miniredis.Miniredis) {}},
		"a missing key, while on":  {true, func(*miniredis.Miniredis) {}},
		"a value it doesn't know":  {true, func(s *miniredis.Miniredis) { _ = s.Set(redissync.Key, "maybe") }},
		"Redis down, while on":     {true, func(s *miniredis.Miniredis) { s.Close() }},
		"Redis down, while off":    {false, func(s *miniredis.Miniredis) { s.Close() }},
	} {
		t.Run(name, func(t *testing.T) {
			reportingtest.Install(t)
			reporting.SetEnabled(tc.before)
			server, client := newRedis(t)
			tc.prepare(server)

			redissync.NewRefresher(client, time.Minute).Refresh(context.Background())

			assert.Equal(t, tc.before, reporting.Enabled())
		})
	}
}

func TestRefresh_NeverTurnsOnAProcessTheEnvironmentForcesOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.Setup(reporting.Options{DSN: "https://public@sentry.invalid/1", ForcedOff: true, Transport: recorder})
	server, client := newRedis(t)
	require.NoError(t, server.Set(redissync.Key, "true"))

	redissync.NewRefresher(client, time.Minute).Refresh(context.Background())

	assert.False(t, reporting.Enabled())
}

func TestRun_ReadsAtOnceAndThenEveryInterval(t *testing.T) {
	reportingtest.Install(t)
	reporting.SetEnabled(false)
	server, client := newRedis(t)
	require.NoError(t, server.Set(redissync.Key, "true"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		redissync.NewRefresher(client, 10*time.Millisecond).Run(ctx)
	}()

	require.Eventually(t, reporting.Enabled, 2*time.Second, 5*time.Millisecond, "the key is read when it starts")
	require.NoError(t, server.Set(redissync.Key, "false"))
	require.Eventually(t, func() bool { return !reporting.Enabled() }, 2*time.Second, 5*time.Millisecond, "and again after the interval")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}
