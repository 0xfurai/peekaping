//go:build slim

package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// A slim build (-tags slim) leaves out the executors that pull in the heaviest drivers: Docker,
// Kafka, SQL Server, MongoDB and gRPC. Run with: go test -tags slim ./internal/modules/healthcheck/executor/
func TestExecutorRegistry_Types_Slim(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())

	assert.Equal(t, []string{
		"dns", "http", "http-json-query", "http-keyword", "mqtt", "mysql", "ping", "postgres", "push",
		"rabbitmq", "redis", "smtp", "snmp", "tcp",
	}, registry.Types())
}

func TestExecutorRegistry_Slim_LeavesOutTheHeavyTypes(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())

	for _, monitorType := range []string{"docker", "kafka-producer", "sqlserver", "mongodb", "grpc-keyword"} {
		_, found := registry.GetExecutor(monitorType)
		assert.False(t, found, "%s should not be in a slim build", monitorType)
	}
}
