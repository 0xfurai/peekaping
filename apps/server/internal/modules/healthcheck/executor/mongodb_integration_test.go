//go:build integration && !slim

package executor

import (
	"context"
	"net/url"
	"testing"
	"time"

	"peekaping/internal/modules/shared"
	"peekaping/internal/testservice"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMongoDBExecutor_Integration(t *testing.T) {
	srv := testservice.MongoDB(t)
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())

	run := func(t *testing.T, cfg MongoDBConfig) *Result {
		t.Helper()
		return exec.Execute(context.Background(), mongoMonitor(t, 10, cfg), nil)
	}
	// The root user lives in the admin database, and so does the monitor's
	// default authSource.
	good := srv.URL(srv.Username, srv.Password, "admin", nil)

	t.Run("is up for the default ping", func(t *testing.T) {
		result := run(t, MongoDBConfig{ConnectionString: good})

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Equal(t, "Command executed successfully", result.Message)
	})

	t.Run("is up when a JSON path into a nested document matches", func(t *testing.T) {
		result := run(t, MongoDBConfig{
			ConnectionString: good,
			Command:          `{"serverStatus": 1}`,
			JsonPath:         "$.storageEngine.name",
			ExpectedValue:    "wiredTiger",
		})

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Equal(t, "Command executed successfully and expected value was found", result.Message)
	})

	t.Run("is up when a JSON path into a nested document has no expected value", func(t *testing.T) {
		result := run(t, MongoDBConfig{
			ConnectionString: good,
			Command:          `{"serverStatus": 1}`,
			JsonPath:         "connections.current",
		})

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		assert.Equal(t, "Command executed successfully and the jsonata expression produces a result", result.Message)
	})

	t.Run("is down for a wrong password", func(t *testing.T) {
		result := run(t, MongoDBConfig{ConnectionString: srv.URL(srv.Username, "not-"+srv.Password, "admin", nil)})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MongoDB command failed: failed to execute MongoDB command:")
		assert.Contains(t, result.Message, "(AuthenticationFailed) Authentication failed.")
	})

	t.Run("is down when the user is not in the authSource", func(t *testing.T) {
		result := run(t, MongoDBConfig{ConnectionString: srv.URL(srv.Username, srv.Password, "admin", url.Values{"authSource": {"test"}})})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MongoDB command failed: failed to execute MongoDB command:")
		assert.Contains(t, result.Message, "(AuthenticationFailed) Authentication failed.")
	})

	t.Run("is down for a command the server does not know", func(t *testing.T) {
		result := run(t, MongoDBConfig{ConnectionString: good, Command: `{"noSuchCommand": 1}`})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "MongoDB command failed: failed to execute MongoDB command: (CommandNotFound) no such command: 'noSuchCommand'", result.Message)
	})

	t.Run("is down when the value differs from the expected one", func(t *testing.T) {
		result := run(t, MongoDBConfig{
			ConnectionString: good,
			Command:          `{"serverStatus": 1}`,
			JsonPath:         "storageEngine.name",
			ExpectedValue:    "inMemory",
		})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "Query executed, but value is not equal to expected value, value was: [wiredTiger]", result.Message)
	})

	t.Run("is down when the JSON path finds nothing", func(t *testing.T) {
		result := run(t, MongoDBConfig{
			ConnectionString: good,
			Command:          `{"serverStatus": 1}`,
			JsonPath:         "storageEngine.nothing",
		})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "JSON path evaluation failed: field 'nothing' not found", result.Message)
	})
}

func TestMongoDBExecutor_ReplicaSet_Integration(t *testing.T) {
	rs := testservice.MongoReplicaSet(t)
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())

	t.Run("is up for the replica set's name", func(t *testing.T) {
		result := exec.Execute(context.Background(), mongoMonitor(t, 10, MongoDBConfig{ConnectionString: rs.URL("admin", rs.Name)}), nil)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	})

	t.Run("is down for another name, within the monitor's timeout", func(t *testing.T) {
		start := time.Now()
		result := exec.Execute(context.Background(), mongoMonitor(t, 3, MongoDBConfig{ConnectionString: rs.URL("admin", "not-"+rs.Name)}), nil)
		elapsed := time.Since(start)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "MongoDB command failed: failed to execute MongoDB command: server selection error: ")
		assert.Contains(t, result.Message, "current topology: { Type: ReplicaSetNoPrimary, Servers: [] }")
		assert.Less(t, elapsed, 4*time.Second, "the check must end within the monitor's timeout plus 1s")
	})
}
