package smtptest_test

import (
	"bufio"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// talk is a client of the fake server that speaks raw SMTP
type talk struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
}

func dial(t *testing.T, server *smtptest.Server) *talk {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(server.Host(), strconv.Itoa(server.Port())), 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &talk{t: t, conn: conn, reader: bufio.NewReader(conn)}
}

// line reads one reply line, without its line break
func (c *talk) line() string {
	c.t.Helper()
	require.NoError(c.t, c.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	text, err := c.reader.ReadString('\n')
	require.NoError(c.t, err)
	return strings.TrimRight(text, "\r\n")
}

// say sends the lines, each with its line break
func (c *talk) say(lines ...string) {
	c.t.Helper()
	for _, l := range lines {
		_, err := c.conn.Write([]byte(l + "\r\n"))
		require.NoError(c.t, err)
	}
}

// ask sends one command and returns the reply line
func (c *talk) ask(command string) string {
	c.t.Helper()
	c.say(command)
	return c.line()
}

func plain(user, password string) string {
	return "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00"+user+"\x00"+password))
}

func TestServer_GreetsAndAnswersHello(t *testing.T) {
	for _, hello := range []string{"EHLO client.example.com", "HELO client.example.com", "ehlo client.example.com", "Helo x"} {
		t.Run(hello, func(t *testing.T) {
			c := dial(t, smtptest.Start(t))
			assert.Equal(t, "220 smtp.example.com ESMTP", c.line())

			c.say(hello)

			assert.Equal(t, "250-smtp.example.com", c.line())
			assert.Equal(t, "250 AUTH PLAIN", c.line())
		})
	}
}

func TestServer_TakesAMessageWithItsEnvelopeAndLogin(t *testing.T) {
	server := smtptest.Start(t)
	c := dial(t, server)
	c.line()

	assert.Equal(t, "235 2.7.0 Authentication successful", c.ask(plain("alice", "s3cret")))
	assert.Equal(t, "250 2.1.0 OK", c.ask("MAIL FROM:<from@example.com> SIZE=10"))
	assert.Equal(t, "250 2.1.5 OK", c.ask("RCPT TO:<a@example.com>"))
	assert.Equal(t, "250 2.1.5 OK", c.ask("rcpt to: b@example.com"))
	assert.Equal(t, "354 End data with <CR><LF>.<CR><LF>", c.ask("DATA"))
	c.say("Subject: Hi", "", "..a line that starts with a dot", "last line", ".")
	assert.Equal(t, "250 2.0.0 OK: queued", c.line())

	messages := server.WaitFor(t, 1)
	require.Len(t, messages, 1)
	assert.Equal(t, "from@example.com", messages[0].From)
	assert.Equal(t, []string{"a@example.com", "b@example.com"}, messages[0].To)
	assert.Equal(t, "Subject: Hi\r\n\r\n.a line that starts with a dot\r\nlast line\r\n", messages[0].Data)
	assert.True(t, messages[0].Authenticated)
	assert.Equal(t, "alice", messages[0].User)
	assert.Equal(t, "s3cret", messages[0].Password)

	// The next message on the same connection starts from nothing
	assert.Equal(t, "250 2.1.0 OK", c.ask("MAIL FROM:<second@example.com>"))
	assert.Equal(t, "250 2.1.5 OK", c.ask("RCPT TO:<c@example.com>"))
	c.ask("DATA")
	c.say("second", ".")
	assert.Equal(t, "250 2.0.0 OK: queued", c.line())
	messages = server.WaitFor(t, 2)
	assert.Equal(t, smtptest.Message{From: "second@example.com", To: []string{"c@example.com"}, Data: "second\r\n"}, messages[1])
}

func TestServer_AMalformedLoginIsTakenAsAnEmptyOne(t *testing.T) {
	for name, command := range map[string]string{
		"not base64":           "AUTH PLAIN %%%",
		"too few parts":        "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("user")),
		"without a credential": "AUTH PLAIN",
		"another mechanism":    "AUTH LOGIN",
	} {
		t.Run(name, func(t *testing.T) {
			server := smtptest.Start(t)
			c := dial(t, server)
			c.line()

			assert.Equal(t, "235 2.7.0 Authentication successful", c.ask(command))
			c.ask("MAIL FROM:<a@example.com>")
			c.ask("RCPT TO:<b@example.com>")
			c.ask("DATA")
			c.say("x", ".")
			c.line()

			message := server.WaitFor(t, 1)[0]
			assert.True(t, message.Authenticated)
			assert.Empty(t, message.User)
			assert.Empty(t, message.Password)
		})
	}
}

func TestServer_AddressesAreReadWithOrWithoutAngleBrackets(t *testing.T) {
	for name, tc := range map[string]struct{ from, rcpt, wantFrom, wantTo string }{
		"in brackets":       {"MAIL FROM:<a@example.com>", "RCPT TO:<b@example.com>", "a@example.com", "b@example.com"},
		"with a space":      {"MAIL FROM: <a@example.com>", "RCPT TO: <b@example.com>", "a@example.com", "b@example.com"},
		"with a parameter":  {"MAIL FROM:<a@example.com> BODY=8BITMIME", "RCPT TO:<b@example.com> NOTIFY=NEVER", "a@example.com", "b@example.com"},
		"without brackets":  {"MAIL FROM:a@example.com", "RCPT TO:  b@example.com", "a@example.com", "b@example.com"},
		"with no sender":    {"MAIL FROM:<>", "RCPT TO:<b@example.com>", "", "b@example.com"},
		"with no colon":     {"MAIL a@example.com", "RCPT b@example.com", "", ""},
		"unclosed brackets": {"MAIL FROM:<a@example.com", "RCPT TO:<b@example.com", "<a@example.com", "<b@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			server := smtptest.Start(t)
			c := dial(t, server)
			c.line()

			c.ask(tc.from)
			c.ask(tc.rcpt)
			c.ask("DATA")
			c.say("x", ".")
			c.line()

			message := server.WaitFor(t, 1)[0]
			assert.Equal(t, tc.wantFrom, message.From)
			assert.Equal(t, []string{tc.wantTo}, message.To)
		})
	}
}

func TestServer_RSETForgetsTheEnvelopeAndLogin(t *testing.T) {
	server := smtptest.Start(t)
	c := dial(t, server)
	c.line()
	c.ask(plain("alice", "s3cret"))
	c.ask("MAIL FROM:<first@example.com>")
	c.ask("RCPT TO:<first-to@example.com>")

	assert.Equal(t, "250 2.0.0 OK", c.ask("RSET"))

	c.ask("MAIL FROM:<second@example.com>")
	c.ask("RCPT TO:<second-to@example.com>")
	c.ask("DATA")
	c.say("body", ".")
	c.line()
	assert.Equal(t, smtptest.Message{From: "second@example.com", To: []string{"second-to@example.com"}, Data: "body\r\n"}, server.WaitFor(t, 1)[0])
}

func TestServer_AnswersACommandItDoesNotKnow(t *testing.T) {
	c := dial(t, smtptest.Start(t))
	c.line()

	assert.Equal(t, "502 5.5.2 Command not recognized", c.ask("NOOP"))
	assert.Equal(t, "502 5.5.2 Command not recognized", c.ask("VRFY someone"))
	assert.Equal(t, "502 5.5.2 Command not recognized", c.ask(""))
	assert.Equal(t, "250 2.1.0 OK", c.ask("MAIL FROM:<a@example.com>"), "and goes on")
}

func TestServer_QuitSaysByeAndClosesTheConnection(t *testing.T) {
	c := dial(t, smtptest.Start(t))
	c.line()

	assert.Equal(t, "221 2.0.0 Bye", c.ask("QUIT"))

	require.NoError(t, c.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := c.reader.ReadString('\n')
	assert.Error(t, err, "the server hung up")
}

func TestServer_RejectionsAnswerInPlaceOfTheCommand(t *testing.T) {
	server := smtptest.Start(t)
	c := dial(t, server)
	c.line()
	c.ask("MAIL FROM:<a@example.com>")

	server.RejectRecipients("550 5.1.1 <b@example.com>: user unknown")
	assert.Equal(t, "550 5.1.1 <b@example.com>: user unknown", c.ask("RCPT TO:<b@example.com>"))
	server.RejectRecipients("")
	assert.Equal(t, "250 2.1.5 OK", c.ask("RCPT TO:<c@example.com>"))

	server.RejectMessages("554 no")
	assert.Equal(t, "554 no", c.ask("DATA"))
	server.RejectMessages("")
	assert.Equal(t, "354 End data with <CR><LF>.<CR><LF>", c.ask("DATA"))
	c.say("kept", ".")
	assert.Equal(t, "250 2.0.0 OK: queued", c.line())

	message := server.WaitFor(t, 1)[0]
	assert.Equal(t, []string{"c@example.com"}, message.To, "the rejected recipient is not on the message")
	assert.Empty(t, server.Messages()[1:], "and the rejected data left nothing")
}

func TestServer_AConnectionThatDropsMidDataLeavesNoMessage(t *testing.T) {
	server := smtptest.Start(t)
	c := dial(t, server)
	c.line()
	c.ask("MAIL FROM:<a@example.com>")
	c.ask("RCPT TO:<b@example.com>")
	c.ask("DATA")
	c.say("half a message")
	require.NoError(t, c.conn.Close())

	// A second client is served: the first one's handler ended
	other := dial(t, server)
	other.line()
	assert.Equal(t, "250 2.1.0 OK", other.ask("MAIL FROM:<a@example.com>"))
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, server.Messages())
}
