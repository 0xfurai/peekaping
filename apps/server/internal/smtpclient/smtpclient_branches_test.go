package smtpclient_test

import (
	"bufio"
	"context"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/smtpclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedServer is an SMTP server that answers each command with the reply the test gives for its
// verb, and keeps the verbs it was sent. It offers the extensions it is told to.
type scriptedServer struct {
	host       string
	port       int
	mu         sync.Mutex
	verbs      []string
	replies    map[string]string
	extensions []string
	// hangUpAfterData closes the connection once it has said it takes the message
	hangUpAfterData bool
}

func startScripted(t *testing.T, extensions []string, replies map[string]string) *scriptedServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	s := &scriptedServer{host: "127.0.0.1", port: listener.Addr().(*net.TCPAddr).Port, replies: replies, extensions: extensions}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *scriptedServer) reply(verb, otherwise string) string {
	if text, ok := s.replies[verb]; ok {
		return text
	}
	return otherwise
}

func (s *scriptedServer) serve(conn net.Conn) {
	defer conn.Close()
	say := func(text string) { _, _ = conn.Write([]byte(text + "\r\n")) }
	reader := bufio.NewReader(conn)
	say(s.reply("GREETING", "220 scripted.example.com ESMTP"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		verb, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		verb = strings.ToUpper(verb)
		s.mu.Lock()
		s.verbs = append(s.verbs, verb)
		s.mu.Unlock()
		switch verb {
		case "EHLO", "HELO":
			if text, ok := s.replies["EHLO"]; ok {
				say(text)
				continue
			}
			lines := append([]string{"scripted.example.com"}, s.extensions...)
			for i, l := range lines {
				sep := "-"
				if i == len(lines)-1 {
					sep = " "
				}
				say("250" + sep + l)
			}
		case "DATA":
			say(s.reply("DATA", "354 go ahead"))
			if s.replies["DATA"] != "" && !strings.HasPrefix(s.replies["DATA"], "354") {
				continue
			}
			if s.hangUpAfterData {
				return
			}
			for {
				l, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			say(s.reply("END", "250 queued"))
		case "QUIT":
			say(s.reply("QUIT", "221 bye"))
			return
		case "AUTH":
			say(s.reply("AUTH", "235 welcome"))
		case "STARTTLS":
			say(s.reply("STARTTLS", "502 no TLS here"))
		default:
			say(s.reply(verb, "250 ok"))
		}
	}
}

func (s *scriptedServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.verbs...)
}

func (s *scriptedServer) send(t *testing.T, mutate func(*smtpclient.Server)) error {
	t.Helper()
	server := smtpclient.Server{Host: s.host, Port: s.port}
	if mutate != nil {
		mutate(&server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return smtpclient.Send(ctx, server, "from@example.com", []string{"a@example.com", "b@example.com"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))
}

func TestSend_ASuccessfulSendSaysItsCommandsInOrder(t *testing.T) {
	server := startScripted(t, []string{"AUTH PLAIN"}, nil)

	err := server.send(t, func(s *smtpclient.Server) {
		s.TLS = smtpclient.None
		s.Auth = smtp.PlainAuth("", "user", "pass", s.Host)
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"EHLO", "AUTH", "MAIL", "RCPT", "RCPT", "DATA", "QUIT"}, server.seen())
}

func TestSend_FailsAtTheFirstCommandTheServerRefuses(t *testing.T) {
	tests := []struct {
		name       string
		extensions []string
		replies    map[string]string
		mutate     func(*smtpclient.Server)
		wantErr    string
		wantVerbs  []string
	}{
		{name: "a server that refuses the connection", replies: map[string]string{"GREETING": "554 go away"}, wantErr: "554 go away", wantVerbs: nil},
		{name: "a server that refuses hello", replies: map[string]string{"EHLO": "554 no hello"}, wantErr: "554 no hello", wantVerbs: []string{"EHLO", "HELO"}},
		{name: "a server with no AUTH, asked to log in", mutate: func(s *smtpclient.Server) { s.Auth = smtp.PlainAuth("", "u", "p", s.Host) }, wantErr: "smtp: server doesn't support AUTH", wantVerbs: []string{"EHLO"}},
		{name: "a login the server refuses", extensions: []string{"AUTH PLAIN"}, replies: map[string]string{"AUTH": "535 5.7.8 bad credentials"}, mutate: func(s *smtpclient.Server) { s.Auth = smtp.PlainAuth("", "u", "p", s.Host) }, wantErr: "535 5.7.8 bad credentials", wantVerbs: []string{"EHLO", "AUTH"}},
		{name: "a sender the server refuses", replies: map[string]string{"MAIL": "553 5.1.8 sender refused"}, wantErr: "553 5.1.8 sender refused", wantVerbs: []string{"EHLO", "MAIL"}},
		{name: "the first recipient the server refuses", replies: map[string]string{"RCPT": "550 5.1.1 no such user"}, wantErr: "550 5.1.1 no such user", wantVerbs: []string{"EHLO", "MAIL", "RCPT"}},
		{name: "a message the server will not take", replies: map[string]string{"DATA": "554 5.3.0 not today"}, wantErr: "554 5.3.0 not today", wantVerbs: []string{"EHLO", "MAIL", "RCPT", "RCPT", "DATA"}},
		{name: "a message the server refuses after it has it", replies: map[string]string{"END": "552 5.3.4 too big"}, wantErr: "552 5.3.4 too big", wantVerbs: []string{"EHLO", "MAIL", "RCPT", "RCPT", "DATA"}},
		{name: "a goodbye the server answers badly", replies: map[string]string{"QUIT": "500 what"}, wantErr: "500 what", wantVerbs: []string{"EHLO", "MAIL", "RCPT", "RCPT", "DATA", "QUIT"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := startScripted(t, tc.extensions, tc.replies)

			err := server.send(t, func(s *smtpclient.Server) {
				s.TLS = smtpclient.None
				if tc.mutate != nil {
					tc.mutate(s)
				}
			})

			require.Error(t, err)
			// net/smtp may quote the server's text, so the code and the words are read apart
			code, words, _ := strings.Cut(tc.wantErr, " ")
			assert.Contains(t, err.Error(), code)
			assert.Contains(t, err.Error(), words)
			seen := server.seen()
			// A client that fails may say QUIT or cancel the login on its way out
			assert.Equal(t, tc.wantVerbs, seen[:min(len(seen), len(tc.wantVerbs))], "the commands sent, in order")
		})
	}
}

func TestSend_FailsWhenTheServerHangsUpOnTheMessage(t *testing.T) {
	server := startScripted(t, nil, nil)
	server.hangUpAfterData = true
	big := make([]byte, 16<<20)
	for i := range big {
		big[i] = 'x'
		if i%70 == 69 {
			big[i] = '\n'
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := smtpclient.Send(ctx, smtpclient.Server{Host: server.host, Port: server.port, TLS: smtpclient.None}, "from@example.com", []string{"a@example.com"}, big)

	assert.Error(t, err)
}

// What the mode does with a server that offers STARTTLS: Opportunistic and the modes that are not
// known try it, StartTLS needs it, and None and TLS never ask. The server here offers it and
// refuses it, so that a mode that asks fails and one that doesn't sends.
func TestSend_ModesTryStartTLSOnlyAsTheyShould(t *testing.T) {
	tests := []struct {
		mode      smtpclient.TLSMode
		asks      bool
		wantErr   string
		wantVerbs []string
	}{
		{mode: smtpclient.Opportunistic, asks: true, wantErr: "502 no TLS here", wantVerbs: []string{"EHLO", "STARTTLS"}},
		{mode: "", asks: true, wantErr: "502 no TLS here", wantVerbs: []string{"EHLO", "STARTTLS"}},
		{mode: "something else", asks: true, wantErr: "502 no TLS here", wantVerbs: []string{"EHLO", "STARTTLS"}},
		{mode: smtpclient.StartTLS, asks: true, wantErr: "502 no TLS here", wantVerbs: []string{"EHLO", "STARTTLS"}},
		{mode: smtpclient.None, wantVerbs: []string{"EHLO", "MAIL", "RCPT", "RCPT", "DATA", "QUIT"}},
	}
	for _, tc := range tests {
		t.Run("mode "+string(tc.mode), func(t *testing.T) {
			server := startScripted(t, []string{"STARTTLS"}, nil)

			err := server.send(t, func(s *smtpclient.Server) { s.TLS = tc.mode })

			if tc.asks {
				require.Error(t, err)
				code, words, _ := strings.Cut(tc.wantErr, " ")
				assert.Contains(t, err.Error(), code)
				assert.Contains(t, err.Error(), words)
			} else {
				require.NoError(t, err)
			}
			seen := server.seen()
			assert.Equal(t, tc.wantVerbs, seen[:min(len(seen), len(tc.wantVerbs))])
		})
	}
}

// StartTLS and Opportunistic read the server's extensions after hello, so a server that offers none
// is told apart from one that refuses
func TestSend_WithoutSTARTTLSTheModesPartWays(t *testing.T) {
	t.Run("opportunistic sends in the clear", func(t *testing.T) {
		server := startScripted(t, nil, nil)
		require.NoError(t, server.send(t, nil))
		assert.NotContains(t, server.seen(), "STARTTLS")
	})
	t.Run("starttls stops before it sends", func(t *testing.T) {
		server := startScripted(t, nil, nil)
		err := server.send(t, func(s *smtpclient.Server) { s.TLS = smtpclient.StartTLS })
		assert.EqualError(t, err, "smtp: server doesn't support STARTTLS")
		assert.Equal(t, []string{"EHLO"}, server.seen())
	})
}
