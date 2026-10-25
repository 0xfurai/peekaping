//go:build !slim

package probeclient_test

import (
	"testing"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
)

// The full build of the probe runs every monitor type but push
func TestSupportedTypes_OfTheRealRegistry(t *testing.T) {
	executors := probeclient.NewExecutors(executor.NewExecutorRegistry(nopLogger()))

	assert.Equal(t, []string{
		"dns", "docker", "grpc-keyword", "http", "http-json-query", "http-keyword", "kafka-producer", "mongodb", "mqtt",
		"mysql", "ping", "postgres", "rabbitmq", "redis", "smtp", "snmp", "sqlserver", "tcp",
	}, executors.SupportedTypes(), "every executor it has but push")
}
