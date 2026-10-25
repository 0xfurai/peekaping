package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest/observer"
)

// serving waits until the worker has started
func serving(t *testing.T, logs *observer.ObservedLogs) {
	t.Helper()
	require.Eventually(t, func() bool { return logs.FilterMessage("Serving: the worker is running").Len() == 1 }, wait, 10*time.Millisecond)
}

// AC6: on SQLite and PostgreSQL the probes run the checks, so the worker has no work left. It logs
// that, and keeps running: it refuses nothing, since a task left in its queue by an older producer
// is still run.
func TestRun_TheWorkerLogsThatItHasNoWorkLeftOnSQLStorage(t *testing.T) {
	for _, dbType := range []string{"sqlite", "postgres"} {
		t.Run(dbType, func(t *testing.T) {
			cfg, _ := loadConfig(t, map[string]string{"DB_TYPE": dbType})
			logger, logs := observedLogger()

			in := startWorker(t, cfg, logger)
			serving(t, logs)

			notices := logs.FilterMessageSnippet("has no work left").All()
			require.Len(t, notices, 1)
			assert.Contains(t, notices[0].Message, "DB_TYPE="+dbType)
			assert.Contains(t, notices[0].Message, "https://docs.peekaping.com/self-hosting/upgrading-to-probes")
			assert.NoError(t, in.stop(t), "and it runs until it is stopped")
		})
	}
}

// The worker has no database, so it reads no DB_TYPE unless the install passes one. Without it, the
// worker says what is true on any storage.
func TestRun_TheWorkerWithNoStorageSaysItHasWorkOnMongoDBOnly(t *testing.T) {
	cfg, _ := loadConfig(t, nil)
	logger, logs := observedLogger()

	in := startWorker(t, cfg, logger)
	serving(t, logs)

	notices := logs.FilterMessageSnippet("work only on MongoDB storage").All()
	require.Len(t, notices, 1)
	assert.Contains(t, notices[0].Message, "no work left")
	assert.NoError(t, in.stop(t))
}

// MongoDB has no probes (ADR 0001): its worker runs the checks as it always did, and says nothing
// about its job
func TestRun_TheWorkerOnMongoDBStorageSaysNothingAboutItsJob(t *testing.T) {
	cfg, _ := loadConfig(t, map[string]string{"DB_TYPE": "mongo"})
	logger, logs := observedLogger()

	in := startWorker(t, cfg, logger)
	serving(t, logs)

	assert.Zero(t, logs.FilterMessageSnippet("no work left").Len())
	assert.Zero(t, logs.FilterMessageSnippet("work only on MongoDB").Len())
	assert.NoError(t, in.stop(t))
}
