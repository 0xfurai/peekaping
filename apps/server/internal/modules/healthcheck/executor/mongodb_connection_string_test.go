//go:build !slim

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
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
		{"a database followed by a fragment", "mongodb://h1/db#frag"},
		{"a Unix socket", "mongodb://%2Ftmp%2Fmongodb-27017.sock/db"},
		{"an empty port", "mongodb://h:/db"},
		{"an empty port on an IPv6 address", "mongodb://[::1]:/db"},
		{"a trailing comma in the seed list", "mongodb://h1,/db"},
		{"a leading comma in the seed list", "mongodb://,h1/db"},
		{"an empty host between two", "mongodb://h1:27017,,h2:27017/db"},
		{"a space after the comma, which the driver reports", "mongodb://h1:27017, h2:27017/db"},
		{"a + in a host, which the driver takes", "mongodb://h1+x/db"},
		{"a user with no password", "mongodb://u@h1/db"},
		{"a user with an empty password", "mongodb://u:@h1/db"},
		{"an SRV name that does not resolve", "mongodb+srv://u:p@no-such-cluster.invalid/db"},
		{"an SRV name with options", "mongodb+srv://cluster0.example.invalid/db?retryWrites=true"},
		{"an SRV name with a port, which the driver reports", "mongodb+srv://h1:27017/db"},
	}
	for _, tc := range valid {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			assert.NoError(t, validateMongoString(t, tc.connectionString))
		})
		if !strings.HasPrefix(tc.connectionString, "mongodb+srv:") {
			// A saved monitor stays editable: whatever the driver takes, and the check
			// accepted before, still saves.
			t.Run("the driver takes "+tc.name, func(t *testing.T) {
				_, err := connstring.ParseAndValidate(tc.connectionString)
				assert.NoError(t, err)
			})
		}
	}
}

// refusedMongoStrings are the malformed connection strings, each with what the error says.
var refusedMongoStrings = []struct{ name, connectionString, want string }{
	{"no string", "", "connection string cannot be empty"},
	{"no scheme", "h1:27017,h2:27017/db", "connection string must start with mongodb:// or mongodb+srv://"},
	{"another scheme", "mysql://h1:3306/db", "connection string must use mongodb:// or mongodb+srv://"},
	{"no host", "mongodb:///db", "connection string must include host"},
	{"no host after the credentials", "mongodb://u:p@:27017/db", "connection string must include host"},
	{"only empty hosts", "mongodb://,/db", "connection string must include host"},
	{"a port that is not a number", "mongodb://h1:27017,h2:abc/db", "the port of host 2 must be a number from 1 to 65535"},
	{"a port that is out of range", "mongodb://h1:70000/db", "the port of host 1 must be a number from 1 to 65535"},
	{"port 0", "mongodb://h1:0/db", "the port of host 1 must be a number from 1 to 65535"},
	{"a signed port", "mongodb://h1:+5/db", "the port of host 1 must be a number from 1 to 65535"},
	{"an unclosed bracket", "mongodb://h0,[fd00::1:27017/db", "host 2 has a [ without a closing ]"},
	{"a host escape that is broken", "mongodb://h1,h%zz:27017/db", "host 2 has a bad %-escape"},
	{"an empty user name", "mongodb://@h1/db", "the user name must not be empty"},
	{"a password with no user name", "mongodb://:p@h1/db", "the user name must not be empty"},
	{"an empty user name and password", "mongodb://:@h1/db", "the user name must not be empty"},
	{"a password with no user name, SECRET", "mongodb://:SECRET@h1/db", "the user name must not be empty"},
	{"a host escape that is broken, SECRET", "mongodb://h%SECRET/db", "host 1 has a bad %-escape"},
	{"no database", "mongodb://h1:27017,h2:27017","connection string must include database name"},
	{"an empty database", "mongodb://h1:27017,h2:27017/", "connection string must include database name"},
	{"options without a database", "mongodb://h1:27017/?replicaSet=rs0", "connection string must include database name"},
	{"a database escape that is broken", "mongodb://h1/my%zzdb", "the database name has a bad %-escape"},
	{"credentials escape that is broken", "mongodb://u:p%zz@h1/db", "the user name or password has a bad %-escape"},
	// A "/" or ":" a password doesn't escape ends the host part early, so what follows is read as a
	// host and a port. The error must still say nothing of it.
	{"a password with a slash", "mongodb://user:SECRET/rest@h/db", "the port of host 1 must be a number from 1 to 65535"},
	{"a password with a colon", "mongodb://u:SECRET:ss/x@h/db", "must be a number from 1 to 65535"},
	{"a password with a bad escape", "mongodb://u:SECRET%ss@h/db", "the user name or password has a bad %-escape"},
}

func TestMongoDBExecutor_Validate_RefusesMalformedConnectionStrings(t *testing.T) {
	for _, tc := range refusedMongoStrings {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			err := validateMongoString(t, tc.connectionString)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid connection string: ")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestMongoDBExecutor_Validate_RefusesWhatTheDriverRefuses(t *testing.T) {
	for _, connectionString := range []string{"mongodb://h%zz/db", "mongodb://@h1/db", "mongodb://:p@h1/db", "mongodb://:@h1/db"} {
		t.Run(connectionString, func(t *testing.T) {
			_, err := connstring.ParseAndValidate(connectionString)
			require.Error(t, err, "the driver refuses it")

			assert.Error(t, validateMongoString(t, connectionString))
		})
	}
}

// A refusal reaches the API response, so it must not quote the string: a password that the string
// holds in a place the parser reads as a host or a port would come out with it.
func TestMongoDBExecutor_Validate_ErrorsNeverQuoteTheString(t *testing.T) {
	const marker = "SECRETMARKER"
	for _, tc := range refusedMongoStrings {
		variants := map[string]string{"as written": strings.ReplaceAll(tc.connectionString, "SECRET", marker)}
		if strings.Contains(tc.connectionString, "://") && !strings.Contains(tc.connectionString, "@") {
			variants["with the marker as the password"] = strings.Replace(tc.connectionString, "://", "://u:"+marker+"@", 1)
		}
		for how, connectionString := range variants {
			t.Run(tc.name+", "+how, func(t *testing.T) {
				err := validateMongoString(t, connectionString)

				if err == nil {
					return // the marker made it valid; nothing to quote
				}
				assert.NotContains(t, err.Error(), marker)
				assert.NotContains(t, err.Error(), "SECRET")
				assert.NotContains(t, err.Error(), "h2", "no host name")
				assert.NotContains(t, err.Error(), "27017", "no port")
			})
		}
	}
}

func TestMongoDBExecutor_Targets_SeedList(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())
	tests := []struct {
		name, connectionString string
		want                   []string
	}{
		{"every host of a seed list", "mongodb://u:p@h1.example.com:27017,10.0.0.2,[fd00::3]:27019/db?replicaSet=rs0", []string{"h1.example.com", "10.0.0.2", "fd00::3"}},
		{"an encoded @ in the password", "mongodb://u:p%40ss@h1.example.com,10.0.0.2/db", []string{"h1.example.com", "10.0.0.2"}},
		{"a ? and an @ in the options", "mongodb://u:p@h1.example.com,10.0.0.2:27018/db?appName=a?b@evil.example.com", []string{"h1.example.com", "10.0.0.2"}},
		{"a trailing comma", "mongodb://h1.example.com,/db", []string{"h1.example.com"}},
		{"a leading comma", "mongodb://,h1.example.com/db", []string{"h1.example.com"}},
		{"an empty host with a port", "mongodb://:27017,h1.example.com/db", []string{"h1.example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := exec.Targets(`{"connectionString": "` + tc.connectionString + `"}`)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMongoDBExecutor_enhanceConnectionString(t *testing.T) {
	exec := NewMongoDBExecutor(zap.NewNop().Sugar())
	tests := []struct{ name, in, want string }{
		{"adds the admin authSource for a user", "mongodb://u:p@h1:27017/db", "mongodb://u:p@h1:27017/db?authSource=admin"},
		{"adds it to a seed list's options", "mongodb://u:p@h1:27017,h2:27017/db?replicaSet=rs0", "mongodb://u:p@h1:27017,h2:27017/db?replicaSet=rs0&authSource=admin"},
		{"keeps the authSource the string has", "mongodb://u:p@h1:27017,h2:27017/db?authSource=test", "mongodb://u:p@h1:27017,h2:27017/db?authSource=test"},
		{"keeps an authSource written in another case", "mongodb://u:p@h1/db?authsource=test", "mongodb://u:p@h1/db?authsource=test"},
		{"keeps an authSource before a semicolon option", "mongodb://u:p@h1/db?authSource=users;retryWrites=true", "mongodb://u:p@h1/db?authSource=users;retryWrites=true"},
		{"keeps an authSource after a semicolon option", "mongodb://u:p@h1/db?retryWrites=true;authsource=users", "mongodb://u:p@h1/db?retryWrites=true;authsource=users"},
		{"keeps an authSource after both separators", "mongodb://u:p@h1/db?a=1&b=2;authSource=users&c=3", "mongodb://u:p@h1/db?a=1&b=2;authSource=users&c=3"},
		{"does not count an authSource with no value", "mongodb://u:p@h1/db?authSource=", "mongodb://u:p@h1/db?authSource=&authSource=admin"},
		{"does not take a longer name for authSource", "mongodb://u:p@h1/db?authSourceX=y", "mongodb://u:p@h1/db?authSourceX=y&authSource=admin"},
		{"adds it to options that are empty", "mongodb://u:p@h1/db?", "mongodb://u:p@h1/db?authSource=admin"},
		{"adds it before a fragment", "mongodb://u:p@h1/db#frag", "mongodb://u:p@h1/db?authSource=admin#frag"},
		{"adds it to options before a fragment", "mongodb://u:p@h1/db?replicaSet=rs0#frag", "mongodb://u:p@h1/db?replicaSet=rs0&authSource=admin#frag"},
		{"keeps an authSource before a fragment", "mongodb://u:p@h1/db?AUTHSOURCE=test#frag", "mongodb://u:p@h1/db?AUTHSOURCE=test#frag"},
		{"leaves a string without a user", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0"},
		{"leaves a string with a password only", "mongodb://:p@h1/db", "mongodb://:p@h1/db"},
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
		{"mongodb://h1/db#frag", "db"},
		{"mongodb://h1/db?x=1#frag", "db"},
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
