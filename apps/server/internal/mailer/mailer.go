// Package mailer sends the emails an account gets: a password reset link, an email verification
// link and a workspace invite. They all go out from one sender for the whole install, set by the
// ACCOUNT_SMTP_* settings. Without ACCOUNT_SMTP_HOST the mailer is off (Enabled is false) and the
// app offers nothing that needs email.
//
// The SMTP notification channels have SMTP settings of their own, per channel. They share the client
// (internal/smtpclient) and nothing else with this. The install email channel is the exception: it
// sends through this sender, to a member of its workspace.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/smtpclient"

	"go.uber.org/zap"
)

// ErrDisabled is what Send returns when the install has no sender
var ErrDisabled = errors.New("account emails are off: ACCOUNT_SMTP_HOST is not set")

// deliverTimeout is how long a background send may take before it is given up on
const deliverTimeout = 30 * time.Second

// sendFunc is smtpclient.Send; tests replace it
type sendFunc func(ctx context.Context, server smtpclient.Server, from string, to []string, msg []byte) error

type Mailer struct {
	server smtpclient.Server
	// from is the sender; empty when the mailer is off
	from   *mail.Address
	send   sendFunc
	logger *zap.SugaredLogger
	now    func() time.Time

	pending sync.WaitGroup
	running atomic.Int64
}

// New builds the mailer the config describes. The config was validated at startup
// (config.ValidateAccountSMTP), so a sender that does not parse leaves the mailer off, and says
// so in the log.
func New(cfg *config.Config, logger *zap.SugaredLogger) *Mailer {
	m := &Mailer{send: smtpclient.Send, logger: logger.Named("[mailer]"), now: time.Now}
	if !cfg.AccountEmailsEnabled() {
		return m
	}
	from, err := mail.ParseAddress(cfg.AccountSMTPFrom)
	if err != nil {
		m.logger.Errorw("ACCOUNT_SMTP_FROM is not an email address: account emails stay off", "error", err)
		return m
	}
	var auth smtp.Auth
	if cfg.AccountSMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.AccountSMTPUser, cfg.AccountSMTPPassword, cfg.AccountSMTPHost)
	}
	mode := smtpclient.TLSMode(cfg.AccountSMTPTLS)
	if mode == "" {
		mode = smtpclient.StartTLS
	}
	m.server = smtpclient.Server{Host: strings.TrimSpace(cfg.AccountSMTPHost), Port: cfg.AccountSMTPPort, Auth: auth, TLS: mode}
	m.from = from
	return m
}

// Enabled is whether the install has a sender. When it is not, Send and Deliver do nothing.
func (m *Mailer) Enabled() bool { return m != nil && m.from != nil }

// Send emails one recipient and waits for the server to take the message. text is the plain
// message and html, when it is not empty, an alternative to it. It returns ErrDisabled without a
// sender. A recipient or subject with a line break is refused: it would add headers.
func (m *Mailer) Send(ctx context.Context, to, subject, text, html string) error {
	if !m.Enabled() {
		return ErrDisabled
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return errors.New("the subject has a line break")
	}
	msg, err := m.build(recipient, subject, text, html)
	if err != nil {
		return err
	}
	return m.send(ctx, m.server, m.from.Address, []string{recipient.Address}, msg)
}

// Deliver sends in the background and logs a failure; it never fails the caller. A request that
// sends an account email answers at once, so that how long the SMTP server takes does not tell a
// visitor whether the address has an account. Wait waits for the sends that are running.
func (m *Mailer) Deliver(to, subject, text, html string) {
	m.Background(func(ctx context.Context) {
		if err := m.Send(ctx, to, subject, text, html); err != nil {
			// The recipient is left out of the log: it is somebody's address. So is the server's text
			// (see Describe)
			m.logger.Errorw("Failed to send an account email", "subject", subject, "reason", Describe(err))
		}
	})
}

// Describe says why a Send failed in a form that is safe to log: the SMTP reply code, or the class
// of the failure (a host name that does not resolve, a certificate that does not verify, the network
// operation and the OS error, a timeout), or else the type of the error. It never carries the text of
// the error, because an SMTP server echoes the recipient's address in a rejection
// ("550 5.1.1 <alice@example.com>: user unknown") and net/smtp returns that text. A network error's
// text names the server's address too.
func Describe(err error) string {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return fmt.Sprintf("the server replied %d", reply.Code)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "send failed: the host name could not be resolved (not found)"
		case dnsErr.IsTimeout:
			return "send failed: the host name could not be resolved (timeout)"
		}
		return "send failed: the host name could not be resolved"
	}
	if certificateUnverified(err) {
		return "send failed: the certificate could not be verified"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op != "" {
		// Op is a fixed word of the net package ("dial", "read", "write"), and an errno's text is the
		// OS's own ("connection refused"); neither names the server
		var errno syscall.Errno
		switch {
		case errors.As(opErr.Err, &errno):
			return fmt.Sprintf("send failed: %s: %s", opErr.Op, errno.Error())
		case opErr.Timeout():
			return fmt.Sprintf("send failed: %s: timed out", opErr.Op)
		}
		return fmt.Sprintf("send failed: %s: %T", opErr.Op, leaf(opErr))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "send failed: timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "send failed: cancelled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "send failed: timed out"
	}
	return fmt.Sprintf("send failed: %T", leaf(err))
}

// certificateUnverified is whether err is a TLS certificate that failed verification
func certificateUnverified(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) || errors.As(err, &invalid)
}

// leaf is the deepest error err wraps
func leaf(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// Background runs fn in its own goroutine with a context that ends after deliverTimeout, and Wait
// waits for it. It does nothing without a sender. A panic in fn is logged and goes no further.
func (m *Mailer) Background(fn func(ctx context.Context)) {
	if !m.Enabled() {
		return
	}
	m.pending.Add(1)
	m.running.Add(1)
	go func() {
		defer m.pending.Done()
		defer m.running.Add(-1)
		defer func() {
			if r := recover(); r != nil {
				m.logger.Errorw("An account email job panicked", "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
		defer cancel()
		fn(ctx)
	}()
}

// Wait returns once the jobs that Deliver and Background started have finished. Tests use it, and so can a
// shutdown that wants to let the last mails out.
func (m *Mailer) Wait() { m.pending.Wait() }

// WaitFor is Wait with a bound: it returns once the jobs have finished or after timeout, and says
// how many are still running (0 when they all finished). The jobs are not stopped; the caller is
// about to exit. A shutdown uses it to let the last mails out without waiting on a slow server.
func (m *Mailer) WaitFor(timeout time.Duration) (stillRunning int) {
	done := make(chan struct{})
	go func() {
		m.pending.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return 0
	case <-timer.C:
		return int(m.running.Load())
	}
}

func (m *Mailer) build(to *mail.Address, subject, text, html string) ([]byte, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	domain := "localhost"
	if _, after, ok := strings.Cut(m.from.Address, "@"); ok && after != "" {
		domain = after
	}
	var out bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&out, "%s: %s\r\n", name, value) }
	header("From", m.from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", m.now().UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+domain+">")
	header("MIME-Version", "1.0")
	// Mail that nobody is meant to answer, and that auto-replies should leave alone
	header("Auto-Submitted", "auto-generated")

	if html == "" {
		header("Content-Type", `text/plain; charset="utf-8"`)
		header("Content-Transfer-Encoding", "quoted-printable")
		out.WriteString("\r\n")
		if err := writeQuotedPrintable(&out, text); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}

	parts := multipart.NewWriter(&out)
	header("Content-Type", `multipart/alternative; boundary="`+parts.Boundary()+`"`)
	out.WriteString("\r\n")
	for _, part := range []struct{ contentType, body string }{
		{`text/plain; charset="utf-8"`, text},
		{`text/html; charset="utf-8"`, html},
	} {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeQuotedPrintable(w, part.body); err != nil {
			return nil, err
		}
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeQuotedPrintable(w interface{ Write([]byte) (int, error) }, body string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}
