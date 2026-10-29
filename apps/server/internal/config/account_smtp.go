package config

import (
	"fmt"
	"net/mail"
	"strings"
)

// The ways to secure the connection to the account emails' SMTP server (ACCOUNT_SMTP_TLS)
const (
	AccountSMTPStartTLS = "starttls"
	AccountSMTPTLS      = "tls"
	AccountSMTPNone     = "none"
)

// AccountEmailsEnabled is whether the install has a sender for account emails: password reset,
// email verification and invites. ACCOUNT_SMTP_HOST turns them on, and with it unset the app
// asks for no email and offers nothing that needs one.
func (c *Config) AccountEmailsEnabled() bool {
	return strings.TrimSpace(c.AccountSMTPHost) != ""
}

// ValidateAccountSMTP checks the account emails' settings together, for the binaries that read
// them. A host without a sender address is refused: mail with no From is rejected by most
// servers, and finding out at the first password reset is too late.
func ValidateAccountSMTP(host, from, tlsMode string) error {
	switch tlsMode {
	case AccountSMTPStartTLS, AccountSMTPTLS, AccountSMTPNone:
	default:
		return fmt.Errorf("ACCOUNT_SMTP_TLS must be one of: %s, %s, %s", AccountSMTPStartTLS, AccountSMTPTLS, AccountSMTPNone)
	}
	if strings.TrimSpace(host) == "" {
		return nil
	}
	if strings.TrimSpace(from) == "" {
		return fmt.Errorf("ACCOUNT_SMTP_FROM is required when ACCOUNT_SMTP_HOST is set: it is the address account emails are sent from, such as no-reply@example.com")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("ACCOUNT_SMTP_FROM must be an email address such as no-reply@example.com or Peekaping <no-reply@example.com>: %w", err)
	}
	return nil
}
