// Package smtpclient sends one message through an SMTP server. The notification channels' email
// provider and the account emails (password reset, verification, invites) share it.
package smtpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
)

// TLSMode is how the connection to the server is secured
type TLSMode string

const (
	// Opportunistic upgrades with STARTTLS when the server offers it and goes on in the clear
	// when it doesn't. It is what the notification channels have always done.
	Opportunistic TLSMode = "opportunistic"
	// StartTLS upgrades the connection with STARTTLS and fails when the server doesn't offer it
	StartTLS TLSMode = "starttls"
	// TLS wraps the connection in TLS from the first byte (SMTPS, usually port 465)
	TLS TLSMode = "tls"
	// None never uses TLS. It suits a relay on the same host or network.
	None TLSMode = "none"
)

// Server is where to send to, and how
type Server struct {
	Host string
	Port int
	// Auth is nil for a server that needs no login
	Auth smtp.Auth
	// TLS defaults to Opportunistic
	TLS TLSMode
	// Dialer makes the connection. It defaults to a net.Dialer with no settings; the notification
	// channels' sender gives one that dials through the netguard (PP-158), and the account emails'
	// sender, which is the install's own setting, leaves it out.
	Dialer *net.Dialer
}

// Send delivers msg, the whole message with its headers, from the envelope sender to the
// recipients, and stops when ctx is done. smtp.SendMail has no context or timeout, so a server
// that never answers would hold the send forever.
func Send(ctx context.Context, server Server, from string, to []string, msg []byte) error {
	addr := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
	dialer := server.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	var conn net.Conn
	var err error
	if server.TLS == TLS {
		tlsDialer := tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: server.Host}}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	// Closing the connection unblocks the SMTP client once ctx is done
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	err = sendOn(conn, server, from, to, msg)
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("failed to send email: %w", ctx.Err())
	}
	return err
}

func sendOn(conn net.Conn, server Server, from string, to []string, msg []byte) error {
	c, err := smtp.NewClient(conn, server.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()

	if err := c.Hello("localhost"); err != nil {
		return err
	}
	switch server.TLS {
	case StartTLS:
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server doesn't support STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{ServerName: server.Host}); err != nil {
			return err
		}
	case TLS, None:
	default:
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: server.Host}); err != nil {
				return err
			}
		}
	}
	if server.Auth != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err := c.Auth(server.Auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
