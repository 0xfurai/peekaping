package notification_channel

import (
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
		"install_email":  providers.NewInstallEmailSender(logger, accountMailer, members),
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
