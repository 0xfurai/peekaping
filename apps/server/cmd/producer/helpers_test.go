package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the producer does on its own
const wait = 10 * time.Second

// loadConfig loads the producer's config from the environment, with a SQLite database and an
// in-process Redis of its own, changed by env
func loadConfig(t *testing.T, env map[string]string) (*Config, *miniredis.Miniredis) {
	t.Helper()
	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	t.Setenv("REDIS_HOST", redisHost)
	t.Setenv("REDIS_PORT", redisPort)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	t.Setenv("MODE", "test")
	t.Setenv("PRODUCER_CONCURRENCY", "2")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg, redis
}

// migrateDatabase migrates the database before the producer starts, as the startup script does
// with cmd/bun
func migrateDatabase(t testing.TB, cfg *Config) {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: cfg.DBType, DBName: cfg.DBName}, zap.NewNop().Sugar())
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
}

// insertMonitor adds an active push monitor to the migrated database
func insertMonitor(t testing.TB, cfg *Config, id string) {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: cfg.DBType, DBName: cfg.DBName}, zap.NewNop().Sugar())
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active, status, config, push_token)
		VALUES (?, 'push', ?, 60, 16, 0, 60, 0, 1, 0, ?, ?)`,
		id, "Monitor "+id, fmt.Sprintf(`{"pushToken":%q}`, id), id)
	require.NoError(t, err)
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

// inspector reads the queues of the Redis, as the worker would see them
func inspector(t testing.TB, redis *miniredis.Miniredis) *asynq.Inspector {
	t.Helper()
	i := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr()})
	t.Cleanup(func() { _ = i.Close() })
	return i
}

// instance is a producer running in the test
type instance struct {
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// startProducer runs the producer with a logger the test observes
func startProducer(t testing.TB, cfg *Config, logger *zap.SugaredLogger) *instance {
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
