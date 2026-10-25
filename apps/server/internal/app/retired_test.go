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
		})
	}
}

func TestWorkerHasNoWorkNotice(t *testing.T) {
	t.Run("SQLite and PostgreSQL: no work left", func(t *testing.T) {
		for _, dbType := range []string{"sqlite", "postgres"} {
			message := WorkerHasNoWorkNotice(dbType)

			assert.Contains(t, message, "The worker has no work left on SQLite or PostgreSQL storage (DB_TYPE="+dbType+")", dbType)
			assert.Contains(t, message, UpgradeToProbesURL)
		}
	})
	t.Run("a worker told no storage says the same without naming one", func(t *testing.T) {
		message := WorkerHasNoWorkNotice("")

		assert.Contains(t, message, "The worker has no work left: peekaping-probe instances run the checks")
		assert.NotContains(t, message, "DB_TYPE")
		assert.NotContains(t, message, "MongoDB")
		assert.Contains(t, message, UpgradeToProbesURL)
	})
}
