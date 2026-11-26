package notification_channel

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/netguard"
	"slices"

	"go.uber.org/zap"
)

// ProviderRegistry holds the provider for each notification channel type. dig builds it once,
// so the controller and the listener use the same providers.
type ProviderRegistry struct {
	byType map[string]NotificationChannelProvider
	guard  *netguard.Guard
}

// NewProviderRegistry builds the provider for every notification channel type. The providers
// share one HTTP client, so they reuse connections. The install email provider sends through the
// install's account-email sender, to a member of the channel's workspace.
//
// The guard (BLOCK_PRIVATE_TARGETS, PP-158) is the one the checks dial through. The HTTP client and
// the SMTP channel's sender connect through it, and CheckTargets uses it on a save. The install email
// channel is not guarded: it sends through the install's own account-email sender. A nil guard
// blocks nothing.
func NewProviderRegistry(logger *zap.SugaredLogger, settings *installsettings.Resolver, accountMailer *mailer.Mailer, members workspace.MemberRepository, guard *netguard.Guard) *ProviderRegistry {
	client := providers.NewGuardedHTTPClient(guard)
	return &ProviderRegistry{guard: guard, byType: map[string]NotificationChannelProvider{
		"smtp":           providers.NewGuardedEmailSender(logger, guard),
		"install_email":  providers.NewInstallEmailSender(logger, settings, accountMailer, members),
		"telegram":       providers.NewTelegramSender(logger, client),
		"webhook":        providers.NewWebhookSender(logger, client),
		"slack":          providers.NewSlackSender(logger, settings, client),
		"ntfy":           providers.NewNTFYSender(logger, client),
		"pagerduty":      providers.NewPagerDutySender(logger, settings, client),
		"opsgenie":       providers.NewOpsgenieSender(logger, client),
		"google_chat":    providers.NewGoogleChatSender(logger, settings, client),
		"grafana_oncall": providers.NewGrafanaOncallSender(logger, client),
		"signal":         providers.NewSignalSender(logger, client),
		"gotify":         providers.NewGotifySender(logger, client),
		"pushover":       providers.NewPushoverSender(logger, client),
		"mattermost":     providers.NewMattermostSender(logger, client),
		"matrix":         providers.NewMatrixSender(logger, client),
		"discord":        providers.NewDiscordSender(logger, client),
		"wecom":          providers.NewWeComSender(logger, client),
		"whatsapp":       providers.NewWhatsAppSender(logger, client),
		"twilio":         providers.NewTwilioSender(logger, client),
		"sendgrid":       providers.NewSendGridSender(logger, client),
		"pushbullet":     providers.NewPushbulletSender(logger, client),
		"pagertree":      providers.NewPagerTreeSender(logger, client),
		"line":           providers.NewLineSender(logger, client),
		"teams":          providers.NewTeamsSender(logger, settings, client),
	}}
}

// Get returns the provider for a channel type
func (r *ProviderRegistry) Get(channelType string) (NotificationChannelProvider, bool) {
	provider, ok := r.byType[channelType]
	return provider, ok
}

// Types returns the channel types the registry has a provider for, in alphabetical order
func (r *ProviderRegistry) Types() []string {
	return slices.Sorted(maps.Keys(r.byType))
}

// ErrUnsupportedType is a channel type that has no provider
var ErrUnsupportedType = errors.New("unsupported notification type")

// ValidateConfig checks a channel's type and config in themselves, the way the create route does
// before it looks at the install or the workspace: the type has a provider, and the provider takes
// the config.
func (r *ProviderRegistry) ValidateConfig(channelType, config string) error {
	provider, ok := r.Get(channelType)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnsupportedType, channelType)
	}
	return provider.Validate(config)
}

// The reasons the install or the workspace refuses a channel whose config is valid in itself
const (
	// RefusedTarget is a URL or host that the install blocks (BLOCK_PRIVATE_TARGETS)
	RefusedTarget = "target_not_allowed"
	// RefusedNoSender is an install email channel on an install with no sender for account emails
	RefusedNoSender = "no_install_sender"
	// RefusedRecipient is an install email channel whose recipient is not a member of the workspace
	RefusedRecipient = "recipient_not_member"
)

// Refusal is why the install or the workspace would refuse to save a channel whose config is valid
// in itself (ValidateConfig), as one of the Refused codes, or "" when it would save it. The error
// is a check that failed to run.
func (r *ProviderRegistry) Refusal(ctx context.Context, workspaceID workspace.ID, channelType, config string) (string, error) {
	if err := r.CheckTargets(channelType, config); err != nil {
		var blocked *netguard.TargetError
		if errors.As(err, &blocked) {
			return RefusedTarget, nil
		}
		return "", err
	}
	provider, _ := r.Get(channelType)
	scoped, ok := provider.(WorkspaceValidator)
	if !ok {
		return "", nil
	}
	err := scoped.ValidateInWorkspace(providers.WithWorkspace(ctx, workspaceID), config)
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, providers.ErrNoInstallSender):
		return RefusedNoSender, nil
	case errors.Is(err, providers.ErrRecipientNotMember):
		return RefusedRecipient, nil
	}
	return "", err
}
