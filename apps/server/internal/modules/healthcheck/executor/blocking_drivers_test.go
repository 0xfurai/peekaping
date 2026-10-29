package executor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
)

// A listener a check would reach, which counts what connects to it
type blockingTarget struct {
	port        int
	connections func() int
}

func listenTCP(t *testing.T) blockingTarget {
	t.Helper()
	address, accepted := silentServer(t)
	return blockingTarget{port: portOf(t, address), connections: accepted}
}

func listenHTTP(t *testing.T) blockingTarget {
	t.Helper()
	server, requests := countingServer(t, func(w http.ResponseWriter, r *http.Request) {})
	return blockingTarget{port: portOf(t, server.Listener.Addr().String()), connections: func() int { return int(requests.Load()) }}
}

// listenUDP counts the datagrams it gets
func listenUDP(t *testing.T) blockingTarget {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	var datagrams atomic.Int32
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := conn.ReadFrom(buf); err != nil {
				return
			}
			datagrams.Add(1)
		}
	}()
	return blockingTarget{port: portOf(t, conn.LocalAddr().String()), connections: func() int { return int(datagrams.Load()) }}
}

func portOf(t *testing.T, address string) int {
	t.Helper()
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	return number
}

// blockingCase is one monitor type that connects out, and the config that points it at host:port
type blockingCase struct {
	monitorType string
	listen      func(*testing.T) blockingTarget
	config      func(host string, port int) string
}

var blockingCases = []blockingCase{
	{"snmp", listenUDP, func(host string, port int) string {
		return fmt.Sprintf(`{"host": %q, "port": %d, "community": "public", "snmp_version": "v2c", "oid": "1.3.6.1.2.1.1.1.0"}`, host, port)
	}},
	{"postgres", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"database_connection_string": "postgres://u:p@%s:%d/db?sslmode=disable"}`, host, port)
	}},
	{"mysql", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"connection_string": "mysql://u:p@%s:%d/db"}`, host, port)
	}},
	{"redis", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"databaseConnectionString": "redis://%s:%d"}`, host, port)
	}},
	{"redis", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"databaseConnectionString": "rediss://%s:%d", "ignoreTls": true}`, host, port)
	}},
	{"mqtt", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"hostname": %q, "port": %d, "topic": "t", "check_type": "none"}`, host, port)
	}},
	{"mqtt", listenTCP, func(host string, port int) string {
		return fmt.Sprintf(`{"hostname": "mqtts://%s", "port": %d, "topic": "t", "check_type": "none"}`, host, port)
	}},
	{"rabbitmq", listenHTTP, func(host string, port int) string {
		return fmt.Sprintf(`{"nodes": ["http://%s:%d"], "username": "u", "password": "p"}`, host, port)
	}},
}

// checkContext bounds a check whose executor leaves the time to its caller, as the Docker one does
func checkContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func driverMonitor(monitorType, config string) *Monitor {
	return &Monitor{ID: monitorType, Type: monitorType, Name: monitorType, Timeout: 1, Config: config}
}

// Each check that connects out dials through the guard: a target at a blocked address is Down with
// the guard's message, and what listens there sees nothing. With the option off the same target
// is reached, which is what shows the target was the right one.
func TestDrivers_BlockPrivateTargets(t *testing.T) {
	testBlockPrivateTargets(t, blockingCases)
}

func testBlockPrivateTargets(t *testing.T, cases []blockingCase) {
	t.Helper()
	for _, tc := range cases {
		config := tc.config("127.0.0.1", 1)
		t.Run(tc.monitorType+" "+config, func(t *testing.T) {
			target := tc.listen(t)

			t.Run("a literal loopback address", func(t *testing.T) {
				e := guardedExecutor(t, tc.monitorType, netguard.New(true))

				result := e.Execute(checkContext(t), driverMonitor(tc.monitorType, tc.config("127.0.0.1", target.port)), nil)

				requireBlocked(t, result)
			})
			t.Run("a name that resolves to loopback", func(t *testing.T) {
				e := guardedExecutor(t, tc.monitorType, netguard.New(true))

				result := e.Execute(checkContext(t), driverMonitor(tc.monitorType, tc.config("localhost", target.port)), nil)

				requireBlocked(t, result)
			})
			assert.Zero(t, target.connections(), "the target saw no connection")

			for name, guard := range map[string]*netguard.Guard{"off": netguard.New(false), "none": nil} {
				t.Run("the guard "+name, func(t *testing.T) {
					e := guardedExecutor(t, tc.monitorType, guard)

					result := e.Execute(checkContext(t), driverMonitor(tc.monitorType, tc.config("127.0.0.1", target.port)), nil)

					assert.NotContains(t, result.Message, blockedMessage)
					assert.Positive(t, target.connections(), "the check reached the target: %s", result.Message)
				})
			}
		})
	}
}

// A check that connects to an address the guard allows is not in the guard's way
func TestDrivers_ReachAnAllowedTarget(t *testing.T) {
	// Only 127.0.0.2 is blocked, so the target on 127.0.0.1 is allowed
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
	target := listenHTTP(t)
	e := guardedExecutor(t, "rabbitmq", guard)

	result := e.Execute(context.Background(), driverMonitor("rabbitmq", blockingCases[len(blockingCases)-1].config("127.0.0.1", target.port)), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, 1, target.connections())
}

// A management API that redirects to a blocked address is refused on the redirect, also when the
// guard is turned on while the check runs, as a probe's is at a hello
func TestRabbitMQExecutor_BlocksARedirectToAPrivateTarget(t *testing.T) {
	for name, offAtFirst := range map[string]bool{"on from the start": false, "turned on while the check runs": true} {
		t.Run(name, func(t *testing.T) {
			// Only 127.0.0.2 is blocked, so that the test can serve on 127.0.0.1 and redirect to it
			guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
			guard.Set(!offAtFirst)
			var blockedRequests atomic.Int32
			target := "127.0.0.2:" + strconv.Itoa(closedPort(t))
			if listener, err := net.Listen("tcp", "127.0.0.2:0"); err == nil {
				blockedServer := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					blockedRequests.Add(1)
				})}}
				blockedServer.Start()
				t.Cleanup(blockedServer.Close)
				target = listener.Addr().String()
			}
			first, firstRequests := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
				guard.Set(true)
				http.Redirect(w, r, "http://"+target+"/", http.StatusFound)
			})
			config := fmt.Sprintf(`{"nodes": [%q], "username": "u", "password": "p"}`, first.URL)

			result := guardedExecutor(t, "rabbitmq", guard).Execute(context.Background(), driverMonitor("rabbitmq", config), nil)

			requireBlocked(t, result)
			assert.Equal(t, int32(1), firstRequests.Load(), "the first node was reached")
			assert.Zero(t, blockedRequests.Load(), "the address the redirect leads to saw no request")
		})
	}
}

// pgdriver is told to connect over TCP whatever the string says, which keeps its Unix socket out
// of reach of the guard
func TestNewPostgresConnector_NeverUsesAUnixSocket(t *testing.T) {
	for _, connectionString := range []string{
		"postgres://u:p@localhost/db?host=/var/run/postgresql",
		"postgres://u:p@localhost/db?host=%2Fvar%2Frun%2Fpostgresql",
	} {
		connector, err := newPostgresConnector(connectionString)
		require.NoError(t, err)
		assert.Equal(t, "tcp", connector.Config().Network, connectionString)
		assert.Equal(t, "localhost:5432", connector.Config().Addr, connectionString)
	}
}

func TestDrivers_Targets(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar())
	tests := []struct {
		name, monitorType, config string
		want                      []string
	}{
		{"snmp", "snmp", `{"host": "10.0.0.7", "community": "public", "snmp_version": "v2c", "oid": "1.3"}`, []string{"10.0.0.7"}},
		{"postgres", "postgres", `{"database_connection_string": "postgres://u:p%40ss@db.example.com:5432/app?sslmode=require"}`, []string{"db.example.com"}},
		{"postgres on an IPv6 address", "postgres", `{"database_connection_string": "postgresql://u:p@[fd00::5]:5432/app"}`, []string{"fd00::5"}},
		{"mysql", "mysql", `{"connection_string": "mysql://u:p@mysql.example.com/app"}`, []string{"mysql.example.com"}},
		{"redis", "redis", `{"databaseConnectionString": "redis://user:pass@cache.example.com:6379/1"}`, []string{"cache.example.com"}},
		{"redis with TLS", "redis", `{"databaseConnectionString": "rediss://[::1]:6380"}`, []string{"::1"}},
		{"mqtt", "mqtt", `{"hostname": "broker.example.com", "port": 1883, "topic": "t", "check_type": "none"}`, []string{"broker.example.com"}},
		{"mqtt with a scheme", "mqtt", `{"hostname": "mqtts://169.254.169.254", "port": 8883, "topic": "t", "check_type": "none"}`, []string{"169.254.169.254"}},
		{"mqtt on an IPv6 address", "mqtt", `{"hostname": "[fd00::1]", "port": 1883, "topic": "t", "check_type": "none"}`, []string{"fd00::1"}},
		{"rabbitmq nodes", "rabbitmq", `{"nodes": ["https://node1.example.com:15672", "http://10.0.0.2:15672/", "http://[fd00::3]:15672"], "username": "u", "password": "p"}`,
			[]string{"node1.example.com", "10.0.0.2", "fd00::3"}},
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

func TestDrivers_RefuseBlockedLiteralTargetsOnSave(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(netguard.New(true)))
	tests := []struct {
		name, monitorType, config, blocked string
	}{
		{"snmp", "snmp", `{"host": "192.168.0.1", "community": "public", "snmp_version": "v2c", "oid": "1.3"}`, "192.168.0.1"},
		{"postgres", "postgres", `{"database_connection_string": "postgres://u:p@10.1.2.3:5432/app"}`, "10.1.2.3"},
		{"mysql", "mysql", `{"connection_string": "mysql://u:p@127.0.0.1:3306/app"}`, "127.0.0.1"},
		{"redis", "redis", `{"databaseConnectionString": "redis://169.254.169.254:6379"}`, "169.254.169.254"},
		{"mqtt", "mqtt", `{"hostname": "100.64.0.9", "port": 1883, "topic": "t", "check_type": "none"}`, "100.64.0.9"},
		{"the second rabbitmq node", "rabbitmq", `{"nodes": ["https://ok.example.com:15672", "http://172.16.0.4:15672"], "username": "u", "password": "p"}`, "172.16.0.4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.ValidateConfig(tc.monitorType, tc.config)

			var target *netguard.TargetError
			require.ErrorAs(t, err, &target)
			assert.Equal(t, tc.blocked, target.Host)
		})
	}

	t.Run("names pass", func(t *testing.T) {
		assert.NoError(t, registry.ValidateConfig("postgres", `{"database_connection_string": "postgres://u:p@db.corp.example:5432/app"}`))
		assert.NoError(t, registry.ValidateConfig("redis", `{"databaseConnectionString": "redis://localhost:6379"}`))
		assert.NoError(t, registry.ValidateConfig("snmp", `{"host": "router.corp.example", "community": "public", "snmp_version": "v2c", "oid": "1.3"}`))
	})
}
