//go:build integration

package testservice

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func askForMongo(t *testing.T) (MongoServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv MongoServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = MongoDB(rec)
	}()
	<-done
	return srv, rec
}

func askForMongoReplicaSet(t *testing.T) (MongoReplicaSetServer, *recorder) {
	t.Helper()
	rec := &recorder{TB: t}
	var srv MongoReplicaSetServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv = MongoReplicaSet(rec)
	}()
	<-done
	return srv, rec
}

func TestMongoDB_EnvVarWins(t *testing.T) {
	dockerMustNotBeAsked(t)
	t.Setenv(mongoURLEnv, "mongodb://root:s3cret@127.0.0.1:27018")

	srv, rec := askForMongo(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, MongoServer{Addr: "127.0.0.1:27018", Username: "root", Password: "s3cret"}, srv)
}

func TestMongoDB_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{"127.0.0.1:27017", "mongodb://127.0.0.1:27017", "mongodb://root@127.0.0.1:27017", "mongodb://root:pw@127.0.0.1", "http://root:pw@127.0.0.1:27017"} {
		t.Setenv(mongoURLEnv, raw)

		_, rec := askForMongo(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, mongoURLEnv, raw)
	}
}

func TestMongoDB_URLEscapesTheCredentials(t *testing.T) {
	srv := MongoServer{Addr: "127.0.0.1:27018"}

	got := srv.URL("root", "p@ss/word", "test", url.Values{"authSource": {"admin"}})

	assert.Equal(t, "mongodb://root:p%40ss%2Fword@127.0.0.1:27018/test?authSource=admin", got)
}

func TestMongoReplicaSet_EnvVarWins(t *testing.T) {
	dockerMustNotBeAsked(t)
	t.Setenv(mongoRSURLEnv, "mongodb://127.0.0.1:27019")

	srv, rec := askForMongoReplicaSet(t)

	assert.False(t, rec.skipped || rec.failed, rec.msg)
	assert.Equal(t, MongoReplicaSetServer{Addr: "127.0.0.1:27019", Name: "rs0"}, srv)
	assert.Equal(t, "mongodb://127.0.0.1:27019/test?replicaSet=rs0", srv.URL("test", srv.Name))
}

func TestMongoReplicaSet_MalformedEnvVarFails(t *testing.T) {
	dockerMustNotBeAsked(t)
	for _, raw := range []string{"127.0.0.1:27019", "mongodb://127.0.0.1", "mongodb://root:pw@127.0.0.1:27019", "http://127.0.0.1:27019"} {
		t.Setenv(mongoRSURLEnv, raw)

		_, rec := askForMongoReplicaSet(t)

		assert.True(t, rec.failed, raw)
		assert.Contains(t, rec.msg, mongoRSURLEnv, raw)
	}
}

func TestMongoDB_NoDocker(t *testing.T) {
	t.Setenv(mongoURLEnv, "")
	t.Setenv("CI", "true")
	stubDocker(t, func() error { return assert.AnError })

	_, rec := askForMongo(t)

	require.True(t, rec.failed, "skipped=%v msg=%q", rec.skipped, rec.msg)
	assert.Contains(t, rec.msg, mongoURLEnv)
}
