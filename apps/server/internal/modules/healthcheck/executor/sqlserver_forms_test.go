package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSQLServerExecutor_validateSemicolonFormat(t *testing.T) {
	executor := NewSQLServerExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "complete", input: "Server=localhost,1433;Database=master;User Id=sa;Password=pw;Encrypt=true;TrustServerCertificate=false;Connection Timeout=30"},
		{name: "without port", input: "Server=localhost;Database=master;User Id=sa;Password=pw"},
		{name: "lower case keys", input: "server=localhost;database=master;user id=sa;password=pw;encrypt=yes;timeout=0"},
		{name: "boolean spellings", input: "Server=h;Database=d;User Id=u;Password=p;Encrypt=YES;TrustServerCertificate=No"},
		{name: "highest port", input: "Server=h,65535;Database=d;User Id=u;Password=p"},
		{name: "no server", input: "Database=d;User Id=u;Password=p", wantErr: "Server parameter is required"},
		{name: "empty server", input: "Server=;Database=d;User Id=u;Password=p", wantErr: "Server parameter is required"},
		{name: "no database", input: "Server=h;User Id=u;Password=p", wantErr: "Database parameter is required"},
		{name: "no user", input: "Server=h;Database=d;Password=p", wantErr: "User Id parameter is required"},
		{name: "server with two ports", input: "Server=h,1,2;Database=d;User Id=u;Password=p", wantErr: "invalid Server format. Expected: hostname or hostname,port"},
		{name: "port is not a number", input: "Server=h,abc;Database=d;User Id=u;Password=p", wantErr: "invalid port number: abc"},
		{name: "port zero", input: "Server=h,0;Database=d;User Id=u;Password=p", wantErr: "invalid port number: 0"},
		{name: "negative port", input: "Server=h,-1;Database=d;User Id=u;Password=p", wantErr: "invalid port number: -1"},
		{name: "port too high", input: "Server=h,65536;Database=d;User Id=u;Password=p", wantErr: "invalid port number: 65536"},
		{name: "invalid Encrypt", input: "Server=h;Database=d;User Id=u;Password=p;Encrypt=maybe", wantErr: "invalid Encrypt value: maybe. Expected: true or false"},
		{name: "invalid TrustServerCertificate", input: "Server=h;Database=d;User Id=u;Password=p;TrustServerCertificate=1", wantErr: "invalid TrustServerCertificate value: 1. Expected: true or false"},
		{name: "Encrypt is checked before TrustServerCertificate", input: "Server=h;Database=d;User Id=u;Password=p;TrustServerCertificate=x;Encrypt=y", wantErr: "invalid Encrypt value: y. Expected: true or false"},
		{name: "timeout is not a number", input: "Server=h;Database=d;User Id=u;Password=p;Connection Timeout=soon", wantErr: "invalid Connection Timeout value: soon. Expected: positive integer"},
		{name: "negative timeout", input: "Server=h;Database=d;User Id=u;Password=p;Connection Timeout=-5", wantErr: "invalid Connection Timeout value: -5. Expected: positive integer"},
		{name: "server is checked before the booleans", input: "Server=h,0;Database=d;User Id=u;Password=p;Encrypt=maybe", wantErr: "invalid port number: 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateSemicolonFormat(tt.input)

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestSQLServerExecutor_validateLegacyURL(t *testing.T) {
	executor := NewSQLServerExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "database in the path", input: "sqlserver://sa:pw@localhost:1433/master"},
		{name: "database in the query", input: "mssql://sa:pw@localhost?database=master"},
		{name: "database in both", input: "sqlserver://sa@localhost/master?database=other"},
		{name: "without port", input: "sqlserver://sa@localhost/master"},
		{name: "empty", input: "", wantErr: "connection string cannot be empty"},
		{name: "unparsable", input: "sqlserver://sa@localhost:abc/master", wantErr: `invalid connection string format: parse "sqlserver://sa@localhost:abc/master": invalid port ":abc" after host`},
		{name: "wrong scheme", input: "mysql://sa@localhost/master", wantErr: "invalid scheme: mysql, expected sqlserver:// or mssql://"},
		{name: "no host", input: "sqlserver:///master", wantErr: "connection string must include host"},
		{name: "port without a host name", input: "sqlserver://sa@:1433/master", wantErr: "connection string must include host"},
		{name: "no user info", input: "sqlserver://localhost/master", wantErr: "connection string must include username"},
		{name: "empty username", input: "sqlserver://:pw@localhost/master", wantErr: "connection string must include username"},
		{name: "no database", input: "sqlserver://sa@localhost", wantErr: "connection string must include database name (in path or ?database= parameter)"},
		{name: "root path and no database", input: "sqlserver://sa@localhost/", wantErr: "connection string must include database name (in path or ?database= parameter)"},
		{name: "empty database parameter", input: "sqlserver://sa@localhost/?database=", wantErr: "connection string must include database name (in path or ?database= parameter)"},
		{name: "port zero", input: "sqlserver://sa@localhost:0/master", wantErr: "invalid port: 0"},
		{name: "the host is checked before the user", input: "sqlserver:///", wantErr: "connection string must include host"},
		{name: "the user is checked before the database", input: "sqlserver://localhost", wantErr: "connection string must include username"},
		{name: "the database is checked before the port", input: "sqlserver://sa@localhost:0", wantErr: "connection string must include database name (in path or ?database= parameter)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.validateLegacyURL(tt.input)

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestSQLServerExecutor_parseConnectionString_URLForm(t *testing.T) {
	executor := NewSQLServerExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "database in the query", input: "sqlserver://sa:pw@localhost:1433?database=master", want: "sqlserver://sa:pw@localhost:1433?database=master"},
		{name: "database in the path", input: "mssql://sa:pw@localhost/master", want: "sqlserver://sa:pw@localhost?database=master"},
		{name: "the query's database wins over the path's", input: "sqlserver://sa@localhost/pathdb?database=querydb", want: "sqlserver://sa@localhost?database=querydb"},
		{name: "no database", input: "sqlserver://sa@localhost", want: "sqlserver://sa@localhost"},
		{name: "root path is no database", input: "sqlserver://sa@localhost/", want: "sqlserver://sa@localhost"},
		{name: "other parameters", input: "sqlserver://sa:pw@host?encrypt=true&TrustServerCertificate=true&timeout=30", want: "sqlserver://sa:pw@host?connection+timeout=30&encrypt=true&trustservercertificate=true"},
		{name: "parameter spelled as a synonym", input: "sqlserver://sa@host?Application+Name=peek", want: "sqlserver://sa@host?app+name=peek"},
		{name: "initial catalog is read as the database parameter, which the URL's own database always replaces (so it is dropped)", input: "sqlserver://sa@host?Initial+Catalog=x", want: "sqlserver://sa@host"},
		{name: "parameters the URL's own parts set are dropped", input: "sqlserver://sa:pw@host?server=other&uid=x&password=y", want: "sqlserver://sa:pw@host"},
		{name: "a user parameter is kept without a user", input: "sqlserver://host?user=bob", want: "sqlserver://host?user+id=bob"},
		{name: "a password parameter is kept without a password", input: "sqlserver://sa@host?password=zzz", want: "sqlserver://sa@host?password=zzz"},
		{name: "IPv6 host", input: "sqlserver://sa@[::1]", want: "sqlserver://sa@[::1]:1433"},
		{name: "escaped password", input: "sqlserver://sa:p%40ss@host", want: "sqlserver://sa:p%40ss@host"},
		{name: "unparsable", input: "sqlserver://sa@host:abc", wantErr: `invalid connection string format: parse "sqlserver://sa@host:abc": invalid port ":abc" after host`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := executor.parseConnectionString(tt.input)

			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("wrong scheme", func(t *testing.T) {
		_, err := executor.parseURLConnectionString("mysql://sa@host/db")

		assert.EqualError(t, err, "invalid scheme: mysql, expected sqlserver:// or mssql://")
	})
}
