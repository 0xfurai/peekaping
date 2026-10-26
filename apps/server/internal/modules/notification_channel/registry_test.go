package notification_channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/tag"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
	"go.uber.org/zap"
)

// newTestRegistry returns a registry of the given providers. It keeps the map, so a test can
// add providers to the map after.
func newTestRegistry(byType map[string]NotificationChannelProvider) *ProviderRegistry {
	return &ProviderRegistry{byType: byType}
}

func TestNewProviderRegistry_HasAProviderForEveryChannelType(t *testing.T) {
	registry := NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{})

	assert.Equal(t, []string{
		"discord", "google_chat", "gotify", "grafana_oncall", "line", "matrix", "mattermost", "ntfy",
		"opsgenie", "pagerduty", "pagertree", "pushbullet", "pushover", "sendgrid", "signal", "slack",
		"smtp", "teams", "telegram", "twilio", "webhook", "wecom", "whatsapp",
	}, registry.Types())
	for _, channelType := range registry.Types() {
		provider, ok := registry.Get(channelType)
		assert.True(t, ok, "a provider for %q", channelType)
		assert.NotNil(t, provider, "the provider for %q", channelType)
	}
}

// Each channel type gets its own provider: a type wired to another type's constructor would send
// to the wrong service, and no other test would notice
func TestNewProviderRegistry_MapsEachChannelTypeToItsOwnProvider(t *testing.T) {
	registry := NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{})

	wantProviders := map[string]NotificationChannelProvider{
		"discord":        &providers.DiscordSender{},
		"google_chat":    &providers.GoogleChatSender{},
		"gotify":         &providers.GotifySender{},
		"grafana_oncall": &providers.GrafanaOncallSender{},
		"line":           &providers.LineSender{},
		"matrix":         &providers.MatrixSender{},
		"mattermost":     &providers.MattermostSender{},
		"ntfy":           &providers.NTFYSender{},
		"opsgenie":       &providers.OpsgenieSender{},
		"pagerduty":      &providers.PagerDutySender{},
		"pagertree":      &providers.PagerTreeSender{},
		"pushbullet":     &providers.PushbulletSender{},
		"pushover":       &providers.PushoverSender{},
		"sendgrid":       &providers.SendGridSender{},
		"signal":         &providers.SignalSender{},
		"slack":          &providers.SlackSender{},
		"smtp":           &providers.EmailSender{},
		"teams":          &providers.TeamsSender{},
		"telegram":       &providers.TelegramSender{},
		"twilio":         &providers.TwilioSender{},
		"webhook":        &providers.WebhookSender{},
		"wecom":          &providers.WeComSender{},
		"whatsapp":       &providers.WhatsAppSender{},
	}

	assert.Len(t, wantProviders, len(registry.Types()), "a channel type per provider in the registry")
	for channelType, want := range wantProviders {
		provider, ok := registry.Get(channelType)
		require.True(t, ok, "a provider for %q", channelType)
		assert.IsType(t, want, provider, "the provider for %q", channelType)
	}
}

func TestProviderRegistry_GetRefusesAnUnknownChannelType(t *testing.T) {
	registry := NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{})

	provider, ok := registry.Get("no-such-type")

	assert.False(t, ok)
	assert.Nil(t, provider)
}

// The container builds the registry once, and the controller and the listener both send with it
func TestProviderRegistry_ControllerAndListenerUseTheContainersRegistry(t *testing.T) {
	provider := &recordingProvider{}
	container := dig.New()
	for _, constructor := range []any{
		func() *ProviderRegistry {
			return newTestRegistry(map[string]NotificationChannelProvider{"test-fake": provider})
		},
		func() *zap.SugaredLogger { return zap.NewNop().Sugar() },
		func() Service {
			return &fakeChannelService{channels: map[string]*Model{"a": testChannel("a", "test-fake")}}
		},
		func() monitor.Service {
			return &fakeMonitorService{monitors: map[string]*monitor.Model{"mon-1": {ID: "mon-1", Name: "Public API", Type: "http"}}}
		},
		func() monitor_notification.Service {
			return &fakeMonitorNotificationService{links: map[string][]*monitor_notification.Model{
				"mon-1": {{MonitorID: "mon-1", NotificationID: "a"}},
			}}
		},
		func() probe_notification.Service { return &fakeProbeNotificationService{} },
		func() heartbeat.Service { return nil },
		func() monitor_tag.Service { return &fakeMonitorTagService{} },
		func() tag.Service { return &fakeTagService{} },
		NewController,
		NewNotificationEventListener,
	} {
		require.NoError(t, container.Provide(constructor))
	}

	require.NoError(t, container.Invoke(func(controller *Controller, listener *NotificationEventListener) {
		router := httpx.NewRouter()
		httpx.Register(router, "POST", "/test", huma.Operation{Errors: []int{400, 404, 500}}, controller.Test)
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name": "Alerts", "type": "test-fake", "config": "{}"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, []string{"{}"}, provider.sentConfigs(), "configs the controller sent")

		bus := eventstest.NewBus()
		listener.Subscribe(bus)
		require.NoError(t, events.Publish(context.Background(), bus, events.ImportantHeartbeat, *downHeartbeat("mon-1")))
		assert.Equal(t, []string{"{}", channelConfig("a")}, provider.sentConfigs(), "configs the controller and the listener sent")
	}))
}
