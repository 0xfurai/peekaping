//go:build integration

package testservice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func askForSQLServer(t *testing.T) (SQLServerServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv SQLServerServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = SQLServer(rec)
	}()
	<-done
	return srv, rec
}

func TestSQLServer_EnvVarWins(t *testing.T) {
	dockerMustNotBeAsked(t)
	t.Setenv(sqlServerURLEnv, "sqlserver://sa:Pe%3Be%3DK%7Bing%7D9z@127.0.0.1:11433")

	srv, rec := askForSQLServer(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, SQLServerServer{Host: "127.0.0.1", Port: "11433", Username: "sa", Password: "Pe;e=K{ing}9z"}, srv)
	assert.Equal(t, "127.0.0.1:11433", srv.Addr())
	assert.Equal(t, sqlServerPassword, srv.Password, "the compose file's password and the container's must be the same")
}

func TestSQLServer_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{"127.0.0.1:1433", "sqlserver://127.0.0.1:1433", "sqlserver://sa@127.0.0.1:1433", "sqlserver://sa:pw@127.0.0.1", "mssql://sa:pw@127.0.0.1:1433"} {
		t.Setenv(sqlServerURLEnv, raw)

		_, rec := askForSQLServer(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, sqlServerURLEnv, raw)
	}
}

func TestSQLServer_NoDocker(t *testing.T) {
	t.Setenv(sqlServerURLEnv, "")
	t.Setenv("CI", "")
	stubDocker(t, func() error { return assert.AnError })

	_, rec := askForSQLServer(t)

	require.True(t, rec.skipped, "failed=%v msg=%q", rec.failed, rec.msg)
	assert.Contains(t, rec.msg, sqlServerURLEnv)
}
