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

type PingConfig struct {
	Host       string `json:"host" validate:"required" example:"example.com"`
	PacketSize int    `json:"packet_size" validate:"min=0,max=65507" example:"32"`
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
	return GenericValidator(cfg.(*PingConfig))
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

	p.logger.Debugf("execute ping cfg: %+v", cfg)

	startTime := time.Now().UTC()

	// Try native ICMP first, fallback to system ping command
	success, rtt, err := p.tryNativePing(ctx, cfg.Host, cfg.PacketSize, time.Duration(m.Timeout)*time.Second)
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

// tryNativePing attempts to use native ICMP implementation
func (p *PingExecutor) tryNativePing(ctx context.Context, host string, packetSize int, timeout time.Duration) (bool, time.Duration, error) {
	// Resolve the host
	dst, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return false, 0, fmt.Errorf("failed to resolve host: %v", err)
	}

	// Try to open raw socket for ICMP
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return false, 0, fmt.Errorf("failed to create ICMP socket (try running as root): %v", err)
	}
	defer conn.Close()

	// Set timeout
	deadline := time.Now().Add(timeout)
	conn.SetDeadline(deadline)

	// Create ICMP message with custom data size
	// packetSize represents the data payload size (like ping -s flag)
	dataSize := packetSize
	if dataSize < 0 {
		dataSize = 0
	}
	data := make([]byte, dataSize)
	copy(data, []byte("Peekaping"))

	p.logger.Debugf("Native ping: host=%s, dataSize=%d, totalPacketSize=%d", host, dataSize, dataSize+8)

	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   1,
			Seq:  1,
			Data: data,
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to marshal ICMP message: %v", err)
	}

	start := time.Now()
	_, err = conn.WriteTo(msgBytes, dst)
	if err != nil {
		return false, 0, fmt.Errorf("failed to send ICMP packet: %v", err)
	}

	// Read response
	reply := make([]byte, 1500)
	n, peer, err := conn.ReadFrom(reply)
	if err != nil {
		return false, 0, fmt.Errorf("failed to read ICMP reply: %v", err)
	}
	rtt := time.Since(start)

	// Parse the reply - protocol 1 for IPv4 ICMP
	replyMsg, err := icmp.ParseMessage(1, reply[:n])
	if err != nil {
		return false, 0, fmt.Errorf("failed to parse ICMP reply: %v", err)
	}

	if replyMsg.Type == ipv4.ICMPTypeEchoReply {
		p.logger.Debugf("Received ICMP reply from %v", peer)
		return true, rtt, nil
	}

	return false, 0, fmt.Errorf("unexpected ICMP message type: %v", replyMsg.Type)
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
