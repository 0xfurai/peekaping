package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefuseOnSQLStorage_RefusesEverySQLBackendAndPointsAtTheUpgradeSteps(t *testing.T) {
	for _, dbType := range []string{"sqlite", "postgres", "postgresql", "mysql", "SQLite", "POSTGRES"} {
		t.Run(dbType, func(t *testing.T) {
			err := RefuseOnSQLStorage("producer", dbType, "the probes run the checks")

			require.Error(t, err)
			assert.ErrorContains(t, err, "the producer no longer runs on SQLite or PostgreSQL storage")
			assert.ErrorContains(t, err, "DB_TYPE="+dbType)
			assert.ErrorContains(t, err, "the probes run the checks")
			assert.ErrorContains(t, err, "https://docs.peekaping.com/self-hosting/upgrading-to-probes")
			assert.ErrorContains(t, err, "published with this release")
			assert.ErrorContains(t, err, "still runs on MongoDB storage")
		})
	}
}

func TestRefuseOnSQLStorage_LetsMongoDBRunAsToday(t *testing.T) {
	for _, dbType := range []string{"mongo", "mongodb", "MongoDB"} {
		assert.NoError(t, RefuseOnSQLStorage("producer", dbType, "the probes run the checks"), dbType)
	}
}

func TestWorkerHasNoWorkNotice(t *testing.T) {
	t.Run("SQLite and PostgreSQL: no work left", func(t *testing.T) {
		for _, dbType := range []string{"sqlite", "postgres"} {
			message, ok := WorkerHasNoWorkNotice(dbType)

			require.True(t, ok, dbType)
			assert.Contains(t, message, "The worker has no work left on SQLite or PostgreSQL storage (DB_TYPE="+dbType+")")
			assert.Contains(t, message, UpgradeToProbesURL)
		}
	})
	t.Run("a worker told no storage can only say what is true of both", func(t *testing.T) {
		message, ok := WorkerHasNoWorkNotice("")

		require.True(t, ok)
		assert.Contains(t, message, "work only on MongoDB storage")
		assert.Contains(t, message, "no work left")
		assert.Contains(t, message, UpgradeToProbesURL)
	})
	t.Run("MongoDB: nothing to say", func(t *testing.T) {
		for _, dbType := range []string{"mongo", "mongodb"} {
			_, ok := WorkerHasNoWorkNotice(dbType)
			assert.False(t, ok, dbType)
		}
	})
}
