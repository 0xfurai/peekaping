//go:build !slim

package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// The full build has an executor for every monitor type. The server is always built this way:
// only the probe has a slim build (see types_slim_test.go).
func TestExecutorRegistry_Types(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())

	assert.Equal(t, []string{
		"dns", "docker", "grpc-keyword", "http", "http-json-query", "http-keyword", "kafka-producer", "mongodb", "mqtt",
		"mysql", "ping", "postgres", "push", "rabbitmq", "redis", "smtp", "snmp", "sqlserver", "tcp",
	}, registry.Types())
}
