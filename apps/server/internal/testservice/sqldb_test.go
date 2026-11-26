//go:build integration

package testservice

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func askForSQLDatabase(t *testing.T, service func(testing.TB) SQLDatabase) (SQLDatabase, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv SQLDatabase
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = service(rec)
	}()
	<-done
	return srv, rec
}

var sqlDatabases = []struct {
	name   string
	env    string
	scheme string
	ask    func(testing.TB) SQLDatabase
}{
	{"Postgres", postgresURLEnv, "postgres", Postgres},
	{"MySQL", mysqlURLEnv, "mysql", MySQL},
}

func TestSQLDatabases_EnvVarWins(t *testing.T) {
	for _, svc := range sqlDatabases {
		t.Run(svc.name, func(t *testing.T) {
			dockerMustNotBeAsked(t)
			// The password is percent-encoded in the URL: p@ss:w/rd%1?x
			t.Setenv(svc.env, svc.scheme+"://peekaping:p%40ss%3Aw%2Frd%251%3Fx@127.0.0.1:15432/peekaping")

			srv, rec := askForSQLDatabase(t, svc.ask)

			assert.False(t, rec.skipped || rec.failed, rec.msg)
			assert.Equal(t, SQLDatabase{Scheme: svc.scheme, Addr: "127.0.0.1:15432", User: "peekaping", Password: "p@ss:w/rd%1?x", Database: "peekaping"}, srv)
		})
	}
}

func TestSQLDatabases_MalformedEnvVarFails(t *testing.T) {
	for _, svc := range sqlDatabases {
		t.Run(svc.name, func(t *testing.T) {
			dockerMustNotBeAsked(t)
			other := "mysql"
			if svc.scheme == "mysql" {
				other = "postgres"
			}
			for _, raw := range []string{
				"127.0.0.1:5432",
				svc.scheme + "://peekaping:pw@127.0.0.1/peekaping",
				svc.scheme + "://peekaping@127.0.0.1:5432/peekaping",
				svc.scheme + "://:pw@127.0.0.1:5432/peekaping",
				svc.scheme + "://peekaping:pw@127.0.0.1:5432",
				other + "://peekaping:pw@127.0.0.1:5432/peekaping",
			} {
				t.Setenv(svc.env, raw)

				_, rec := askForSQLDatabase(t, svc.ask)

				assert.True(t, rec.failed, raw)
				assert.Contains(t, rec.msg, svc.env, raw)
			}
		})
	}
}

func TestSQLDatabases_NoDocker(t *testing.T) {
	for _, svc := range sqlDatabases {
		t.Run(svc.name, func(t *testing.T) {
			t.Setenv(svc.env, "")
			stubDocker(t, func() error { return errors.New("cannot connect to the Docker daemon") })

			t.Run("skips outside CI", func(t *testing.T) {
				t.Setenv("CI", "")

				_, rec := askForSQLDatabase(t, svc.ask)

				require.True(t, rec.skipped, "failed=%v msg=%q", rec.failed, rec.msg)
				assert.Contains(t, rec.msg, "Docker")
				assert.Contains(t, rec.msg, svc.env)
			})

			t.Run("fails in CI", func(t *testing.T) {
				t.Setenv("CI", "true")

				_, rec := askForSQLDatabase(t, svc.ask)

				require.True(t, rec.failed, "skipped=%v msg=%q", rec.skipped, rec.msg)
				assert.Contains(t, rec.msg, "Docker")
				assert.Contains(t, rec.msg, svc.env)
			})
		})
	}
}

func TestSQLDatabase_URLEscapesTheAccount(t *testing.T) {
	srv := SQLDatabase{Scheme: "postgres", Addr: "127.0.0.1:15432", User: "peekaping", Password: "p@ss:w/rd%1?x", Database: "peekaping"}

	assert.Equal(t, "postgres://peekaping:p%40ss%3Aw%2Frd%251%3Fx@127.0.0.1:15432/peekaping?sslmode=require", srv.URL(srv.Password, "sslmode=require"))
	assert.Equal(t, "postgres://peekaping:wrong@127.0.0.1:15432/peekaping", srv.URL("wrong", ""))
}
