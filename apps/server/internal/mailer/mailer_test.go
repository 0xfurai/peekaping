package mailer_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func newMailer(t *testing.T, server *smtptest.Server) *mailer.Mailer {
	t.Helper()
	return mailer.New(&config.Config{
		AccountSMTPHost: server.Host(), AccountSMTPPort: server.Port(), AccountSMTPUser: "mailer", AccountSMTPPassword: "secret",
		AccountSMTPFrom: "Peekaping <no-reply@example.com>", AccountSMTPTLS: "none",
	}, zap.NewNop().Sugar())
}

func TestMailer_IsOffWithoutAHost(t *testing.T) {
	m := mailer.New(&config.Config{}, zap.NewNop().Sugar())

	assert.False(t, m.Enabled())
	assert.ErrorIs(t, m.Send(context.Background(), "a@example.com", "Hi", "Hello", ""), mailer.ErrDisabled)
	assert.NotPanics(t, func() { m.Deliver("a@example.com", "Hi", "Hello", ""); m.Wait() })
}

func TestMailer_SendsAMultipartMessageWithTheSenderAndRecipient(t *testing.T) {
	server := smtptest.Start(t)
	m := newMailer(t, server)
	require.True(t, m.Enabled())

	require.NoError(t, m.Send(context.Background(), "alice@example.com", "Reset your password", "Plain text with https://peek.example/reset/pkr_abc", "<p>HTML</p>"))

	sent := server.WaitFor(t, 1)[0]
	assert.Equal(t, "no-reply@example.com", sent.From)
	assert.Equal(t, []string{"alice@example.com"}, sent.To)
	assert.True(t, sent.Authenticated)

	parsed, err := mail.ReadMessage(strings.NewReader(sent.Data))
	require.NoError(t, err)
	assert.Equal(t, `"Peekaping" <no-reply@example.com>`, parsed.Header.Get("From"))
	assert.Equal(t, "<alice@example.com>", parsed.Header.Get("To"))
	assert.Equal(t, "Reset your password", parsed.Header.Get("Subject"))
	assert.NotEmpty(t, parsed.Header.Get("Date"))
	assert.Contains(t, parsed.Header.Get("Message-ID"), "@example.com>")
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/alternative", mediaType)
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var bodies []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(quotedprintable.NewReader(part))
		require.NoError(t, err)
		bodies = append(bodies, string(body))
	}
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[0], "https://peek.example/reset/pkr_abc")
	assert.Equal(t, "<p>HTML</p>", strings.TrimSpace(bodies[1]))
}

func TestMailer_SendsPlainTextWhenThereIsNoHTML(t *testing.T) {
	server := smtptest.Start(t)

	require.NoError(t, newMailer(t, server).Send(context.Background(), "alice@example.com", "Hi", "Just text", ""))

	sent := server.WaitFor(t, 1)[0]
	assert.Contains(t, sent.Data, `Content-Type: text/plain; charset="utf-8"`)
	assert.Contains(t, sent.Data, "Just text")
}

func TestMailer_EncodesANonASCIISubject(t *testing.T) {
	server := smtptest.Start(t)

	require.NoError(t, newMailer(t, server).Send(context.Background(), "alice@example.com", "Invitación a Peekaping", "Hola", ""))

	parsed, err := mail.ReadMessage(strings.NewReader(server.WaitFor(t, 1)[0].Data))
	require.NoError(t, err)
	decoded, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Invitación a Peekaping", decoded)
}

func TestMailer_RefusesALineBreakInTheRecipientOrSubject(t *testing.T) {
	server := smtptest.Start(t)
	m := newMailer(t, server)

	assert.Error(t, m.Send(context.Background(), "alice@example.com\r\nBcc: victim@example.com", "Hi", "Hello", ""))
	assert.Error(t, m.Send(context.Background(), "alice@example.com", "Hi\r\nBcc: victim@example.com", "Hello", ""))
	assert.Empty(t, server.Messages())
}

func TestMailer_DeliverSendsInTheBackgroundAndWaitWaits(t *testing.T) {
	server := smtptest.Start(t)
	m := newMailer(t, server)

	m.Deliver("alice@example.com", "Hi", "Hello", "")
	m.Wait()

	assert.Len(t, server.Messages(), 1)
}

func TestMailer_DeliverSurvivesAServerThatRefusesTheMessage(t *testing.T) {
	server := smtptest.Start(t)
	server.RejectMessages("554 5.7.1 no")
	m := newMailer(t, server)

	assert.NotPanics(t, func() { m.Deliver("alice@example.com", "Hi", "Hello", ""); m.Wait() })
	assert.Empty(t, server.Messages())
}

func TestMailer_DeliverLogsNoRecipientWhenTheServerRejectsItAndEchoesTheAddress(t *testing.T) {
	server := smtptest.Start(t)
	server.RejectRecipients("550 5.1.1 <alice@example.com>: Recipient address rejected: User unknown")
	core, logs := observer.New(zap.ErrorLevel)
	m := mailer.New(&config.Config{
		AccountSMTPHost: server.Host(), AccountSMTPPort: server.Port(), AccountSMTPFrom: "Peekaping <no-reply@example.com>", AccountSMTPTLS: "none",
	}, zap.New(core).Sugar())

	m.Deliver("alice@example.com", "Hi", "Hello", "")
	m.Wait()

	entries := logs.All()
	require.Len(t, entries, 1)
	line := fmt.Sprintf("%s %v", entries[0].Message, entries[0].ContextMap())
	assert.NotContains(t, line, "alice")
	assert.NotContains(t, line, "example.com")
	assert.NotContains(t, line, "Recipient address rejected")
	assert.Contains(t, line, "550", "the reply code is what the log keeps")
}

func TestDescribe_KeepsTheReplyCodeOrTheTypeAndNeverTheText(t *testing.T) {
	assert.Equal(t, "the server replied 550", mailer.Describe(fmt.Errorf("wrapped: %w", &textproto.Error{Code: 550, Msg: "<alice@example.com> unknown"})))
	assert.Equal(t, "send failed: timed out", mailer.Describe(fmt.Errorf("failed to send email: %w", context.DeadlineExceeded)))
	got := mailer.Describe(errors.New("invalid recipient alice@example.com"))
	assert.Equal(t, "send failed: *errors.errorString", got)
}

func TestDescribe_NamesTheFailureClassWithoutTheServerTextOrAnAddress(t *testing.T) {
	server := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 587}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a refused dial", &net.OpError{Op: "dial", Net: "tcp", Addr: server, Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, "send failed: dial: connection refused"},
		{"a reset read", fmt.Errorf("smtp: %w", &net.OpError{Op: "read", Net: "tcp", Addr: server, Err: syscall.ECONNRESET}), "send failed: read: connection reset by peer"},
		{"a read that timed out", &net.OpError{Op: "read", Net: "tcp", Addr: server, Err: os.ErrDeadlineExceeded}, "send failed: read: timed out"},
		{"a host that does not exist", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "smtp.example.com", Server: "192.0.2.53:53", IsNotFound: true}}, "send failed: the host name could not be resolved (not found)"},
		{"a lookup that timed out", &net.DNSError{Err: "i/o timeout", Name: "smtp.example.com", Server: "192.0.2.53:53", IsTimeout: true}, "send failed: the host name could not be resolved (timeout)"},
		{"a lookup that failed otherwise", &net.DNSError{Err: "server misbehaving", Name: "smtp.example.com", Server: "192.0.2.53:53"}, "send failed: the host name could not be resolved"},
		{"a timeout outside a connection", fmt.Errorf("smtp: %w", os.ErrDeadlineExceeded), "send failed: timed out"},
		{"a certificate from an unknown authority", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "send failed: the certificate could not be verified"},
		{"a certificate for another host", fmt.Errorf("starttls: %w", x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"mail.example.com"}}, Host: "192.0.2.10"}), "send failed: the certificate could not be verified"},
		{"a reply", &textproto.Error{Code: 550, Msg: "5.1.1 <alice@example.com>: user unknown"}, "the server replied 550"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mailer.Describe(tc.err)
			assert.Equal(t, tc.want, got)
			for _, secret := range []string{"192.0.2", "example.com", "alice", "587", ":53"} {
				assert.NotContains(t, got, secret)
			}
		})
	}
}

func TestMessages_NameTheLinkAndItsLifetime(t *testing.T) {
	link := "https://peek.example/reset-password/pkr_abc"

	reset := mailer.ResetPassword(link, time.Hour)
	assert.Contains(t, reset.Text, link)
	assert.Contains(t, reset.Text, "expires in 1 hour")
	assert.Contains(t, reset.HTML, `href="`+link+`"`)

	verify := mailer.VerifyEmail("https://peek.example/verify-email/pkv_abc", 72*time.Hour)
	assert.Contains(t, verify.Text, "expires in 3 days")

	invite := mailer.Invite("Acme <b>", "ann@example.com", "admin", "https://peek.example/invite/pki_abc", 7*24*time.Hour)
	assert.Contains(t, invite.Subject, "Acme <b>")
	assert.Contains(t, invite.Text, "ann@example.com invited you")
	assert.Contains(t, invite.Text, "as an admin")
	assert.Contains(t, invite.Text, "expires in 7 days")
	assert.NotContains(t, invite.HTML, "<b>", "names are escaped in the HTML")
}

func TestMailer_WaitForGivesUpAfterTheBoundAndSaysHowManySendsAreStillRunning(t *testing.T) {
	m := newMailer(t, smtptest.Start(t))
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	for range 2 {
		m.Background(func(context.Context) { <-release; finished <- struct{}{} })
	}

	started := time.Now()
	stillRunning := m.WaitFor(50 * time.Millisecond)

	assert.Equal(t, 2, stillRunning)
	assert.Less(t, time.Since(started), 5*time.Second)

	close(release)
	<-finished
	<-finished
	assert.Zero(t, m.WaitFor(5*time.Second), "once the sends return, nothing is left")
}

func TestMailer_WaitForReturnsAtOnceWhenNothingIsSending(t *testing.T) {
	assert.Zero(t, mailer.New(&config.Config{}, zap.NewNop().Sugar()).WaitFor(time.Hour))
	assert.Zero(t, newMailer(t, smtptest.Start(t)).WaitFor(time.Hour))
}

// switchableSender is an install's sender that an instance admin changes while the app runs
type switchableSender struct {
	mu     sync.Mutex
	sender *installsettings.AccountSMTP
}

func (s *switchableSender) AccountSMTP(context.Context) *installsettings.AccountSMTP {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sender
}

func (s *switchableSender) set(sender *installsettings.AccountSMTP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sender = sender
}

func TestMailer_EachSendUsesTheSenderOfTheMoment(t *testing.T) {
	first, second := smtptest.Start(t), smtptest.Start(t)
	settings := &switchableSender{}
	m := mailer.NewFrom(settings, zap.NewNop().Sugar())
	assert.False(t, m.Enabled())
	assert.ErrorIs(t, m.Send(context.Background(), "a@example.com", "Hi", "Hello", ""), mailer.ErrDisabled)

	settings.set(&installsettings.AccountSMTP{Host: first.Host(), Port: first.Port(), From: "first@example.com", TLS: "none"})
	require.True(t, m.Enabled())
	require.NoError(t, m.Send(context.Background(), "a@example.com", "Hi", "Hello", ""))
	settings.set(&installsettings.AccountSMTP{Host: second.Host(), Port: second.Port(), From: "second@example.com", TLS: "none"})
	require.NoError(t, m.Send(context.Background(), "a@example.com", "Hi", "Hello", ""))

	assert.Equal(t, "first@example.com", first.WaitFor(t, 1)[0].From)
	assert.Equal(t, "second@example.com", second.WaitFor(t, 1)[0].From)

	settings.set(nil)
	assert.False(t, m.Enabled(), "a sender that was removed turns the mails off")
}

func TestMailer_ASenderWhoseFromDoesNotParseIsOff(t *testing.T) {
	m := mailer.NewFrom(&switchableSender{sender: &installsettings.AccountSMTP{Host: "127.0.0.1", Port: 25, From: "not an address"}}, zap.NewNop().Sugar())

	assert.False(t, m.Enabled())
}
