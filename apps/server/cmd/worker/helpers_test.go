package main

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the worker does on its own
const wait = 10 * time.Second

// loadConfig loads the worker's config from the environment, with an in-process Redis of its
// own, changed by env
func loadConfig(t *testing.T, env map[string]string) (*Config, *miniredis.Miniredis) {
	t.Helper()
	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	t.Setenv("REDIS_HOST", redisHost)
	t.Setenv("REDIS_PORT", redisPort)
	t.Setenv("MODE", "test")
	t.Setenv("QUEUE_CONCURRENCY", "4")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg, redis
}

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

// logged returns the messages logged that are among messages, in the order they were logged
func logged(logs *observer.ObservedLogs, messages []string) []string {
	var got []string
	for _, entry := range logs.All() {
		if slices.Contains(messages, entry.Message) {
			got = append(got, entry.Message)
		}
	}
	return got
}

// instance is a worker running in the test
type instance struct {
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// startWorker runs the worker with a logger the test observes
func startWorker(t testing.TB, cfg *Config, logger *zap.SugaredLogger) *instance {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{cancel: cancel, done: make(chan error, 1)}
	go func() { in.done <- run(ctx, cfg, logger) }()
	t.Cleanup(func() { _ = in.stop(t) })
	return in
}

// stop cancels run's context, as SIGTERM does, and returns what run returned. It fails the test
// if run takes longer than the shutdown deadline.
func (in *instance) stop(t testing.TB) error {
	t.Helper()
	in.stopOnce.Do(func() {
		in.cancel()
		limit := shutdownTimeouts.Total() + 5*time.Second
		select {
		case in.stopErr = <-in.done:
		case <-time.After(limit):
			in.stopErr = fmt.Errorf("run didn't return within %s", limit)
			t.Error(in.stopErr)
		}
	})
	return in.stopErr
}

// queues is the queues of the Redis, as the worker and the ingester see them
type queues struct {
	client    *asynq.Client
	inspector *asynq.Inspector
}

func newQueues(t testing.TB, redis *miniredis.Miniredis) *queues {
	t.Helper()
	redisOpt := asynq.RedisClientOpt{Addr: redis.Addr()}
	q := &queues{client: asynq.NewClient(redisOpt), inspector: asynq.NewInspector(redisOpt)}
	t.Cleanup(func() {
		_ = q.client.Close()
		_ = q.inspector.Close()
	})
	return q
}
