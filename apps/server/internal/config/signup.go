package config

import (
	"fmt"
	"strings"
)

// The signup modes (SIGNUP_MODE). Docs: GLOSSARY.md, Signup mode.
const (
	SignupModeInvite = "invite"
	SignupModeOpen   = "open"
)

// SignupOpen is whether anyone may sign up and get a workspace of their own. Unset counts as invite.
func (c *Config) SignupOpen() bool { return c.SignupMode == SignupModeOpen }

// SignupTurnstileEnabled is whether open signups must pass Cloudflare Turnstile
func (c *Config) SignupTurnstileEnabled() bool {
	return c.SignupTurnstileSiteKey != "" && c.SignupTurnstileSecret != ""
}

// ValidateSignup checks the signup settings together, for the binaries that read them. Open signup
// needs a sender for account emails, since each new account must verify its address before its
// monitors run; a mistyped mode or half a Turnstile setting stops startup, not the first signup.
func ValidateSignup(mode, turnstileSiteKey, turnstileSecret, accountSMTPHost string) error {
	switch mode {
	case "", SignupModeInvite:
	case SignupModeOpen:
		if strings.TrimSpace(accountSMTPHost) == "" {
			return fmt.Errorf("SIGNUP_MODE=open needs a sender for account emails: set ACCOUNT_SMTP_HOST and ACCOUNT_SMTP_FROM, because new accounts must verify their email address")
		}
	default:
		return fmt.Errorf("SIGNUP_MODE must be %s or %s, got %q", SignupModeInvite, SignupModeOpen, mode)
	}
	if (turnstileSiteKey == "") != (turnstileSecret == "") {
		return fmt.Errorf("SIGNUP_TURNSTILE_SITE_KEY and SIGNUP_TURNSTILE_SECRET must be set together, or both left unset")
	}
	return nil
}
