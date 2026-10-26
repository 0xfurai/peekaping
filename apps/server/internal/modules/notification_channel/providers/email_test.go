package providers

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMTPServer accepts SMTP sessions on localhost and records the commands clients send,
// with the message data. net/smtp only sends PLAIN credentials unencrypted to localhost.
type fakeSMTPServer struct {
	listener net.Listener
	sessions chan string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	return startFakeSMTPServerOn(t, "127.0.0.1:0")
}

func startFakeSMTPServerOn(t *testing.T, addr string) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	s := &fakeSMTPServer{listener: listener, sessions: make(chan string, 1)}
	t.Cleanup(func() { _ = listener.Close() })
	go s.serve()
	return s
}

func (s *fakeSMTPServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *fakeSMTPServer) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	var transcript strings.Builder
	defer func() { s.sessions <- transcript.String() }()
	reader := bufio.NewReader(conn)
	reply := func(text string) { _, _ = fmt.Fprintf(conn, "%s\r\n", text) }
	readLine := func() (string, bool) {
		text, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		text = strings.TrimRight(text, "\r\n")
		transcript.WriteString(text + "\n")
		return text, true
	}

	reply("220 smtp.example.com ESMTP")
	for {
		command, ok := readLine()
		if !ok {
			return
		}
		verb, _, _ := strings.Cut(command, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			reply("250-smtp.example.com")
			reply("250 AUTH PLAIN")
		case "AUTH":
			reply("235 2.7.0 Authentication successful")
		case "MAIL", "RCPT":
			reply("250 2.1.0 OK")
		case "DATA":
			reply("354 End data with <CR><LF>.<CR><LF>")
			for {
				data, ok := readLine()
				if !ok {
					return
				}
				if data == "." {
					break
				}
			}
			reply("250 2.0.0 OK: queued")
		case "QUIT":
			reply("221 2.0.0 Bye")
			return
		default:
			reply("502 5.5.2 Command not recognized")
		}
	}
}

// smtpConfig is an SMTP channel's config without its server, which tests fill in
const smtpConfig = `{"username": "alerts@example.com", "password": "smtp-pass", "from": "alerts@example.com", "to": "oncall@example.com"}`

// emailGoldenCases have no sender; they all use EmailSender. Their configs leave out the
// SMTP server, which the test fills in.
var emailGoldenCases = []goldenCase{
	{name: "smtp_down", event: downEvent, config: smtpConfig},
	{name: "smtp_templates_up", event: upEvent,
		config: `{"username": "alerts@example.com", "password": "smtp-pass", "from": "alerts@example.com", "to": "oncall@example.com", "custom_subject": "[{{ status }}] {{ name }}", "custom_body": "{{ name }} is {{ status }}: {{ msg }} ({{ ping }} ms)"}`},
	{name: "smtp_certificate_expiry", event: certExpiryEvent, config: smtpConfig},
}

// TestEmailSender_SendGoldenRequests sends each case's notification to a fake SMTP server and
// compares the SMTP session with its golden file
func TestEmailSender_SendGoldenRequests(t *testing.T) {
	for _, tc := range emailGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			server := startFakeSMTPServer(t)
			config := withSMTPServer(t, tc.config, server.port())
			sender := NewEmailSender(nopLogger())
			message, mon, hb := goldenNotification(tc.event)

			require.NoError(t, sender.Validate(config), "the case's config must pass validation, as the listener checks it first")
			require.NoError(t, sender.Send(context.Background(), config, message, mon, hb))

			assertGolden(t, tc.name, <-server.sessions)
		})
	}
}

// TestEmailSender_SendToIPv6Host checks that a bare IPv6 address works as the SMTP host
func TestEmailSender_SendToIPv6Host(t *testing.T) {
	if l, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	} else {
		_ = l.Close()
	}
	server := startFakeSMTPServerOn(t, "[::1]:0")
	config := strings.Replace(withSMTPServer(t, emailGoldenCases[1].config, server.port()), "127.0.0.1", "::1", 1)
	message, mon, hb := goldenNotification(emailGoldenCases[1].event)

	require.NoError(t, NewEmailSender(nopLogger()).Send(context.Background(), config, message, mon, hb))

	assert.Contains(t, <-server.sessions, "EHLO")
}

// TestEmailSender_SendWithoutAMonitor checks that a template that names the monitor renders, and
// doesn't panic, when the notification has none
func TestEmailSender_SendWithoutAMonitor(t *testing.T) {
	for _, withHeartbeat := range []*heartbeat.Model{nil, goldenDown, goldenUp} {
		server := startFakeSMTPServer(t)
		config := withSMTPServer(t, emailGoldenCases[1].config, server.port())

		require.NotPanics(t, func() {
			assert.NoError(t, NewEmailSender(nopLogger()).Send(context.Background(), config, "A message with no monitor", nil, withHeartbeat))
		})
		assert.Contains(t, <-server.sessions, "A message with no monitor")
	}
}

// withSMTPServer points an SMTP config at the fake server
func withSMTPServer(t *testing.T, config string, port int) string {
	t.Helper()
	require.True(t, strings.HasPrefix(config, "{"))
	return fmt.Sprintf(`{"smtp_host": "127.0.0.1", "smtp_port": %d, %s`, port, config[1:])
}

func TestEmailSender_SendStopsAtContextDeadline(t *testing.T) {
	// The server accepts the connection but never greets the client
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = conn.Read(make([]byte, 1))
		}
	}()
	config := withSMTPServer(t, smtpConfig, listener.Addr().(*net.TCPAddr).Port)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = NewEmailSender(nopLogger()).Send(ctx, config, goldenDown.Msg, goldenMonitor, goldenDown)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}
