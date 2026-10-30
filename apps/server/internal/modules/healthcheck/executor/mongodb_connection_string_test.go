//go:build !slim

package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"peekaping/internal/modules/shared"
)

func validateMongoString(t *testing.T, connectionString string) error {
	t.Helper()
	return NewMongoDBExecutor(zap.NewNop().Sugar()).Validate(`{"connectionString": "` + connectionString + `"}`)
}

func TestMongoDBExecutor_Validate_ConnectionString(t *testing.T) {
	valid := []struct{ name, connectionString string }{
		{"a seed list", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0"},
		{"a seed list with credentials", "mongodb://u:p%40ss@h1:27017,h2:27018,h3/db?replicaSet=rs0&authSource=admin"},
		{"a seed list of IPv6 addresses", "mongodb://[fd00::1]:27017,[fd00::2]/db"},
		{"a single host", "mongodb://h1/db"},
		{"a database name that is escaped", "mongodb://h1/my%20db"},
		{"a Unix socket", "mongodb://%2Ftmp%2Fmongodb-27017.sock/db"},
		{"an SRV name that does not resolve", "mongodb+srv://u:p@no-such-cluster.invalid/db"},
		{"an SRV name with options", "mongodb+srv://cluster0.example.invalid/db?retryWrites=true"},
	}
	for _, tc := range valid {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			assert.NoError(t, validateMongoString(t, tc.connectionString))
		})
	}

	invalid := []struct{ name, connectionString, want string }{
		{"no string", "", "connection string cannot be empty"},
		{"no scheme", "h1:27017,h2:27017/db", "connection string must start with mongodb:// or mongodb+srv://"},
		{"another scheme", "mysql://h1:3306/db", "connection string must use one of [mongodb mongodb+srv] schemes, got: mysql"},
		{"no host", "mongodb:///db", "connection string must include host"},
		{"no host after the credentials", "mongodb://u:p@:27017/db", "connection string must include host"},
		{"an empty host in a seed list", "mongodb://h1:27017,,h2:27017/db", "connection string must include host"},
		{"a trailing comma", "mongodb://h1:27017,/db", "connection string must include host"},
		{"a port that is not a number", "mongodb://h1:27017,h2:abc/db", `invalid port "abc" for host h2`},
		{"a port that is out of range", "mongodb://h1:70000/db", `invalid port "70000" for host h1`},
		{"port 0", "mongodb://h1:0/db", `invalid port "0" for host h1`},
		{"an empty port", "mongodb://h1:/db", `invalid port "" for host h1`},
		{"an IPv6 address without brackets", "mongodb://fd00::1/db", "invalid host fd00::1: write an IPv6 address in brackets"},
		{"an unclosed bracket", "mongodb://[fd00::1:27017/db", "invalid host [fd00::1:27017: missing ]"},
		{"a space in a host", "mongodb://h 1:27017/db", "invalid host h 1"},
		{"a space after the comma of a seed list", "mongodb://h1:27017, h2:27017/db", "invalid host  h2:27017"},
		{"no database", "mongodb://h1:27017,h2:27017", "connection string must include database name"},
		{"an empty database", "mongodb://h1:27017,h2:27017/", "connection string must include database name"},
		{"options without a database", "mongodb://h1:27017/?replicaSet=rs0", "connection string must include database name"},
		{"a database escape that is broken", "mongodb://h1/my%zzdb", "invalid database name"},
		{"credentials escape that is broken", "mongodb://u:p%zz@h1/db", "invalid credentials"},
		{"an SRV name with several hosts", "mongodb+srv://h1,h2/db", "a mongodb+srv connection string takes one host"},
		{"an SRV name with a port", "mongodb+srv://h1:27017/db", "a mongodb+srv connection string takes no port"},
	}
	for _, tc := range invalid {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			err := validateMongoString(t, tc.connectionString)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid connection string: ")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestMongoDBExecutor_Targets_SeedList(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())

	got, err := exec.Targets(`{"connectionString": "mongodb://u:p@h1.example.com:27017,10.0.0.2,[fd00::3]:27019/db?replicaSet=rs0"}`)

	require.NoError(t, err)
	assert.Equal(t, []string{"h1.example.com", "10.0.0.2", "fd00::3"}, got)
}

func TestMongoDBExecutor_enhanceConnectionString(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())
	tests := []struct{ name, in, want string }{
		{"adds the admin authSource for a user", "mongodb://u:p@h1:27017/db", "mongodb://u:p@h1:27017/db?authSource=admin"},
		{"adds it to a seed list's options", "mongodb://u:p@h1:27017,h2:27017/db?replicaSet=rs0", "mongodb://u:p@h1:27017,h2:27017/db?replicaSet=rs0&authSource=admin"},
		{"keeps the authSource the string has", "mongodb://u:p@h1:27017,h2:27017/db?authSource=test", "mongodb://u:p@h1:27017,h2:27017/db?authSource=test"},
		{"leaves a string without a user", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0"},
		{"keeps an escaped password as written", "mongodb://u:p%40ss%2F@h1/db", "mongodb://u:p%40ss%2F@h1/db?authSource=admin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := exec.enhanceConnectionString(tc.in)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMongoDBExecutor_extractDatabaseName(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())
	tests := []struct{ in, want string }{
		{"mongodb://h1:27017/db", "db"},
		{"mongodb://h1:27017,h2:27017/db?replicaSet=rs0", "db"},
		{"mongodb+srv://u:p@cluster.example.com/my%20db?retryWrites=true", "my db"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := exec.extractDatabaseName(tc.in)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMongoDBExecutor_Execute_SeedListReachesTheDriver(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())
	connectionString := "mongodb://u:p@" + refusedAddr(t) + ",[::1]:2/admin?replicaSet=rs0"

	result := exec.Execute(context.Background(), mongoMonitor(t, 2, MongoDBConfig{ConnectionString: connectionString}), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "server selection error", "the driver tried the seeds")
}
