// Package mailer sends the emails an account gets: a password reset link, an email verification
// link and a workspace invite. They all go out from one sender for the whole install, set by the
// ACCOUNT_SMTP_* settings. Without ACCOUNT_SMTP_HOST the mailer is off (Enabled is false) and the
// app offers nothing that needs email.
//
// The notification channels have SMTP settings of their own, per channel. They share the client
// (internal/smtpclient) and nothing else with this.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
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
			// The recipient is left out of the log: it is somebody's address
			m.logger.Errorw("Failed to send an account email", "subject", subject, "error", err)
		}
	})
}

// Background runs fn in its own goroutine with a context that ends after deliverTimeout, and Wait
// waits for it. It does nothing without a sender. A panic in fn is logged and goes no further.
func (m *Mailer) Background(fn func(ctx context.Context)) {
	if !m.Enabled() {
		return
	}
	m.pending.Add(1)
	go func() {
		defer m.pending.Done()
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
