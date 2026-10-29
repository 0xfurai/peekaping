// Package smtptest is a fake SMTP server for tests. It speaks enough of the protocol to take
// messages from net/smtp and keeps them for the test to read.
package smtptest

import (
	"bufio"
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
	reader := bufio.NewReader(conn)
	reply := func(text string) { _, _ = fmt.Fprintf(conn, "%s\r\n", text) }
	readLine := func() (string, bool) {
		text, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		return strings.TrimRight(text, "\r\n"), true
	}

	var current Message
	reply("220 smtp.example.com ESMTP")
	for {
		command, ok := readLine()
		if !ok {
			return
		}
		verb, rest, _ := strings.Cut(command, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			reply("250-smtp.example.com")
			reply("250 AUTH PLAIN")
		case "AUTH":
			current.Authenticated = true
			reply("235 2.7.0 Authentication successful")
		case "MAIL":
			current.From = address(rest)
			reply("250 2.1.0 OK")
		case "RCPT":
			s.mu.Lock()
			rejected := s.rejectRcpt
			s.mu.Unlock()
			if rejected != "" {
				reply(rejected)
				continue
			}
			current.To = append(current.To, address(rest))
			reply("250 2.1.5 OK")
		case "RSET":
			current = Message{}
			reply("250 2.0.0 OK")
		case "DATA":
			s.mu.Lock()
			rejected := s.rejectData
			s.mu.Unlock()
			if rejected != "" {
				reply(rejected)
				continue
			}
			reply("354 End data with <CR><LF>.<CR><LF>")
			var data strings.Builder
			for {
				line, ok := readLine()
				if !ok {
					return
				}
				if line == "." {
					break
				}
				data.WriteString(strings.TrimPrefix(line, ".") + "\r\n")
			}
			current.Data = data.String()
			s.mu.Lock()
			s.messages = append(s.messages, current)
			s.mu.Unlock()
			current = Message{}
			reply("250 2.0.0 OK: queued")
		case "QUIT":
			reply("221 2.0.0 Bye")
			return
		default:
			reply("502 5.5.2 Command not recognized")
		}
	}
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
