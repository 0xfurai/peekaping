package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"peekaping/internal/mailer"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Why an install email channel is refused or fails, in words that are safe to show to the user who
// set it up
var (
	ErrNoInstallSender       = errors.New("this install has no sender for account emails (ACCOUNT_SMTP_HOST is not set)")
	ErrRecipientNotMember    = errors.New("the recipient must be the email address of a member of the workspace")
	errNoWorkspaceForChannel = errors.New("the channel's workspace is not known")
)

// InstallEmailConfig is the config of an install email channel: the one recipient. Example JSON:
// {"to":"alice@example.com"}
type InstallEmailConfig struct {
	To string `json:"to" validate:"required,email,max=255"`
}

// AccountMailer is the sender of the install's account emails (mailer.Mailer is one)
type AccountMailer interface {
	Enabled() bool
	Send(ctx context.Context, to, subject, text, html string) error
}

// WorkspaceMembers answers whether an email address is a member's in a workspace. It ignores case
// (workspace.MemberRepository is one).
type WorkspaceMembers interface {
	HasMemberWithEmail(ctx context.Context, id workspace.ID, email string) (bool, error)
}

type workspaceKey struct{}

// WithWorkspace returns a context that names the workspace that owns the channel being validated or
// sent to. The providers take only the channel's config, which an install email channel can't be
// checked against alone: its recipient must be a member of the workspace that owns the channel.
func WithWorkspace(ctx context.Context, id workspace.ID) context.Context {
	return context.WithValue(ctx, workspaceKey{}, id)
}

func workspaceFrom(ctx context.Context) (workspace.ID, bool) {
	id, ok := ctx.Value(workspaceKey{}).(workspace.ID)
	return id, ok && id != ""
}

// InstallEmailSender sends a notification through the install's account-email sender to one member
// of the workspace. Only members may be the recipient so that a workspace of the cloud can't use
// the install's sender to mail strangers.
type InstallEmailSender struct {
	logger  *zap.SugaredLogger
	mailer  AccountMailer
	members WorkspaceMembers
}

// NewInstallEmailSender creates an InstallEmailSender
func NewInstallEmailSender(logger *zap.SugaredLogger, mail AccountMailer, members WorkspaceMembers) *InstallEmailSender {
	return &InstallEmailSender{logger: logger, mailer: mail, members: members}
}

// Unmarshal reads the config. The address is trimmed, since a pasted one often has a space.
func (e *InstallEmailSender) Unmarshal(configJSON string) (any, error) {
	cfg, err := GenericUnmarshal[InstallEmailConfig](configJSON)
	if err != nil {
		return nil, err
	}
	cfg.To = strings.TrimSpace(cfg.To)
	return cfg, nil
}

// Validate checks the shape of the config. It can't tell whether the recipient is a member, which
// needs the workspace: ValidateInWorkspace does.
func (e *InstallEmailSender) Validate(configJSON string) error {
	cfg, err := e.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*InstallEmailConfig))
}

// ValidateInWorkspace is Validate for a channel of the workspace in ctx (see WithWorkspace). It also
// refuses a config on an install with no sender, and a recipient who is not a member of the
// workspace.
func (e *InstallEmailSender) ValidateInWorkspace(ctx context.Context, configJSON string) error {
	if err := e.Validate(configJSON); err != nil {
		return err
	}
	if !e.mailer.Enabled() {
		return ErrNoInstallSender
	}
	_, err := e.recipient(ctx, configJSON)
	return err
}

// recipient returns the address to send to: the member's email as the workspace knows it. It
// returns ErrRecipientNotMember when the config's address is no member's.
func (e *InstallEmailSender) recipient(ctx context.Context, configJSON string) (string, error) {
	id, ok := workspaceFrom(ctx)
	if !ok {
		return "", errNoWorkspaceForChannel
	}
	parsed, err := e.Unmarshal(configJSON)
	if err != nil {
		return "", err
	}
	to := parsed.(*InstallEmailConfig).To
	if e.members == nil {
		return "", ErrRecipientNotMember
	}
	member, err := e.members.HasMemberWithEmail(ctx, id, to)
	if err != nil {
		return "", fmt.Errorf("failed to check the recipient: %w", err)
	}
	if !member {
		return "", ErrRecipientNotMember
	}
	return to, nil
}

// Send emails the notification. The recipient is checked again here, since they may have left the
// workspace since the channel was saved: then nothing is sent and the error says so.
func (e *InstallEmailSender) Send(
	ctx context.Context,
	configJSON string,
	message string,
	m *monitor.Model,
	heartbeat *heartbeat.Model,
) error {
	if err := e.Validate(configJSON); err != nil {
		return err
	}
	if !e.mailer.Enabled() {
		return ErrNoInstallSender
	}
	to, err := e.recipient(ctx, configJSON)
	if err != nil {
		return err
	}
	// The subject and body are those of an SMTP channel without custom templates
	if err := e.mailer.Send(ctx, to, defaultEmailSubject, message, ""); err != nil {
		// The server's own text repeats the recipient's address
		return fmt.Errorf("failed to send the email: %s", mailer.Describe(err))
	}
	e.logger.Infof("Install email notification sent successfully")
	return nil
}
