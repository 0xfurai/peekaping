package shutdown_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// wait is how long a test waits for something the code under test does on its own
const wait = 10 * time.Second

// observedLogger is a logger whose Info and higher entries the test reads
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.InfoLevel)
	return zap.New(core).Sugar(), logs
}

func TestTimeouts_TotalIsTheSumOfTheSteps(t *testing.T) {
	timeouts := shutdown.Timeouts{HTTP: 15 * time.Second, Jobs: 10 * time.Second, EventBus: 5 * time.Second}

	assert.Equal(t, 30*time.Second, timeouts.Total())
}

func TestNotifySignals_StopCancelsTheContextAndMayBeCalledAgain(t *testing.T) {
	logger, _ := observedLogger()
	ctx, stop := shutdown.NotifySignals(logger, func(code int) { assert.Fail(t, "exit was called", "code %d", code) })

	require.NoError(t, ctx.Err())
	stop()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(wait):
		require.FailNow(t, "stop didn't cancel the context")
	}
}

func TestAwait_ReturnsOnceDoneClosesBeforeTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	done := make(chan struct{})
	awaited := make(chan error, 1)
	go func() { awaited <- shutdown.Await(ctx, "the scheduler", done) }()

	select {
	case err := <-awaited:
		require.FailNow(t, "Await returned before done closed", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(done)

	select {
	case err := <-awaited:
		assert.NoError(t, err)
	case <-time.After(wait):
		require.FailNow(t, "Await didn't return once done closed")
	}
}

func TestAwait_FailsNamingTheStepWhenTheContextIsDoneFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := shutdown.Await(ctx, "the scheduler", make(chan struct{}))

	require.Error(t, err)
	assert.EqualError(t, err, "the scheduler didn't stop in time: context deadline exceeded")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// Both channels are ready when the context is already done. A select picks one of them at random,
// so one call could fail by chance: the loop makes a missing check fail every time.
func TestAwait_CountsADoneThatClosedEvenOnceTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	close(done)

	for range 100 {
		require.NoError(t, shutdown.Await(ctx, "the scheduler", done))
	}
}

func TestCloseDatabase_CheckpointsASQLiteDatabaseAndClosesIt(t *testing.T) {
	logger, logs := observedLogger()
	db := openSQLite(t)
	queries := &recordQueries{}
	db.AddQueryHook(queries)

	require.NoError(t, shutdown.CloseDatabase(db, "sqlite", logger))

	assert.Contains(t, queries.queries, "PRAGMA wal_checkpoint(TRUNCATE)")
	assert.Error(t, db.Ping(), "the database is still open")
	assert.Equal(t, 1, logs.FilterMessage("SQLite database closed gracefully").Len())
}

func TestCloseDatabase_ClosesOtherDatabasesWithoutCheckpointing(t *testing.T) {
	logger, _ := observedLogger()
	// A SQLite handle stands in for the other storage backends, which close the same way
	db := openSQLite(t)
	queries := &recordQueries{}
	db.AddQueryHook(queries)

	require.NoError(t, shutdown.CloseDatabase(db, "postgres", logger))

	assert.Empty(t, queries.queries)
	assert.Error(t, db.Ping(), "the database is still open")
}

func openSQLite(t *testing.T) *bun.DB {
	t.Helper()
	t.Setenv("BUNDEBUG", "0")
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "peekaping.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// recordQueries is a bun query hook that keeps every query it sees
type recordQueries struct{ queries []string }

func (r *recordQueries) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (r *recordQueries) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	r.queries = append(r.queries, event.Query)
}
