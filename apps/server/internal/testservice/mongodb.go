//go:build integration

package testservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	mongoURLEnv   = "PEE_TEST_MONGODB_URL"
	mongoRSURLEnv = "PEE_TEST_MONGODB_RS_URL"

	// The root user of the standalone server, also in
	// docker-compose.integration.yml. The server has to require a login, so
	// tests can check a wrong password and a wrong authSource.
	mongoUsername = "root"
	mongoPassword = "integration-test-password"

	mongoImage = "mongo:7"

	// The name of the replica set, also in docker-compose.integration.yml.
	mongoReplicaSetName = "rs0"
)

// MongoServer is a running standalone MongoDB that requires a login. The
// root user is created in the admin database.
type MongoServer struct {
	Addr     string // host:port
	Username string
	Password string
}

// URL returns a connection string for the server. Pass a database for the path
// and the options (authSource, replicaSet, ...) the monitor would carry.
func (s MongoServer) URL(username, password, database string, query url.Values) string {
	u := url.URL{
		Scheme:   "mongodb",
		User:     url.UserPassword(username, password),
		Host:     s.Addr,
		Path:     "/" + database,
		RawQuery: query.Encode(),
	}
	return u.String()
}

// MongoReplicaSetServer is a running single-node replica set, without
// authentication: a replica set with authentication needs a key file, and the
// tests that sign in run against the standalone server. The node is
// registered under Addr, which the host can reach, so a client that discovers
// the members connects to that address.
type MongoReplicaSetServer struct {
	Addr string // host:port
	Name string // the replica set's name
}

// URL returns a connection string for the replica set. Pass the name the
// client asks for (Name, unless a test wants a wrong one).
func (s MongoReplicaSetServer) URL(database, replicaSet string) string {
	u := url.URL{
		Scheme:   "mongodb",
		Host:     s.Addr,
		Path:     "/" + database,
		RawQuery: url.Values{"replicaSet": {replicaSet}}.Encode(),
	}
	return u.String()
}

// mongoDialCheck bounds one attempt to reach the server while waiting for it.
const mongoDialCheck = 2 * time.Second

var (
	sharedMongo   lazy[MongoServer]
	sharedMongoRS lazy[MongoReplicaSetServer]
)

// MongoDB returns the standalone MongoDB for this test: the server in
// PEE_TEST_MONGODB_URL (mongodb://<user>:<password>@<host>:<port>), or a
// container shared by the tests of the binary and removed when it exits.
func MongoDB(t testing.TB) MongoServer {
	t.Helper()
	return resolve(t, "MongoDB", []string{mongoURLEnv}, "mongodb://<user>:<password>@host:port",
		func(values []string) (MongoServer, error) { return parseMongoURL(values[0]) }, &sharedMongo, startMongo)
}

// MongoReplicaSet returns the single-node replica set named rs0 for this
// test: the one in PEE_TEST_MONGODB_RS_URL (mongodb://<host>:<port>, the
// address the node is registered under), or a container shared by the tests of
// the binary and removed when it exits.
func MongoReplicaSet(t testing.TB) MongoReplicaSetServer {
	t.Helper()
	return resolve(t, "MongoDB replica set", []string{mongoRSURLEnv}, "mongodb://host:port",
		func(values []string) (MongoReplicaSetServer, error) { return parseMongoRSURL(values[0]) }, &sharedMongoRS, startMongoReplicaSet)
}

func parseMongoURL(raw string) (MongoServer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return MongoServer{}, err
	}
	password, _ := u.User.Password()
	if u.Scheme != "mongodb" || u.Host == "" || u.Port() == "" || u.User.Username() == "" || password == "" {
		return MongoServer{}, errors.New("need scheme mongodb, a user, a password, a host and a port")
	}
	return MongoServer{Addr: u.Host, Username: u.User.Username(), Password: password}, nil
}

func parseMongoRSURL(raw string) (MongoReplicaSetServer, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return MongoReplicaSetServer{}, err
	}
	if u.Scheme != "mongodb" || u.Host == "" || u.Port() == "" {
		return MongoReplicaSetServer{}, errors.New("need scheme mongodb, a host and a port")
	}
	if u.User != nil {
		return MongoReplicaSetServer{}, errors.New("no user: the replica set runs without authentication")
	}
	return MongoReplicaSetServer{Addr: u.Host, Name: mongoReplicaSetName}, nil
}

func startMongo(ctx context.Context) (MongoServer, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        mongoImage,
			Env:          map[string]string{"MONGO_INITDB_ROOT_USERNAME": mongoUsername, "MONGO_INITDB_ROOT_PASSWORD": mongoPassword},
			ExposedPorts: []string{"27017/tcp"},
			WaitingFor:   wait.ForLog("Waiting for connections"),
		},
		Started: true,
	})
	if err != nil {
		return MongoServer{}, err
	}
	host, err := c.Host(ctx)
	if err != nil {
		return MongoServer{}, err
	}
	port, err := c.MappedPort(ctx, "27017/tcp")
	if err != nil {
		return MongoServer{}, err
	}
	srv := MongoServer{Addr: net.JoinHostPort(host, port.Port()), Username: mongoUsername, Password: mongoPassword}
	// The image starts a temporary server to create the user, then restarts:
	// the port can answer before the user exists.
	if err := waitForMongo(ctx, srv.URL(srv.Username, srv.Password, "admin", url.Values{"authSource": {"admin"}})); err != nil {
		return MongoServer{}, err
	}
	return srv, nil
}

func startMongoReplicaSet(ctx context.Context) (MongoReplicaSetServer, error) {
	// The node listens on the port it is published on and registers itself
	// under it, so the host finds the same address in the replica set's
	// configuration as the one it connected to.
	port, err := freePort()
	if err != nil {
		return MongoReplicaSetServer{}, err
	}
	p := strconv.Itoa(port)
	addr := net.JoinHostPort("127.0.0.1", p)

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        mongoImage,
			Cmd:          []string{"mongod", "--replSet", mongoReplicaSetName, "--port", p, "--bind_ip_all"},
			ExposedPorts: []string{p + ":" + p + "/tcp"},
			WaitingFor:   wait.ForLog("Waiting for connections"),
		},
		Started: true,
	})
	if err != nil {
		return MongoReplicaSetServer{}, err
	}

	initiate := fmt.Sprintf(`rs.initiate({_id: %q, members: [{_id: 0, host: %q}]})`, mongoReplicaSetName, addr)
	code, out, err := c.Exec(ctx, []string{"mongosh", "--port", p, "--quiet", "--eval", initiate})
	if err != nil {
		return MongoReplicaSetServer{}, err
	}
	if code != 0 {
		b, _ := io.ReadAll(out)
		return MongoReplicaSetServer{}, fmt.Errorf("rs.initiate exited with %d: %s", code, b)
	}

	srv := MongoReplicaSetServer{Addr: addr, Name: mongoReplicaSetName}
	if err := waitForMongo(ctx, srv.URL("admin", srv.Name)); err != nil {
		return MongoReplicaSetServer{}, err
	}
	return srv, nil
}

// waitForMongo returns once a client connected with the connection string
// pings the server, which for a replica set means that it elected a primary.
func waitForMongo(ctx context.Context, uri string) error {
	var lastErr error
	for ctx.Err() == nil {
		lastErr = pingMongo(ctx, uri)
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
	if lastErr == nil {
		// The start used up the time before the first ping.
		lastErr = ctx.Err()
	}
	return fmt.Errorf("no answer from MongoDB: %w", lastErr)
}

func pingMongo(ctx context.Context, uri string) error {
	ctx, cancel := context.WithTimeout(ctx, mongoDialCheck)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(mongoDialCheck))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background()) //nolint:errcheck
	return client.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err()
}

// freePort asks the OS for a port nothing listens on. Another process can take
// it before the container binds it, which fails the start instead of the test.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
