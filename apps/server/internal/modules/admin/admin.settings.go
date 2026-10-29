package admin

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/auth"

	"go.uber.org/zap"
)

var (
	// ErrNoSender is a test email on an install with no sender for account emails. It answers 400.
	ErrNoSender = errors.New("the install has no sender for account emails")
	// ErrInvalidRecipient is a test email to something that is not an email address. It answers 400.
	ErrInvalidRecipient = errors.New("invalid recipient")
)

// InstallSettings is what an instance admin does to the install settings (docs/adr/0005 section 5):
// the signup mode, the sender of account emails, the public URL and the default limits of a
// workspace. A setting the server's environment sets is locked. A change is logged with who made it and the settings it changed,
// never a value.
type InstallSettings struct {
	settings *installsettings.Resolver
	mailer   *mailer.Mailer
	signup   *auth.SignupGate
	logger   *zap.SugaredLogger
}

func NewInstallSettings(settings *installsettings.Resolver, mailer *mailer.Mailer, signup *auth.SignupGate, logger *zap.SugaredLogger) *InstallSettings {
	return &InstallSettings{settings: settings, mailer: mailer, signup: signup, logger: logger.Named("[admin-settings]")}
}

// Describe is every install setting with where its value comes from
func (s *InstallSettings) Describe(ctx context.Context) (*installsettings.View, error) {
	return s.settings.Describe(ctx)
}

// Update saves the change and returns the settings as they resolve after it. A refusal is an
// *installsettings.Error, and stores nothing.
func (s *InstallSettings) Update(ctx context.Context, actorID string, change installsettings.Change) (*installsettings.View, error) {
	changed, err := s.settings.Update(ctx, change)
	if err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		s.logger.Infow("Admin action", "actorUserId", actorID, "action", "update_install_settings", "settings", changed)
		if change.SignupMode != nil {
			s.signup.WarnIfProxiesUntrusted(ctx)
		}
	}
	return s.settings.Describe(ctx)
}

// SendTestEmail sends a fixed message to the address through the install's sender now, and waits
// for the server to take it: ErrInvalidRecipient, ErrNoSender, or the send's error, which
// mailer.Describe makes safe to show. The address is not logged.
func (s *InstallSettings) SendTestEmail(ctx context.Context, actorID, to string) error {
	if strings.ContainsAny(to, "\r\n") {
		return ErrInvalidRecipient
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return ErrInvalidRecipient
	}
	if !s.mailer.Enabled() {
		return ErrNoSender
	}
	message := mailer.TestEmail(strings.TrimRight(s.settings.PublicURL(ctx), "/"))
	if err := s.mailer.Send(ctx, to, message.Subject, message.Text, message.HTML); err != nil {
		if errors.Is(err, mailer.ErrDisabled) {
			return ErrNoSender
		}
		s.logger.Warnw("A test email failed", "actorUserId", actorID, "reason", mailer.Describe(err))
		return err
	}
	s.logger.Infow("Admin action", "actorUserId", actorID, "action", "send_test_email")
	return nil
}
