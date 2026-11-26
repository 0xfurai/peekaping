//go:build slim

package probeclient_test

import (
	"testing"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
)

// A slim probe (-tags slim) tells the server it runs the types it was built with, which leave out
// the Docker, Kafka, SQL Server, MongoDB and gRPC ones. Run with: go test -tags slim ./internal/probeclient/
func TestSupportedTypes_OfTheRealRegistry_Slim(t *testing.T) {
	supported := probeclient.NewExecutors(executor.NewExecutorRegistry(nopLogger())).SupportedTypes()

	assert.Equal(t, []string{
		"dns", "http", "http-json-query", "http-keyword", "mqtt", "mysql", "ping", "postgres",
		"rabbitmq", "redis", "smtp", "snmp", "tcp",
	}, supported, "every executor it has but push")
	for _, left := range []string{"docker", "kafka-producer", "sqlserver", "mongodb", "grpc-keyword"} {
		assert.NotContains(t, supported, left)
	}
	// An empty list in hello means every type to the server, so a slim probe must never send one
	assert.NotEmpty(t, supported)
}
