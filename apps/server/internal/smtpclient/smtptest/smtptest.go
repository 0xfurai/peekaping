// Package smtptest is a fake SMTP server for tests. It speaks enough of the protocol to take
// messages from net/smtp and keeps them for the test to read.
package smtptest

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Message is one message the server took
type Message struct {
	// From and To are the envelope's sender and recipients
	From string
	To   []string
	// Data is the message as sent, headers and body, with the dot-stuffing removed
	Data string
	// Authenticated is whether the client logged in before it sent the message
	Authenticated bool
	// User and Password are what the client logged in with (AUTH PLAIN)
	User, Password string
}

// Server is a fake SMTP server on a loopback port
type Server struct {
	listener net.Listener

	mu       sync.Mutex
	messages []Message
	// rejectData, when set, is the reply to DATA instead of accepting the message
	rejectData string
	// rejectRcpt, when set, is the reply to RCPT instead of accepting the recipient
	rejectRcpt string
}

// Start runs a server until the test ends
func Start(t testing.TB) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("smtptest: %v", err)
	}
	s := &Server{listener: listener}
	t.Cleanup(func() { _ = listener.Close() })
	go s.accept()
	return s
}

// Host and Port say where the server listens
func (s *Server) Host() string { return "127.0.0.1" }
func (s *Server) Port() int    { return s.listener.Addr().(*net.TCPAddr).Port }

// RejectMessages makes the server answer every message with the SMTP reply (such as "554 no"),
// to test a send that fails. An empty reply accepts messages again.
func (s *Server) RejectMessages(reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectData = reply
}

// RejectRecipients makes the server answer every RCPT with the SMTP reply (such as
// "550 5.1.1 <alice@example.com>: user unknown"), the way a server that echoes the address does.
// An empty reply accepts recipients again.
func (s *Server) RejectRecipients(reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectRcpt = reply
}

// Messages returns the messages taken so far, oldest first
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

// WaitFor waits up to five seconds for n messages and returns them all. It fails the test when
// fewer arrive.
func (s *Server) WaitFor(t testing.TB, n int) []Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.Messages(); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("smtptest: waited for %d messages, got %d", n, len(s.Messages()))
	return nil
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	sess := &session{server: s, conn: conn, reader: bufio.NewReader(conn)}

	sess.reply("220 smtp.example.com ESMTP")
	for {
		command, ok := sess.readLine()
		if !ok {
			return
		}
		verb, rest, _ := strings.Cut(command, " ")
		if !sess.handle(strings.ToUpper(verb), rest) {
			return
		}
	}
}

// session is one client's conversation with the server, and the message it is writing
type session struct {
	server  *Server
	conn    net.Conn
	reader  *bufio.Reader
	current Message
}

func (c *session) reply(text string) { _, _ = fmt.Fprintf(c.conn, "%s\r\n", text) }

func (c *session) readLine() (string, bool) {
	text, err := c.reader.ReadString('\n')
	if err != nil {
		return "", false
	}
	return strings.TrimRight(text, "\r\n"), true
}

// handle answers one command, and reports whether the conversation goes on
func (c *session) handle(verb, rest string) bool {
	switch verb {
	case "EHLO", "HELO":
		c.reply("250-smtp.example.com")
		c.reply("250 AUTH PLAIN")
	case "AUTH":
		c.current.Authenticated = true
		c.current.User, c.current.Password = plainCredentials(rest)
		c.reply("235 2.7.0 Authentication successful")
	case "MAIL":
		c.current.From = address(rest)
		c.reply("250 2.1.0 OK")
	case "RCPT":
		c.recipient(rest)
	case "RSET":
		c.current = Message{}
		c.reply("250 2.0.0 OK")
	case "DATA":
		return c.data()
	case "QUIT":
		c.reply("221 2.0.0 Bye")
		return false
	default:
		c.reply("502 5.5.2 Command not recognized")
	}
	return true
}

// recipient takes the recipient of the message, or answers with the rejection the test set
func (c *session) recipient(rest string) {
	c.server.mu.Lock()
	rejected := c.server.rejectRcpt
	c.server.mu.Unlock()
	if rejected != "" {
		c.reply(rejected)
		return
	}
	c.current.To = append(c.current.To, address(rest))
	c.reply("250 2.1.5 OK")
}

// data takes the message, or answers with the rejection the test set, and reports whether the
// conversation goes on: it does not when the client hangs up before the message ends
func (c *session) data() bool {
	c.server.mu.Lock()
	rejected := c.server.rejectData
	c.server.mu.Unlock()
	if rejected != "" {
		c.reply(rejected)
		return true
	}
	c.reply("354 End data with <CR><LF>.<CR><LF>")
	var data strings.Builder
	for {
		line, ok := c.readLine()
		if !ok {
			return false
		}
		if line == "." {
			break
		}
		data.WriteString(strings.TrimPrefix(line, ".") + "\r\n")
	}
	c.current.Data = data.String()
	c.server.mu.Lock()
	c.server.messages = append(c.server.messages, c.current)
	c.server.mu.Unlock()
	c.current = Message{}
	c.reply("250 2.0.0 OK: queued")
	return true
}

// plainCredentials returns the user and password of "PLAIN <base64 of \x00user\x00password>"
func plainCredentials(rest string) (string, string) {
	_, encoded, _ := strings.Cut(rest, " ")
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(decoded), "\x00", 3)
	if len(parts) != 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

// address returns the address inside "FROM:<a@b>" or "TO:<a@b>"
func address(rest string) string {
	_, after, _ := strings.Cut(rest, ":")
	after = strings.TrimSpace(after)
	if start := strings.Index(after, "<"); start >= 0 {
		if end := strings.Index(after[start:], ">"); end > 0 {
			return after[start+1 : start+end]
		}
	}
	return after
}
