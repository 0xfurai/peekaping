package executor

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// BER encodings of what an SNMP agent answers a GET with
var (
	snmpOctetString   = func(s string) []byte { return berTLV(0x04, []byte(s)) }
	snmpInteger       = func(n byte) []byte { return berTLV(0x02, []byte{n}) }
	snmpGauge32       = func(n byte) []byte { return berTLV(0x42, []byte{n}) }
	snmpIPAddress     = berTLV(0x40, []byte{192, 0, 2, 7})
	snmpNoSuchObject  = berTLV(0x80, nil)
	snmpNoSuchInstace = berTLV(0x81, nil)
)

// berTLV encodes a BER element: tag, length and content.
func berTLV(tag byte, content []byte) []byte {
	out := []byte{tag}
	switch n := len(content); {
	case n < 0x80:
		out = append(out, byte(n))
	default:
		out = append(out, 0x81, byte(n))
	}
	return append(out, content...)
}

// berReadTLV reads the element at the start of b, and returns its tag and content and the rest.
func berReadTLV(b []byte) (tag byte, content, rest []byte) {
	tag, length, b := b[0], int(b[1]), b[2:]
	if length == 0x81 {
		length, b = int(b[0]), b[1:]
	}
	return tag, b[:length], b[length:]
}

// snmpRequest is what the fake agent reads of a GET request.
type snmpRequest struct {
	version   byte
	community string
	oid       string
}

// startFakeSNMPAgent answers SNMP GET requests on a loopback UDP port. reply returns the BER
// encoded value for the OID, or nil to leave the request unanswered, or an empty non-nil slice to
// answer with no variable bindings. It returns the port and the requests received.
func startFakeSNMPAgent(t *testing.T, reply func(oid string) []byte) (uint16, func() []snmpRequest) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	var mu sync.Mutex
	var requests []snmpRequest
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			// message: version, community, PDU(request id, error status, error index, bindings)
			_, message, _ := berReadTLV(buf[:n])
			_, version, rest := berReadTLV(message)
			_, community, rest := berReadTLV(rest)
			_, pdu, _ := berReadTLV(rest)
			idTag, idContent, afterID := berReadTLV(pdu)
			requestID := berTLV(idTag, idContent)
			// the first binding of the PDU: its OID
			_, _, afterStatus := berReadTLV(afterID)
			_, _, afterIndex := berReadTLV(afterStatus)
			_, bindings, _ := berReadTLV(afterIndex)
			_, binding, _ := berReadTLV(bindings)
			_, oidBytes, _ := berReadTLV(binding)
			oid := berOIDString(oidBytes)

			mu.Lock()
			requests = append(requests, snmpRequest{version: version[0], community: string(community), oid: oid})
			mu.Unlock()

			value := reply(oid)
			if value == nil {
				continue
			}
			var variableBindings []byte
			if len(value) > 0 {
				variableBindings = berTLV(0x30, append(berTLV(0x06, oidBytes), value...))
			}
			response := berTLV(0xa2, concat(
				requestID, berTLV(0x02, []byte{0}), berTLV(0x02, []byte{0}), berTLV(0x30, variableBindings),
			))
			_, _ = conn.WriteTo(berTLV(0x30, concat(berTLV(0x02, version), berTLV(0x04, community), response)), peer)
		}
	}()

	return uint16(conn.LocalAddr().(*net.UDPAddr).Port), func() []snmpRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]snmpRequest(nil), requests...)
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// berOIDString decodes the content of an object identifier whose arcs are all below 128.
func berOIDString(b []byte) string {
	arcs := []string{string('0' + b[0]/40), string('0' + b[0]%40)}
	for _, arc := range b[1:] {
		arcs = append(arcs, strconv.Itoa(int(arc)))
	}
	return strings.Join(arcs, ".")
}

func snmpMonitor(t *testing.T, timeout int, cfg SnmpConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "snmp-test", Name: "SNMP test", Type: "snmp", Timeout: timeout, Config: string(raw)}
}

func TestSnmpExecutor_Execute_Outcomes(t *testing.T) {
	const oid = "1.3.6.1.2.1.1.1.0"
	values := map[string][]byte{
		"1.3.6.1.2.1.1.1.0": snmpOctetString("Linux router"),
		"1.3.6.1.2.1.1.2.0": snmpInteger(42),
		"1.3.6.1.2.1.1.3.0": snmpGauge32(7),
		"1.3.6.1.2.1.1.4.0": snmpIPAddress,
		"1.3.6.1.2.1.1.5.0": snmpNoSuchObject,
		"1.3.6.1.2.1.1.6.0": snmpNoSuchInstace,
		"1.3.6.1.2.1.1.7.0": {},
	}
	port, _ := startFakeSNMPAgent(t, func(oid string) []byte { return values[oid] })

	tests := []struct {
		name        string
		cfg         SnmpConfig
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{name: "text value", cfg: SnmpConfig{Oid: oid}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: Linux router"},
		{name: "integer value", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: 42"},
		{name: "gauge value", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.3.0"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: 7"},
		{name: "address value", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.4.0"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: 192.0.2.7"},
		{name: "no such object", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.5.0"}, wantStatus: shared.MonitorStatusDown, wantMessage: "The SNMP query returned that no object exists for OID 1.3.6.1.2.1.1.5.0"},
		{name: "no such instance", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.6.0"}, wantStatus: shared.MonitorStatusDown, wantMessage: "The SNMP query returned that no instance exists for OID 1.3.6.1.2.1.1.6.0"},
		{name: "no variable bindings", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.7.0"}, wantStatus: shared.MonitorStatusDown, wantMessage: "No varbinds returned from SNMP session (OID: 1.3.6.1.2.1.1.7.0)"},
		{name: "condition holds", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0", JsonPath: "$", JsonPathOperator: "eq", ExpectedValue: "42"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP condition passes (comparing 42 eq 42)"},
		{name: "numeric condition holds", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0", JsonPath: "$", JsonPathOperator: "gt", ExpectedValue: "9"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP condition passes (comparing 42 gt 9)"},
		{name: "numeric condition fails", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0", JsonPath: "$", JsonPathOperator: "lt", ExpectedValue: "9"}, wantStatus: shared.MonitorStatusDown, wantMessage: "SNMP condition does not pass (comparing 42 lt 9)"},
		{name: "text condition fails", cfg: SnmpConfig{Oid: oid, JsonPath: "$", JsonPathOperator: "eq", ExpectedValue: "Windows"}, wantStatus: shared.MonitorStatusDown, wantMessage: "SNMP condition does not pass (comparing Linux router eq Windows)"},
		{name: "condition without an operator is equality", cfg: SnmpConfig{Oid: oid, JsonPath: "$", ExpectedValue: "Linux router"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP condition passes (comparing Linux router  Linux router)"},
		{name: "path without an expected value is not checked", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0", JsonPath: "$", JsonPathOperator: "eq"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: 42"},
		{name: "expected value without a path is not checked", cfg: SnmpConfig{Oid: "1.3.6.1.2.1.1.2.0", JsonPathOperator: "eq", ExpectedValue: "0"}, wantStatus: shared.MonitorStatusUp, wantMessage: "SNMP query successful, received value: 42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Host, tt.cfg.Port, tt.cfg.Community, tt.cfg.SnmpVersion = "127.0.0.1", port, "public", "v2c"

			result := NewSnmpExecutor(zap.NewNop().Sugar()).Execute(context.Background(), snmpMonitor(t, 2, tt.cfg), nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestSnmpExecutor_Execute_SendsTheConfiguredRequest(t *testing.T) {
	tests := []struct {
		version     string
		wantVersion byte
	}{
		{version: "v1", wantVersion: 0},
		{version: "v2c", wantVersion: 1},
		{version: "bogus", wantVersion: 1},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			port, requests := startFakeSNMPAgent(t, func(string) []byte { return snmpInteger(1) })

			result := NewSnmpExecutor(zap.NewNop().Sugar()).Execute(context.Background(), snmpMonitor(t, 2, SnmpConfig{
				Host: "127.0.0.1", Port: port, Community: "s3cret", SnmpVersion: tt.version, Oid: "1.3.6.1.2.1.1.1.0",
			}), nil)

			require.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			got := requests()
			require.Len(t, got, 1)
			assert.Equal(t, snmpRequest{version: tt.wantVersion, community: "s3cret", oid: "1.3.6.1.2.1.1.1.0"}, got[0])
		})
	}
}

func TestSnmpExecutor_Execute_Failures(t *testing.T) {
	t.Run("agent never answers", func(t *testing.T) {
		port, requests := startFakeSNMPAgent(t, func(string) []byte { return nil })

		start := time.Now()
		result := NewSnmpExecutor(zap.NewNop().Sugar()).Execute(context.Background(), snmpMonitor(t, 1, SnmpConfig{
			Host: "127.0.0.1", Port: port, Community: "public", SnmpVersion: "v2c", Oid: "1.3.6.1.2.1.1.1.0",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.True(t, strings.HasPrefix(result.Message, "SNMP GET failed: "), result.Message)
		assert.Len(t, requests(), 1, "a monitor with no retries asks once")
		assert.Less(t, time.Since(start), 3*time.Second)
	})

	t.Run("target does not resolve", func(t *testing.T) {
		result := NewSnmpExecutor(zap.NewNop().Sugar()).Execute(context.Background(), snmpMonitor(t, 1, SnmpConfig{
			Host: "no-such-host.invalid", Port: 161, Community: "public", SnmpVersion: "v2c", Oid: "1.3.6.1.2.1.1.1.0",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.True(t, strings.HasPrefix(result.Message, "SNMP connection failed: "), result.Message)
	})
}
