package mailer_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"syscall"
	"testing"
	"time"

	"peekaping/internal/config"
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
	assert.Equal(t, "send failed: syscall.Errno", mailer.Describe(&net.OpError{Op: "dial", Addr: &net.TCPAddr{}, Err: syscall.ECONNREFUSED}))
	got := mailer.Describe(errors.New("invalid recipient alice@example.com"))
	assert.Equal(t, "send failed: *errors.errorString", got)
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
