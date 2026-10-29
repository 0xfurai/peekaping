package providers

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"

	"peekaping/internal/mailer"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Why an install email channel is refused or fails, in words that are safe to show to the user who
// set it up
var (
	ErrNoInstallSender       = errors.New("this install has no sender for account emails (ACCOUNT_SMTP_HOST is not set)")
	ErrRecipientNotMember    = errors.New("the recipient must be the email address of a member of the workspace")
	ErrRecipientNotVerified  = errors.New("the recipient has not verified their email address yet")
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

// WorkspaceMembers answers whether an email address is a member's in a workspace, and whether that
// member has verified it. It ignores case (workspace.MemberRepository is one).
type WorkspaceMembers interface {
	HasMemberWithEmail(ctx context.Context, id workspace.ID, email string) (bool, error)
	HasVerifiedMemberWithEmail(ctx context.Context, id workspace.ID, email string) (bool, error)
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
// the install's sender to mail strangers, and a send needs the member to have verified the address:
// on an open-signup install anyone can sign up with another person's address and join a workspace
// of their own with it, so a member's address alone doesn't show that it is theirs.
type InstallEmailSender struct {
	logger    *zap.SugaredLogger
	publicURL PublicURLSource
	mailer    AccountMailer
	members   WorkspaceMembers
}

// NewInstallEmailSender creates an InstallEmailSender. The install's public URL at the moment of a
// send is what the email links to the monitor's page with.
func NewInstallEmailSender(logger *zap.SugaredLogger, publicURL PublicURLSource, mail AccountMailer, members WorkspaceMembers) *InstallEmailSender {
	return &InstallEmailSender{logger: logger, publicURL: publicURL, mailer: mail, members: members}
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
// workspace. A member who has not verified the address is accepted: a cloud signup sets the channel
// up before verifying, while its workspace is suspended and nothing runs. Send is what needs the
// verified address.
func (e *InstallEmailSender) ValidateInWorkspace(ctx context.Context, configJSON string) error {
	if err := e.Validate(configJSON); err != nil {
		return err
	}
	if !e.mailer.Enabled() {
		return ErrNoInstallSender
	}
	_, err := e.recipient(ctx, configJSON, false)
	return err
}

// recipient returns the address to send to: the config's address as it is saved, once it is a
// member's own email, compared ignoring case. It returns ErrRecipientNotMember when the config's
// address is no member's. With verified it also returns ErrRecipientNotVerified for a member who
// has not verified the address.
func (e *InstallEmailSender) recipient(ctx context.Context, configJSON string, verified bool) (string, error) {
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
	if verified {
		ok, err := e.members.HasVerifiedMemberWithEmail(ctx, id, to)
		if err != nil {
			return "", fmt.Errorf("failed to check the recipient: %w", err)
		}
		if ok {
			return to, nil
		}
	}
	member, err := e.members.HasMemberWithEmail(ctx, id, to)
	if err != nil {
		return "", fmt.Errorf("failed to check the recipient: %w", err)
	}
	if !member {
		return "", ErrRecipientNotMember
	}
	if verified {
		// A member, so the address has not been verified
		return "", ErrRecipientNotVerified
	}
	return to, nil
}

// Send emails the notification. The recipient is checked again here, since they may have left the
// workspace since the channel was saved: then nothing is sent and the error says so. A member who
// has not verified the address gets nothing either (ErrRecipientNotVerified).
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
	to, err := e.recipient(ctx, configJSON, true)
	if err != nil {
		return err
	}
	workspaceID, _ := workspaceFrom(ctx)
	email := installEmail(e.monitorLink(ctx, workspaceID, m), message, m, heartbeat)
	if err := e.mailer.Send(ctx, to, email.Subject, email.Text, email.HTML); err != nil {
		// The server's own text repeats the recipient's address
		return fmt.Errorf("failed to send the email: %s", mailer.Describe(err))
	}
	e.logger.Infof("Install email notification sent successfully")
	return nil
}

// monitorLink is the address of the monitor's page in its workspace, or "" when the install's
// public URL is not set or there is no page: a probe's stand-in monitor, or a test notification's
// monitor, which isn't saved
func (e *InstallEmailSender) monitorLink(ctx context.Context, workspaceID workspace.ID, m *monitor.Model) string {
	if m == nil || m.ID == "" || m.IsProbeEvent() {
		return ""
	}
	if m.WorkspaceID != "" {
		workspaceID = m.WorkspaceID
	}
	base := strings.TrimRight(publicURL(ctx, e.publicURL), "/")
	if base == "" || workspaceID == "" {
		return ""
	}
	return base + "/w/" + url.PathEscape(string(workspaceID)) + "/monitors/" + url.PathEscape(m.ID)
}

// installEmail is the email of a notification. The subject names the monitor and its status, as
// the heartbeat carries it ("Public API is Down"), and the body lists the monitor, the status, the
// time and the message, with a link to the monitor's page when there is one. A probe's notification
// says whether the probe went offline or came back online. A notification with no heartbeat (a
// certificate expiry) names the monitor only, and one with no monitor has the plain subject of an
// SMTP channel.
func installEmail(link, message string, m *monitor.Model, hb *heartbeat.Model) mailer.Message {
	subject := defaultEmailSubject
	var fields [][2]string
	if m != nil {
		label := "Monitor"
		if m.IsProbeEvent() {
			label = "Probe"
		}
		fields = append(fields, [2]string{label, m.Name})
		status := ""
		switch {
		case hb != nil:
			status = statusName(hb.Status)
		case m.IsProbeEvent() && m.Status == shared.MonitorStatusDown:
			status = "offline"
		case m.IsProbeEvent() && m.Status == shared.MonitorStatusUp:
			status = "online"
		}
		if status != "" {
			subject = m.Name + " is " + status
			fields = append(fields, [2]string{"Status", status})
		} else {
			subject = defaultEmailSubject + ": " + m.Name
		}
	}
	if hb != nil && !hb.Time.IsZero() {
		fields = append(fields, [2]string{"Time", hb.Time.UTC().Format("2006-01-02 15:04:05") + " UTC"})
	}

	var text, body strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&text, "%s: %s\n", field[0], field[1])
	}
	if len(fields) > 0 {
		text.WriteString("\n")
		body.WriteString("<p>")
		for i, field := range fields {
			if i > 0 {
				body.WriteString("<br>\n")
			}
			fmt.Fprintf(&body, "%s: <strong>%s</strong>", field[0], html.EscapeString(field[1]))
		}
		body.WriteString("</p>\n")
	}
	text.WriteString(message + "\n")
	body.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(message), "\n", "<br>\n") + "</p>\n")
	if link != "" {
		text.WriteString("\nOpen the monitor:\n" + link + "\n")
		fmt.Fprintf(&body, "<p><a href=\"%s\">Open the monitor</a></p>\n", html.EscapeString(link))
	}
	return mailer.Message{Subject: subject, Text: text.String(), HTML: body.String()}
}

// statusName is a heartbeat status as the email says it
func statusName(status shared.MonitorStatus) string {
	switch status {
	case shared.MonitorStatusDown:
		return "Down"
	case shared.MonitorStatusUp:
		return "Up"
	case shared.MonitorStatusPending:
		return "Pending"
	case shared.MonitorStatusMaintenance:
		return "Maintenance"
	default:
		return fmt.Sprintf("in status %d", status)
	}
}
