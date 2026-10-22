package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "Correct-horse-1"
	// wait is how long a test waits for something the app does on its own
	wait = 10 * time.Second
)

// loadConfig loads the API's config from the environment, with a SQLite database and an
// in-process Redis of its own, changed by env
func loadConfig(t *testing.T, env map[string]string) *Config {
	t.Helper()
	utils.RegisterCustomValidators()
	redis := miniredis.RunT(t)
	redisHost, redisPort, err := net.SplitHostPort(redis.Addr())
	require.NoError(t, err)
	t.Setenv("REDIS_HOST", redisHost)
	t.Setenv("REDIS_PORT", redisPort)
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", filepath.Join(t.TempDir(), "peekaping.db"))
	t.Setenv("MODE", "test")
	t.Setenv("METRICS_ENABLED", "")
	for name, value := range env {
		t.Setenv(name, value)
	}
	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)
	return cfg
}

// migrateDatabase migrates the database before the API starts, as the startup script does with
// cmd/bun
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

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

// instance is an API running in the test, on a listener of its own
type instance struct {
	url    string
	logs   *observer.ObservedLogs
	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// startAPI migrates the database and runs the API with a logger the test observes. It returns
// once the API answers. The test's cleanup stops it if the test didn't.
func startAPI(t testing.TB, cfg *Config, logger *zap.SugaredLogger, logs *observer.ObservedLogs) *instance {
	t.Helper()
	migrateDatabase(t, cfg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{
		url:    "http://" + listener.Addr().String(),
		logs:   logs,
		cancel: cancel,
		done:   make(chan error, 1),
	}
	go func() { in.done <- run(ctx, cfg, logger, listener) }()
	t.Cleanup(func() { _ = in.stop(t) })

	deadline := time.Now().Add(wait)
	for {
		resp, err := http.Get(in.url + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return in
			}
		}
		select {
		case err := <-in.done:
			require.FailNow(t, "the API stopped while starting", "%v", err)
		case <-time.After(20 * time.Millisecond):
		}
		require.True(t, time.Now().Before(deadline), "the API didn't answer within %s", wait)
	}
}

// stop cancels run's context, as SIGTERM does, and returns what run returned. It fails the test
// if run takes longer than the shutdown deadline.
func (in *instance) stop(t testing.TB) error {
	t.Helper()
	in.stopOnce.Do(func() {
		in.cancel()
		limit := wait
		select {
		case in.stopErr = <-in.done:
		case <-time.After(limit):
			in.stopErr = fmt.Errorf("run didn't return within %s", limit)
			t.Error(in.stopErr)
		}
	})
	return in.stopErr
}
