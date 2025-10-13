package executor

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"peekaping/src/modules/shared"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Ping configuration constants (inspired by Uptime Kuma's approach)
const (
	PING_COUNT_MIN                   = 1
	PING_COUNT_MAX                   = 100
	PING_COUNT_DEFAULT               = 1
	PING_PER_REQUEST_TIMEOUT_MIN     = 1
	PING_PER_REQUEST_TIMEOUT_MAX     = 60
	PING_PER_REQUEST_TIMEOUT_DEFAULT = 2
)

type PingConfig struct {
	Host              string `json:"host" validate:"required" example:"example.com"`
	PacketSize        int    `json:"packet_size" validate:"min=0,max=65507" example:"32"`
	Count             int    `json:"count" validate:"min=1,max=100" example:"1"`
	PerRequestTimeout int    `json:"per_request_timeout" validate:"min=1,max=60" example:"2"`
}

type PingExecutor struct {
	logger *zap.SugaredLogger
}

func NewPingExecutor(logger *zap.SugaredLogger) *PingExecutor {
	return &PingExecutor{
		logger: logger,
	}
}

func (s *PingExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[PingConfig](configJSON)
}

func (s *PingExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	pingCfg := cfg.(*PingConfig)

	// Validate basic fields
	if err := GenericValidator(pingCfg); err != nil {
		return err
	}

	// Validate count range
	if pingCfg.Count < PING_COUNT_MIN || pingCfg.Count > PING_COUNT_MAX {
		return fmt.Errorf("count must be between %d and %d (default: %d)",
			PING_COUNT_MIN, PING_COUNT_MAX, PING_COUNT_DEFAULT)
	}

	// Validate per-request timeout range
	if pingCfg.PerRequestTimeout < PING_PER_REQUEST_TIMEOUT_MIN || pingCfg.PerRequestTimeout > PING_PER_REQUEST_TIMEOUT_MAX {
		return fmt.Errorf("per_request_timeout must be between %d and %d seconds (default: %d)",
			PING_PER_REQUEST_TIMEOUT_MIN, PING_PER_REQUEST_TIMEOUT_MAX, PING_PER_REQUEST_TIMEOUT_DEFAULT)
	}

	return nil
}

func (p *PingExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := p.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*PingConfig)

	// Set default packet size if not provided
	if cfg.PacketSize == 0 {
		cfg.PacketSize = 32
	}

	// Set default count if not provided
	if cfg.Count == 0 {
		cfg.Count = PING_COUNT_DEFAULT
	}

	// Set default per-request timeout if not provided
	if cfg.PerRequestTimeout == 0 {
		cfg.PerRequestTimeout = PING_PER_REQUEST_TIMEOUT_DEFAULT
	}

	p.logger.Debugf("execute ping cfg: %+v", cfg)

	// Validate global timeout is sufficient for the ping configuration
	theoreticalMaxTime := cfg.Count * cfg.PerRequestTimeout
	if m.Timeout < theoreticalMaxTime {
		return DownResult(fmt.Errorf("global timeout (%ds) must be >= theoretical max time (%ds = %d pings × %ds per ping)",
			m.Timeout, theoreticalMaxTime, cfg.Count, cfg.PerRequestTimeout), time.Now().UTC(), time.Now().UTC())
	}

	startTime := time.Now().UTC()

	// Try native ICMP first, fallback to system ping command
	success, rtt, err := p.tryNativePing(ctx, cfg.Host, cfg.PacketSize, cfg.Count, cfg.PerRequestTimeout, time.Duration(m.Timeout)*time.Second)
	if err != nil {
		// Fallback to system ping command
		p.logger.Debugf("Ping failed: %s, %s, %s", m.Name, err.Error(), "trying system ping")
		startTime = time.Now().UTC() // reset start time
		success, rtt, err = p.trySystemPing(ctx, cfg.Host, cfg.PacketSize, time.Duration(m.Timeout)*time.Second)
	}

	endTime := time.Now().UTC()

	if err != nil {
		p.logger.Infof("Ping failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Ping failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	if !success {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   "Ping failed: no response received",
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	p.logger.Infof("Ping successful: %s, RTT: %v", m.Name, rtt)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Ping successful, RTT: %v", rtt),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// tryNativePing attempts to use native ICMP implementation with multi-ping support
func (p *PingExecutor) tryNativePing(ctx context.Context, host string, packetSize int, count int, perRequestTimeout int, globalTimeout time.Duration) (bool, time.Duration, error) {
	// Resolve the host using context-aware DNS resolution
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false, 0, fmt.Errorf("failed to resolve host: %v", err)
	}

	// Find the first IPv4 address
	var dst *net.IPAddr
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			dst = &ip
			break
		}
	}
	if dst == nil {
		return false, 0, fmt.Errorf("no IPv4 address found for host: %s", host)
	}

	// Try to open raw socket for ICMP
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false, 0, fmt.Errorf("failed to create ICMP socket (try running as root): %v", err)
	}
	defer conn.Close()

	// Set global timeout on the connection
	deadline := time.Now().Add(globalTimeout)
	conn.SetDeadline(deadline)

	// Create ICMP message with custom data size
	// packetSize represents the data payload size (like ping -s flag)
	dataSize := packetSize
	if dataSize < 0 {
		dataSize = 0
	}
	data := make([]byte, dataSize)
	copy(data, []byte("Peekaping"))

	p.logger.Debugf("Native ping: host=%s, count=%d, perRequestTimeout=%ds, dataSize=%d", host, count, perRequestTimeout, dataSize)

	var totalRTT time.Duration
	successfulPings := 0

	// Send multiple ping packets
	for i := 0; i < count; i++ {
		// Check if global context is cancelled before each ping
		select {
		case <-ctx.Done():
			return false, 0, fmt.Errorf("ping cancelled: %v", ctx.Err())
		default:
		}

		// Create ICMP message for this ping
		msg := &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   i + 1, // Use sequence number as ID
				Seq:  i + 1,
				Data: data,
			},
		}

		msgBytes, err := msg.Marshal(nil)
		if err != nil {
			return false, 0, fmt.Errorf("failed to marshal ICMP message: %v", err)
		}

		start := time.Now()

		// Send the ping
		_, err = conn.WriteTo(msgBytes, dst)
		if err != nil {
			return false, 0, fmt.Errorf("failed to send ICMP packet: %v", err)
		}

		// Read response with per-request timeout
		reply := make([]byte, 1500)
		type readResult struct {
			n    int
			peer net.Addr
			err  error
		}

		readChan := make(chan readResult, 1)
		go func() {
			n, peer, err := conn.ReadFrom(reply)
			readChan <- readResult{n, peer, err}
		}()

		// Wait for response with per-request timeout
		perRequestCtx, cancel := context.WithTimeout(ctx, time.Duration(perRequestTimeout)*time.Second)
		defer cancel()

		select {
		case result := <-readChan:
			if result.err != nil {
				p.logger.Debugf("Ping %d failed: %v", i+1, result.err)
				continue // Try next ping
			}

			rtt := time.Since(start)

			// Parse the reply - protocol 1 for IPv4 ICMP
			replyMsg, err := icmp.ParseMessage(1, reply[:result.n])
			if err != nil {
				p.logger.Debugf("Ping %d failed to parse reply: %v", i+1, err)
				continue // Try next ping
			}

			if replyMsg.Type == ipv4.ICMPTypeEchoReply {
				p.logger.Debugf("Ping %d successful, RTT: %v", i+1, rtt)
				totalRTT += rtt
				successfulPings++
			} else {
				p.logger.Debugf("Ping %d unexpected ICMP message type: %v", i+1, replyMsg.Type)
			}

		case <-perRequestCtx.Done():
			p.logger.Debugf("Ping %d timed out after %ds", i+1, perRequestTimeout)
			continue // Try next ping
		}
	}

	// Return success if at least one ping succeeded
	if successfulPings > 0 {
		avgRTT := totalRTT / time.Duration(successfulPings)
		p.logger.Debugf("Ping completed: %d/%d successful, avg RTT: %v", successfulPings, count, avgRTT)
		return true, avgRTT, nil
	}

	return false, 0, fmt.Errorf("all %d ping attempts failed", count)
}

// trySystemPing falls back to using the system ping command
func (p *PingExecutor) trySystemPing(ctx context.Context, host string, packetSize int, timeout time.Duration) (bool, time.Duration, error) {
	var cmd *exec.Cmd

	p.logger.Debugf("System ping: host=%s, dataSize=%d, totalPacketSize=%d", host, packetSize, packetSize+8)

	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-l", strconv.Itoa(packetSize), "-w", strconv.Itoa(int(timeout.Milliseconds())), host)
	case "darwin":
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-s", strconv.Itoa(packetSize), "-W", strconv.Itoa(int(timeout.Milliseconds())), host)
	default: // linux and others
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-s", strconv.Itoa(packetSize), "-W", strconv.Itoa(int(timeout.Seconds())), host)
	}

	start := time.Now()
	output, err := cmd.Output()
	rtt := time.Since(start)

	if err != nil {
		return false, 0, fmt.Errorf("ping command failed: %v", err)
	}

	outputStr := string(output)

	// Check if ping was successful based on output
	if strings.Contains(outputStr, "100% packet loss") ||
		strings.Contains(outputStr, "100% loss") ||
		strings.Contains(outputStr, "Request timed out") ||
		strings.Contains(outputStr, "Destination host unreachable") {
		return false, rtt, nil
	}

	// Look for success indicators
	if strings.Contains(outputStr, "bytes from") ||
		strings.Contains(outputStr, "Reply from") ||
		(strings.Contains(outputStr, "packets transmitted") && !strings.Contains(outputStr, "100% packet loss")) {
		return true, rtt, nil
	}

	// If we can't determine from output, assume failure
	return false, rtt, fmt.Errorf("unable to determine ping result from output: %s", outputStr)
}
