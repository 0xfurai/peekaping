package main

import (
	"context"
	"database/sql"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the ingester does on its own
const wait = 10 * time.Second

// loadConfig loads the ingester's config from the environment, with a SQLite database and an
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
	t.Setenv("QUEUE_CONCURRENCY", "4")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg, redis
}

// migrateDatabase migrates the database before the ingester starts, as the startup script does
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

// storedHeartbeats counts the monitor's heartbeats in the database, read from a connection of
// its own once the ingester has stopped
func storedHeartbeats(t testing.TB, cfg *Config, monitorID string) int {
	t.Helper()
	db, err := sql.Open(sqliteshim.ShimName, "file:"+cfg.DBName+"?mode=rw")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM heartbeats WHERE monitor_id = ?`, monitorID).Scan(&count))
	return count
}

// lockDatabase holds the SQLite database's write lock from a connection of its own, as another
// writer would, until unlock is called. The ingester's writes wait for it.
func lockDatabase(t *testing.T, path string) (unlock func()) {
	t.Helper()
	ctx := context.Background()
	// Without the app's shared cache, so that the two connections lock each other out
	db, err := sql.Open(sqliteshim.ShimName, "file:"+path+"?mode=rw")
	require.NoError(t, err)
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "PRAGMA busy_timeout=5000")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)

	var once sync.Once
	unlock = func() {
		once.Do(func() {
			_, err := conn.ExecContext(ctx, "ROLLBACK")
			assert.NoError(t, err)
			assert.NoError(t, conn.Close())
			assert.NoError(t, db.Close())
		})
	}
	t.Cleanup(unlock)
	return unlock
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

// instance is an ingester running in the test
type instance struct {
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// startIngester runs the ingester with a logger the test observes, and returns once it serves
func startIngester(t testing.TB, cfg *Config, logger *zap.SugaredLogger, logs *observer.ObservedLogs) *instance {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{cancel: cancel, done: make(chan error, 1)}
	go func() { in.done <- run(ctx, cfg, logger) }()
	t.Cleanup(func() { _ = in.stop(t) })
	require.Eventually(t, func() bool { return logs.FilterMessage("Serving: the ingester is running").Len() == 1 },
		wait, 10*time.Millisecond, "the ingester didn't start")
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

// queues is the queues of the Redis, as the ingester sees them
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
