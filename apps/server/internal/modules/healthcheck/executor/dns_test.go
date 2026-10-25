package executor

import (
	"context"
	"fmt"
	"net"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDNSExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		expectedHost  string
		expectedPort  int
	}{
		{
			name: "valid config",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: false,
			expectedHost:  "example.com",
			expectedPort:  53,
		},
		{
			name: "invalid json",
			config: `{
				"host": "example.com",
				invalid
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedHost != "" {
					cfg := result.(*DNSConfig)
					assert.Equal(t, tt.expectedHost, cfg.Host)
					assert.Equal(t, tt.expectedPort, cfg.Port)
				}
			}
		})
	}
}

func TestDNSExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewDNSExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid config with all fields",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: false,
		},
		{
			name: "missing required host",
			config: `{
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "missing required resolver_server",
			config: `{
				"host": "example.com",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid resolver_server (not IP)",
			config: `{
				"host": "example.com",
				"resolver_server": "not-an-ip",
				"port": 53,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (too high)",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 70000,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid port (zero)",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 0,
				"resolve_type": "A"
			}`,
			expectedError: true,
		},
		{
			name: "invalid resolve_type",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 53,
				"resolve_type": "INVALID"
			}`,
			expectedError: true,
		},
		{
			name: "valid with AAAA type",
			config: `{
				"host": "example.com",
				"resolver_server": "1.1.1.1",
				"port": 53,
				"resolve_type": "AAAA"
			}`,
			expectedError: false,
		},
		{
			name: "valid with MX type",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.4.4",
				"port": 53,
				"resolve_type": "MX"
			}`,
			expectedError: false,
		},
		{
			name: "valid with alternative port",
			config: `{
				"host": "example.com",
				"resolver_server": "8.8.8.8",
				"port": 5353,
				"resolve_type": "A"
			}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// testZone is what startDNSServer answers from. The names are under .test
// (RFC 2606) and the addresses in documentation ranges, so /etc/hosts never
// answers for them first.
var testZone = []string{
	"peekaping.test. 300 IN A 192.0.2.10",
	"peekaping.test. 300 IN AAAA 2001:db8::10",
	"peekaping.test. 300 IN MX 10 mail.peekaping.test.",
	"peekaping.test. 300 IN NS ns1.peekaping.test.",
	`peekaping.test. 300 IN TXT "v=spf1 -all"`,
	`peekaping.test. 300 IN CAA 0 issue "letsencrypt.org"`,
	"peekaping.test. 300 IN SOA ns1.peekaping.test. hostmaster.peekaping.test. 2026101701 7200 3600 1209600 300",
	"mail.peekaping.test. 300 IN A 192.0.2.25",
	"www.peekaping.test. 300 IN CNAME peekaping.test.",
	"_http._tcp.peekaping.test. 300 IN SRV 10 5 8080 peekaping.test.",
	"10.2.0.192.in-addr.arpa. 300 IN PTR peekaping.test.",
}

// startDNSServer serves testZone over UDP on an IPv4 loopback port and returns the port.
func startDNSServer(t *testing.T) int {
	t.Helper()
	return startDNSServerOn(t, "127.0.0.1:0")
}

// startDNSServerOn serves testZone over UDP on addr and returns the port.
func startDNSServerOn(t *testing.T, addr string) int {
	t.Helper()
	var zone []dns.RR
	for _, record := range testZone {
		rr, err := dns.NewRR(record)
		require.NoError(t, err)
		zone = append(zone, rr)
	}
	lookup := func(name string, qtype uint16) []dns.RR {
		var answer []dns.RR
		for _, rr := range zone {
			h := rr.Header()
			if strings.EqualFold(h.Name, name) && (h.Rrtype == qtype || h.Rrtype == dns.TypeCNAME) {
				answer = append(answer, rr)
			}
		}
		return answer
	}

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Authoritative = true
		resp.RecursionAvailable = true
		q := req.Question[0]
		resp.Answer = lookup(q.Name, q.Qtype)
		// Follow a CNAME to its target's records, like a recursive resolver.
		if len(resp.Answer) == 1 && q.Qtype != dns.TypeCNAME {
			if cname, ok := resp.Answer[0].(*dns.CNAME); ok {
				resp.Answer = append(resp.Answer, lookup(cname.Target, q.Qtype)...)
			}
		}
		if len(resp.Answer) == 0 && len(lookup(q.Name, dns.TypeANY)) == 0 {
			resp.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(resp)
	})

	pc, err := net.ListenPacket("udp", addr)
	require.NoError(t, err)
	started := make(chan struct{})
	srv := &dns.Server{PacketConn: pc, Handler: handler, NotifyStartedFunc: func() { close(started) }}
	go srv.ActivateAndServe()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func dnsMonitor(port int, host, resolveType string, timeout int) *Monitor {
	return &Monitor{
		ID:      "dns",
		Type:    "dns",
		Name:    host,
		Timeout: timeout,
		Config:  fmt.Sprintf(`{"host": %q, "resolver_server": "127.0.0.1", "port": %d, "resolve_type": %q}`, host, port, resolveType),
	}
}

func TestDNSExecutor_Execute_RecordTypes(t *testing.T) {
	executor := NewDNSExecutor(zap.NewNop().Sugar())
	port := startDNSServer(t)

	tests := []struct {
		resolveType string
		host        string
		wantMessage string
	}{
		{"A", "peekaping.test", "A records: 192.0.2.10"},
		{"AAAA", "peekaping.test", "AAAA records: 2001:db8::10"},
		{"CNAME", "www.peekaping.test", "CNAME: peekaping.test."},
		{"MX", "peekaping.test", "MX records: mail.peekaping.test. (priority: 10)"},
		{"NS", "peekaping.test", "NS records: ns1.peekaping.test."},
		{"TXT", "peekaping.test", "TXT records: v=spf1 -all"},
		{"PTR", "192.0.2.10", "PTR records: peekaping.test."},
		{"SRV", "_http._tcp.peekaping.test", "SRV records (cname: _http._tcp.peekaping.test.): peekaping.test.:8080 (priority: 10, weight: 5)"},
		{"CAA", "peekaping.test", `CAA records: 0 issue "letsencrypt.org"`},
		{"SOA", "peekaping.test", "SOA: Primary NS: ns1.peekaping.test., Admin: hostmaster.peekaping.test., Serial: 2026101701, Refresh: 7200, Retry: 3600, Expire: 1209600, Min TTL: 300"},
	}

	for _, tt := range tests {
		t.Run(tt.resolveType, func(t *testing.T) {
			result := executor.Execute(context.Background(), dnsMonitor(port, tt.host, tt.resolveType, 2), nil)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}

func TestDNSExecutor_Execute_IPv6Resolver(t *testing.T) {
	requireIPv6Loopback(t)
	port := startDNSServerOn(t, "[::1]:0")
	monitor := dnsMonitor(port, "peekaping.test", "A", 2)
	monitor.Config = strings.Replace(monitor.Config, "127.0.0.1", "::1", 1)

	result := NewDNSExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, "A records: 192.0.2.10", result.Message)
}

func TestDNSExecutor_Execute_NoRecords(t *testing.T) {
	executor := NewDNSExecutor(zap.NewNop().Sugar())
	port := startDNSServer(t)

	tests := []struct {
		name        string
		host        string
		resolveType string
		wantMessage string
	}{
		{name: "non-existent domain", host: "missing.peekaping.test", resolveType: "A", wantMessage: "DNS lookup failed"},
		{name: "no record of the type", host: "mail.peekaping.test", resolveType: "CAA", wantMessage: "No CAA records found for mail.peekaping.test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.Execute(context.Background(), dnsMonitor(port, tt.host, tt.resolveType, 2), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, tt.wantMessage)
		})
	}
}

func TestDNSExecutor_Execute_Timeout(t *testing.T) {
	executor := NewDNSExecutor(zap.NewNop().Sugar())
	// A UDP socket that is never read from, so no query gets an answer.
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer silent.Close()
	port := silent.LocalAddr().(*net.UDPAddr).Port

	// A goes through net.Resolver, SOA through miekg/dns.
	for _, resolveType := range []string{"A", "SOA"} {
		t.Run(resolveType, func(t *testing.T) {
			start := time.Now()
			result := executor.Execute(context.Background(), dnsMonitor(port, "peekaping.test", resolveType, 1), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, "DNS lookup failed")
			assert.Less(t, time.Since(start), 1500*time.Millisecond, "should give up after the 1s monitor timeout")
		})
	}
}

func TestDNSExecutor_Execute_InvalidConfig(t *testing.T) {
	monitor := &Monitor{ID: "dns", Type: "dns", Name: "dns", Timeout: 2, Config: `{invalid json}`}

	result := NewDNSExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "failed to parse config")
}

func TestDNSExecutor_Execute_IgnoresProxy(t *testing.T) {
	port := startDNSServer(t)
	proxy := &Proxy{ID: "proxy1", Host: "127.0.0.1", Port: closedPort(t), Protocol: "http"}

	result := NewDNSExecutor(zap.NewNop().Sugar()).Execute(context.Background(), dnsMonitor(port, "peekaping.test", "A", 2), proxy)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
}

func TestNewDNSExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewDNSExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.logger)
}
