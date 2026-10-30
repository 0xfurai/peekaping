package executor

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	protocolICMP     = 1
	protocolIPv6ICMP = 58
)

// errNoICMPSocket means neither an unprivileged nor a raw ICMP socket could be opened.
var errNoICMPSocket = errors.New("cannot open an ICMP socket")

// Raw ICMP sockets receive every ICMP packet on the host, so each echo request
// carries this process's ID and its own sequence number to tell replies apart.
var (
	echoID  = rand.IntN(0xffff) + 1
	echoSeq atomic.Uint32
)

// packetConn is the part of *icmp.PacketConn that an echo exchange uses.
type packetConn interface {
	ReadFrom(b []byte) (int, net.Addr, error)
	WriteTo(b []byte, dst net.Addr) (int, error)
}

type PingConfig struct {
	Host       string `json:"host" validate:"required" example:"example.com"`
	PacketSize int    `json:"packet_size" validate:"min=0,max=65507" example:"32"`
}

type PingExecutor struct {
	guarded
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

// Targets lists the host a ping check sends to
func (s *PingExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[PingConfig](configJSON)
	if err != nil {
		return nil, err
	}
	return []string{cfg.Host}, nil
}

// guardedHost checks the address a ping would send to, when the guard is on, and returns the host
// to use. An echo request is not a dial the guard can see, so the name is resolved here and the
// checks go to the address that was checked, as a literal: resolving it again could give another.
// With the guard off the host is returned as it is.
func (p *PingExecutor) guardedHost(ctx context.Context, host string) (string, error) {
	if !p.guard.Enabled() {
		return host, nil
	}
	addrs, err := p.guard.ResolveAndCheck(ctx, host)
	if errors.Is(err, netguard.ErrBlocked) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve host: %v", err)
	}
	return preferIPv4(addrs).String(), nil
}

// preferIPv4 picks the address to ping from a resolved list that is not empty
func preferIPv4(addrs []netip.Addr) netip.Addr {
	for _, addr := range addrs {
		if addr.Unmap().Is4() {
			return addr.Unmap()
		}
	}
	return addrs[0]
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

	startTime := time.Now().UTC()
	timeout := time.Duration(m.Timeout) * time.Second
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	host, err := p.guardedHost(pingCtx, cfg.Host)
	if err != nil {
		p.logger.Infof("Ping failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("Ping failed: %v", err),
			StartTime: startTime,
			EndTime:   time.Now().UTC(),
		}
	}

	// The system ping command is only a fallback for hosts where no ICMP socket
	// can be opened. A timeout from the native ping is a real result.
	success, rtt, err := p.tryNativePing(pingCtx, host, cfg.PacketSize, timeout)
	if errors.Is(err, errNoICMPSocket) {
		p.logger.Debugf("Ping failed: %s, %s, %s", m.Name, err.Error(), "trying system ping")
		nativeErr := err
		startTime = time.Now().UTC() // reset start time
		success, rtt, err = p.trySystemPing(pingCtx, host, cfg.PacketSize, timeout)
		switch {
		case errors.Is(err, exec.ErrNotFound):
			err = nativeErr
		case err != nil:
			err = fmt.Errorf("%w (system ping: %v)", nativeErr, err)
		}
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

// tryNativePing resolves host, sends one ICMP echo request and waits for its
// reply. All of that has to fit in the timeout or the context deadline,
// whichever comes first.
func (p *PingExecutor) tryNativePing(ctx context.Context, host string, packetSize int, timeout time.Duration) (bool, time.Duration, error) {
	start := time.Now()
	deadline := pingDeadline(ctx, start, timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	dst, err := resolvePingTarget(ctx, host)
	if err != nil {
		return false, 0, err
	}
	v6 := dst.IP.To4() == nil

	conn, dgram, err := listenICMP(v6)
	if err != nil {
		return false, 0, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	if err := conn.SetDeadline(deadline); err != nil {
		return false, 0, fmt.Errorf("failed to set ICMP socket deadline: %v", err)
	}

	// packetSize represents the data payload size (like ping -s flag)
	data := make([]byte, max(packetSize, 0))
	copy(data, []byte("Peekaping"))
	req := &icmp.Echo{ID: echoID, Seq: int(uint16(echoSeq.Add(1))), Data: data}

	var target net.Addr = dst
	if dgram {
		target = &net.UDPAddr{IP: dst.IP, Zone: dst.Zone}
	}
	// On Linux the kernel rewrites the ID of datagram ICMP sockets and only
	// delivers replies that carry it, so the reply's ID won't match ours.
	checkID := !dgram || runtime.GOOS != "linux"

	p.logger.Debugf("Native ping: host=%s, ip=%s, dgram=%t, dataSize=%d, totalPacketSize=%d", host, dst, dgram, len(data), len(data)+8)

	rtt, err := exchangeEcho(conn, target, req, v6, checkID)
	return echoOutcome(ctx, dst, rtt, err, start, deadline)
}

// pingDeadline is when a ping that starts now has to be answered by: the timeout from start, or
// the context's deadline when that is sooner.
func pingDeadline(ctx context.Context, start time.Time, timeout time.Duration) time.Time {
	deadline := start.Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return deadline
}

// echoOutcome is the result of a native ping from how the echo exchange went
func echoOutcome(ctx context.Context, dst *net.IPAddr, rtt time.Duration, err error, start, deadline time.Time) (bool, time.Duration, error) {
	switch {
	case err == nil:
		return true, rtt, nil
	// The context expires together with the socket deadline, so either one
	// means the host didn't answer in time.
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return false, 0, fmt.Errorf("no reply from %s within %v", dst, max(deadline.Sub(start), 0).Round(100*time.Millisecond))
	case ctx.Err() != nil:
		return false, 0, fmt.Errorf("no reply from %s: %v", dst, ctx.Err())
	default:
		return false, 0, err
	}
}

// resolvePingTarget resolves host to the address to ping, preferring IPv4.
func resolvePingTarget(ctx context.Context, host string) (*net.IPAddr, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve host: %v", err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("failed to resolve host: no addresses for %s", host)
	}
	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			return &addr, nil
		}
	}
	return &addrs[0], nil
}

// listenICMP opens an unprivileged datagram ICMP socket, which Linux allows
// for groups in the net.ipv4.ping_group_range sysctl. Failing that, it opens
// a raw ICMP socket, which needs root with the NET_RAW capability.
func listenICMP(v6 bool) (*icmp.PacketConn, bool, error) {
	dgramNetwork, rawNetwork, address := "udp4", "ip4:icmp", "0.0.0.0"
	if v6 {
		dgramNetwork, rawNetwork, address = "udp6", "ip6:ipv6-icmp", "::"
	}

	conn, dgramErr := icmp.ListenPacket(dgramNetwork, address)
	if dgramErr == nil {
		return conn, true, nil
	}
	conn, rawErr := icmp.ListenPacket(rawNetwork, address)
	if rawErr == nil {
		return conn, false, nil
	}
	err := fmt.Errorf("%w: %s: %v, %v", errNoICMPSocket, dgramNetwork, dgramErr, rawErr)
	if errors.Is(rawErr, os.ErrPermission) {
		err = fmt.Errorf("%w; allow unprivileged ICMP with the net.ipv4.ping_group_range sysctl, or run Peekaping as root with the NET_RAW capability", err)
	}
	return nil, false, err
}

// exchangeEcho sends req to dst and returns the round-trip time of its reply.
// Packets that aren't the reply to req are skipped until conn's deadline.
func exchangeEcho(conn packetConn, dst net.Addr, req *icmp.Echo, v6, checkID bool) (time.Duration, error) {
	requestType, replyType, proto := echoTypes(v6)

	msg, err := (&icmp.Message{Type: requestType, Body: req}).Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal ICMP message: %v", err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(msg, dst); err != nil {
		return 0, fmt.Errorf("failed to send ICMP packet: %v", err)
	}

	buf := make([]byte, max(1500, len(msg)+60))
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, err
		}
		echo, ok := parseEchoReply(proto, replyType, buf[:n])
		if !ok || !answersRequest(echo, req, checkID, peer, dst) {
			continue
		}
		return time.Since(start), nil
	}
}

// echoTypes is the ICMP types of an echo request and its reply, and the protocol number of the
// messages, for IPv4 or IPv6
func echoTypes(v6 bool) (requestType, replyType icmp.Type, proto int) {
	if v6 {
		return ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply, protocolIPv6ICMP
	}
	return ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply, protocolICMP
}

// parseEchoReply parses the packet as an echo reply of the given type. It reports false for any
// other packet.
func parseEchoReply(proto int, replyType icmp.Type, packet []byte) (*icmp.Echo, bool) {
	reply, err := icmp.ParseMessage(proto, packet)
	if err != nil || reply.Type != replyType {
		return nil, false
	}
	echo, ok := reply.Body.(*icmp.Echo)
	return echo, ok
}

// answersRequest reports whether the echo reply, which came from peer, is the reply to req sent to
// dst. checkID is false where the kernel rewrites the ID.
func answersRequest(echo, req *icmp.Echo, checkID bool, peer, dst net.Addr) bool {
	return echo.Seq == req.Seq && (!checkID || echo.ID == req.ID) && sameIP(peer, dst)
}

func sameIP(a, b net.Addr) bool {
	ipA, ipB := addrIP(a), addrIP(b)
	return ipA != nil && ipA.Equal(ipB)
}

func addrIP(addr net.Addr) net.IP {
	switch addr := addr.(type) {
	case *net.IPAddr:
		return addr.IP
	case *net.UDPAddr:
		return addr.IP
	}
	return nil
}

// trySystemPing falls back to using the system ping command
func (p *PingExecutor) trySystemPing(ctx context.Context, host string, packetSize int, timeout time.Duration) (bool, time.Duration, error) {
	p.logger.Debugf("System ping: host=%s, dataSize=%d, totalPacketSize=%d", host, packetSize, packetSize+8)

	cmd := exec.CommandContext(ctx, "ping", systemPingArgs(runtime.GOOS, host, packetSize, timeout)...)

	start := time.Now()
	output, err := cmd.Output()
	rtt := time.Since(start)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// ping exits non-zero both when no reply arrives and when it can't
		// run; only the second writes to stderr.
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return false, 0, fmt.Errorf("ping command failed: %v: %s", err, stderr)
		}
		return false, rtt, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("ping command failed: %w", err)
	}

	success, err := readPingOutput(string(output))
	return success, rtt, err
}

// systemPingArgs is the arguments of the ping command on the operating system: one packet of the
// given data size, answered within the timeout (which Windows and macOS give in milliseconds, and
// other systems in seconds)
func systemPingArgs(goos, host string, packetSize int, timeout time.Duration) []string {
	switch goos {
	case "windows":
		return []string{"-n", "1", "-l", strconv.Itoa(packetSize), "-w", strconv.Itoa(int(timeout.Milliseconds())), host}
	case "darwin":
		return []string{"-c", "1", "-s", strconv.Itoa(packetSize), "-W", strconv.Itoa(int(timeout.Milliseconds())), host}
	default: // linux and others
		return []string{"-c", "1", "-s", strconv.Itoa(packetSize), "-W", strconv.Itoa(int(timeout.Seconds())), host}
	}
}

var (
	pingFailureMarkers = []string{"100% packet loss", "100% loss", "Request timed out", "Destination host unreachable"}
	pingSuccessMarkers = []string{"bytes from", "Reply from"}
)

// containsAny reports whether s contains any of the substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// readPingOutput says from the output of the ping command whether the host replied
func readPingOutput(outputStr string) (bool, error) {
	// Check if ping was successful based on output
	if containsAny(outputStr, pingFailureMarkers) {
		return false, nil
	}

	// Look for success indicators
	if containsAny(outputStr, pingSuccessMarkers) ||
		(strings.Contains(outputStr, "packets transmitted") && !strings.Contains(outputStr, "100% packet loss")) {
		return true, nil
	}

	// If we can't determine from output, assume failure
	return false, fmt.Errorf("unable to determine ping result from output: %s", outputStr)
}
