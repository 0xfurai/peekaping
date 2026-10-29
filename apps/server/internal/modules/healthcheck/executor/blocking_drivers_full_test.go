//go:build !slim

package executor

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
)

var blockingCasesFull = []blockingCase{
	{"sqlserver", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"database_connection_string": "Server=%s,%d;Database=master;User Id=sa;Password=secret;Encrypt=false"}`, host, port)
	}},
	{"sqlserver", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"database_connection_string": "sqlserver://sa:secret@%s:%d?database=master&encrypt=disable"}`, host, port)
	}},
	{"mongodb", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"connectionString": "mongodb://%s:%d/admin"}`, host, port)
	}},
	{"kafka-producer", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"brokers": ["%s:%d"], "topic": "t", "message": "m"}`, host, port)
	}},
	{"kafka-producer", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"brokers": ["%s:%d"], "topic": "t", "message": "m", "ssl": true}`, host, port)
	}},
	{"grpc-keyword", listenTCP, func(host string, port int) string {
		return grpcConfig(fmt.Sprintf("%s:%d", host, port), false)
	}},
	{"grpc-keyword", listenTCP, func(host string, port int) string {
		return grpcConfig(fmt.Sprintf("dns:///%s:%d", host, port), true)
	}},
	{"docker", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://%s:%d"}`, host, port)
	}},
	{"docker", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://%s:%d", "tls_enabled": true}`, host, port)
	}},
}

func grpcConfig(url string, tls bool) string {
	return fmt.Sprintf(`{"grpcUrl": %q, "grpcProtobuf": "syntax = \"proto3\";", "grpcServiceName": "Health", "grpcMethod": "check", "grpcEnableTls": %t}`, url, tls)
}

func TestDriversFull_BlockPrivateTargets(t *testing.T) {
	testBlockPrivateTargets(t, blockingCasesFull)
}

// listenUnix listens on a Unix socket, and counts the connections. The socket is in /tmp: a path
// under the test's directory is too long for a socket on macOS.
func listenUnix(t *testing.T) (path string, accepted func() int) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pp156")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path = filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	var connections atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	return path, func() int { return int(connections.Load()) }
}

// A Unix socket reaches what the machine shares with the server, so it is blocked with the option on
func TestDriversFull_BlockUnixSockets(t *testing.T) {
	tests := []struct {
		name, monitorType string
		config            func(path string) string
	}{
		{"a gRPC unix: target", "grpc-keyword", func(path string) string { return grpcConfig("unix://"+path, false) }},
		{"a gRPC relative unix: target", "grpc-keyword", func(path string) string { return grpcConfig("unix:"+path, false) }},
		{"the Docker socket", "docker", func(path string) string {
			return fmt.Sprintf(`{"container_id": "c", "connection_type": "socket", "docker_daemon": %q}`, path)
		}},
		{"a unix:// Docker daemon over tcp", "docker", func(path string) string {
			return fmt.Sprintf(`{"container_id": "c", "connection_type": "tcp", "docker_daemon": "unix://%s"}`, path)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, accepted := listenUnix(t)
			config := tc.config(path)

			blocked := guardedExecutor(t, tc.monitorType, netguard.New(true)).Execute(checkContext(t), driverMonitor(tc.monitorType, config), nil)
			requireBlocked(t, blocked)
			assert.Contains(t, blocked.Message, "unix")
			assert.Zero(t, accepted(), "the socket saw no connection")

			off := guardedExecutor(t, tc.monitorType, netguard.New(false)).Execute(checkContext(t), driverMonitor(tc.monitorType, config), nil)
			assert.NotContains(t, off.Message, blockedMessage)
			assert.Positive(t, accepted(), "the check reached the socket: %s", off.Message)
		})
	}
}

// A gRPC target can name the DNS server to ask. The resolver would query it with no dialer of
// ours, so the guard refuses the target.
func TestGRPCExecutor_BlocksADNSServerInTheTarget(t *testing.T) {
	target := listenUDP(t)
	config := grpcConfig(fmt.Sprintf("dns://127.0.0.1:%d/service.example.com:443", target.port), false)

	blocked := guardedExecutor(t, "grpc-keyword", netguard.New(true)).Execute(context.Background(), driverMonitor("grpc-keyword", config), nil)

	requireBlocked(t, blocked)
	assert.Zero(t, target.connections())
}

// sarama learns the brokers of a cluster from the first one, and dials them itself. The guard
// refuses the one the seed broker announces at a blocked address.
func TestKafkaProducerExecutor_BlocksABrokerTheClusterAnnounces(t *testing.T) {
	seed := sarama.NewMockBroker(t, 1)
	defer seed.Close()
	var announced atomic.Int32
	announcedBroker := "127.0.0.2:" + fmt.Sprint(closedPort(t))
	if listener, err := net.Listen("tcp", "127.0.0.2:0"); err == nil {
		announcedBroker = listener.Addr().String()
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				announced.Add(1)
				_ = conn.Close()
			}
		}()
	}
	seed.SetHandlerByMap(map[string]sarama.MockResponse{
		"MetadataRequest": sarama.NewMockMetadataResponse(t).
			SetBroker(seed.Addr(), seed.BrokerID()).
			SetBroker(announcedBroker, 2).
			SetController(seed.BrokerID()).
			SetLeader("t", 0, 2),
	})
	// Only 127.0.0.2 is blocked, so that the seed broker on 127.0.0.1 is allowed
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
	config := fmt.Sprintf(`{"brokers": [%q], "topic": "t", "message": "m"}`, seed.Addr())

	result := guardedExecutor(t, "kafka-producer", guard).Execute(context.Background(), driverMonitor("kafka-producer", config), nil)

	requireBlocked(t, result)
	assert.NotEmpty(t, seed.History(), "the seed broker was asked for metadata")
	assert.Zero(t, announced.Load(), "the broker the metadata announced saw no connection")
}

func TestDriversFull_Targets(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())
	tests := []struct {
		name, monitorType, config string
		want                      []string
	}{
		{"sqlserver in the Server=... form", "sqlserver", `{"database_connection_string": "Server=sql.example.com,1433;Database=master;User Id=sa;Password=secret;Encrypt=false"}`, []string{"sql.example.com"}},
		{"sqlserver with an instance", "sqlserver", `{"database_connection_string": "Server=10.0.0.4\\SQLEXPRESS;Database=master;User Id=sa;Password=secret"}`, []string{"10.0.0.4"}},
		{"sqlserver with braces", "sqlserver", `{"database_connection_string": "Server=sql.example.com,1433;Database={d;e};User Id=sa;Password={p;q}"}`, []string{"sql.example.com"}},
		{"sqlserver in the URL form", "sqlserver", `{"database_connection_string": "sqlserver://sa:secret@[fd00::9]:1433?database=master"}`, []string{"fd00::9"}},
		{"sqlserver in the mssql:// form", "sqlserver", `{"database_connection_string": "mssql://sa:secret@192.168.1.5/master"}`, []string{"192.168.1.5"}},
		{"mongodb", "mongodb", `{"connectionString": "mongodb://u:p%40ss@db.example.com:27017/admin?authSource=admin"}`, []string{"db.example.com"}},
		{"mongodb seed list", "mongodb", `{"connectionString": "mongodb://u:p@a.example.com:27017,10.0.0.2:27018,[fd00::3]:27019/admin?replicaSet=rs0"}`, []string{"a.example.com", "10.0.0.2", "fd00::3"}},
		{"mongodb srv", "mongodb", `{"connectionString": "mongodb+srv://u:p@cluster0.example.com/admin"}`, []string{"cluster0.example.com"}},
		{"mongodb socket", "mongodb", `{"connectionString": "mongodb://%2Ftmp%2Fmongodb-27017.sock/admin"}`, nil},
		{"kafka brokers", "kafka-producer", `{"brokers": ["k1.example.com:9092", "10.0.0.8:9093", "[fd00::4]:9092"], "topic": "t", "message": "m"}`, []string{"k1.example.com", "10.0.0.8", "fd00::4"}},
		{"grpc", "grpc-keyword", grpcConfig("svc.example.com:443", true), []string{"svc.example.com"}},
		{"grpc on an IPv6 address", "grpc-keyword", grpcConfig("[fd00::6]:50051", false), []string{"fd00::6"}},
		{"grpc dns scheme", "grpc-keyword", grpcConfig("dns:///svc.example.com:443", true), []string{"svc.example.com"}},
		{"grpc dns scheme with a DNS server", "grpc-keyword", grpcConfig("dns://10.0.0.53:53/svc.example.com:443", true), []string{"svc.example.com", "10.0.0.53"}},
		{"grpc ipv4 scheme", "grpc-keyword", grpcConfig("ipv4:10.0.0.1:50051,10.0.0.2:50052", false), []string{"10.0.0.1", "10.0.0.2"}},
		{"grpc unix", "grpc-keyword", grpcConfig("unix:///var/run/app.sock", false), nil},
		{"docker over tcp", "docker", `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://docker.example.com:2376"}`, []string{"docker.example.com"}},
		{"docker over tcp on an IPv6 address", "docker", `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://[fd00::2]:2375"}`, []string{"fd00::2"}},
		{"docker over a socket", "docker", `{"container_id": "c", "connection_type": "socket", "docker_daemon": "/var/run/docker.sock"}`, nil},
		{"docker over tcp naming a unix socket", "docker", `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "unix:///var/run/docker.sock"}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := registry.GetExecutor(tc.monitorType)
			require.True(t, ok)
			lister, ok := e.(TargetLister)
			require.True(t, ok, "%s lists its targets", tc.monitorType)

			got, err := lister.Targets(tc.config)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDriversFull_RefuseBlockedLiteralTargetsOnSave(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(netguard.New(true)))
	tests := []struct {
		name, monitorType, config, blocked string
	}{
		{"sqlserver", "sqlserver", `{"database_connection_string": "Server=10.0.0.4,1433;Database=master;User Id=sa;Password=secret;Encrypt=false"}`, "10.0.0.4"},
		{"sqlserver URL", "sqlserver", `{"database_connection_string": "sqlserver://sa:secret@127.0.0.1:1433?database=master"}`, "127.0.0.1"},
		{"the second mongodb host", "mongodb", `{"connectionString": "mongodb://a.example.com:27017,192.168.1.2:27017/admin"}`, "192.168.1.2"},
		{"the second kafka broker", "kafka-producer", `{"brokers": ["k1.example.com:9092", "169.254.169.254:9092"], "topic": "t", "message": "m", "sasl_options": {"mechanism": "None"}}`, "169.254.169.254"},
		{"grpc", "grpc-keyword", grpcConfig("10.1.1.1:50051", false), "10.1.1.1"},
		{"grpc DNS server", "grpc-keyword", grpcConfig("dns://127.0.0.53:53/svc.example.com:443", false), "127.0.0.53"},
		{"docker over tcp", "docker", `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://172.17.0.1:2375"}`, "172.17.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.ValidateConfig(tc.monitorType, tc.config)

			var target *netguard.TargetError
			require.ErrorAs(t, err, &target)
			assert.Equal(t, tc.blocked, target.Host)
		})
	}

	t.Run("names, sockets and public addresses pass", func(t *testing.T) {
		assert.NoError(t, registry.ValidateConfig("docker", `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://docker.corp.example:2376"}`))
		assert.NoError(t, registry.ValidateConfig("docker", `{"container_id": "c", "connection_type": "socket", "docker_daemon": "/var/run/docker.sock"}`))
		assert.NoError(t, registry.ValidateConfig("grpc-keyword", grpcConfig("8.8.8.8:443", true)))
		assert.NoError(t, registry.ValidateConfig("kafka-producer", `{"brokers": ["localhost:9092"], "topic": "t", "message": "m", "sasl_options": {"mechanism": "None"}}`))
	})
}

// With the guard on, a Docker daemon over tcp:// is dialed itself, and not through a proxy from the
// environment, which the Docker client sets up for it: the guard would check the proxy's address
// then, and a daemon at a blocked address would be reached through the proxy. Go reads HTTP_PROXY
// once per process, so the test runs itself again with a proxy set, the way the standard library's
// own tests do.
func TestDockerExecutor_TheGuardChecksTheDaemonAndNotAProxyFromTheEnvironment(t *testing.T) {
	if os.Getenv("PEEKAPING_TEST_DOCKER_PROXY") != "" {
		guard := netguard.NewWithPrefixes(netip.MustParsePrefix("203.0.113.0/24"))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result := guardedExecutor(t, "docker", guard).Execute(ctx, &Monitor{ID: "docker-proxy", Name: "Docker", Type: "docker", Timeout: 2,
			Config: `{"container_id": "c", "connection_type": "tcp", "docker_daemon": "tcp://203.0.113.7:2375"}`}, nil)
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, blockedMessage)
		return
	}

	proxy := listenTCP(t)
	proxyURL := "http://127.0.0.1:" + strconv.Itoa(proxy.port)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDockerExecutor_TheGuardChecksTheDaemonAndNotAProxyFromTheEnvironment$", "-test.v")
	cmd.Env = append(os.Environ(), "PEEKAPING_TEST_DOCKER_PROXY=1",
		"HTTP_PROXY="+proxyURL, "http_proxy="+proxyURL, "NO_PROXY=", "no_proxy=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	// A renamed test would match nothing and exit 0.
	require.Contains(t, string(out), "--- PASS: TestDockerExecutor_TheGuardChecksTheDaemonAndNotAProxyFromTheEnvironment", string(out))
	assert.Zero(t, proxy.connections(), "the proxy saw no connection")
}
