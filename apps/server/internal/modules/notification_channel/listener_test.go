package notification_channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/tag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// syncEventBus calls handlers as soon as an event is published
type syncEventBus struct {
	handlers map[events.EventType][]events.EventHandler
}

func (b *syncEventBus) Subscribe(eventType events.EventType, handler events.EventHandler) {
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *syncEventBus) Publish(event events.Event) {
	for _, handler := range b.handlers[event.Type] {
		handler(event)
	}
}

func (b *syncEventBus) Close() error { return nil }

type fakeChannelService struct {
	Service
	channels map[string]*Model
	errs     map[string]error
}

func (s *fakeChannelService) FindByID(_ context.Context, id string) (*Model, error) {
	if err := s.errs[id]; err != nil {
		return nil, err
	}
	return s.channels[id], nil
}

type fakeMonitorService struct {
	monitor.Service
	monitors map[string]*monitor.Model
}

func (s *fakeMonitorService) FindByID(_ context.Context, id string) (*monitor.Model, error) {
	return s.monitors[id], nil
}

type fakeMonitorNotificationService struct {
	monitor_notification.Service
	links map[string][]*monitor_notification.Model
	err   error
}

func (s *fakeMonitorNotificationService) FindByMonitorID(_ context.Context, monitorID string) ([]*monitor_notification.Model, error) {
	return s.links[monitorID], s.err
}

type sentNotification struct {
	config    string
	message   string
	monitor   *monitor.Model
	heartbeat *heartbeat.Model
}

// recordingProvider is a notification channel provider that records what it's asked to send
type recordingProvider struct {
	validateErr error
	// send, when set, decides how each send ends
	send func(ctx context.Context) error

	mu   sync.Mutex
	sent []sentNotification
}

func (p *recordingProvider) Validate(string) error { return p.validateErr }

func (p *recordingProvider) Unmarshal(string) (any, error) { return nil, nil }

func (p *recordingProvider) Send(ctx context.Context, configJSON, message string, m *monitor.Model, hb *heartbeat.Model) error {
	p.mu.Lock()
	p.sent = append(p.sent, sentNotification{config: configJSON, message: message, monitor: m, heartbeat: hb})
	p.mu.Unlock()
	if p.send != nil {
		return p.send(ctx)
	}
	return nil
}

// sentConfigs returns the configs of the channels the provider sent to
func (p *recordingProvider) sentConfigs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	configs := []string{}
	for _, n := range p.sent {
		configs = append(configs, n.config)
	}
	return configs
}

func registerTestProvider(t *testing.T, channelType string, provider NotificationChannelProvider) {
	t.Helper()
	RegisterNotificationChannelProvider(channelType, provider)
	t.Cleanup(func() { delete(NotificationChannelProviderRegistry, channelType) })
}

type listenerFixture struct {
	listener    *NotificationEventListener
	bus         *syncEventBus
	channels    *fakeChannelService
	monitors    *fakeMonitorService
	links       *fakeMonitorNotificationService
	monitorTags *fakeMonitorTagService
	tags        *fakeTagService
}

func newListenerFixture(t *testing.T) *listenerFixture {
	t.Helper()
	f := &listenerFixture{
		bus:         &syncEventBus{handlers: map[events.EventType][]events.EventHandler{}},
		channels:    &fakeChannelService{channels: map[string]*Model{}, errs: map[string]error{}},
		monitors:    &fakeMonitorService{monitors: map[string]*monitor.Model{}},
		links:       &fakeMonitorNotificationService{links: map[string][]*monitor_notification.Model{}},
		monitorTags: &fakeMonitorTagService{},
		tags:        &fakeTagService{tags: map[string]*tag.Model{}},
	}
	f.listener = NewNotificationEventListener(NotificationEventListenerParams{
		Service:                    f.channels,
		MonitorSvc:                 f.monitors,
		MonitorNotificationService: f.links,
		MonitorTagService:          f.monitorTags,
		TagService:                 f.tags,
		Logger:                     zap.NewNop().Sugar(),
		Config:                     &config.Config{},
	})
	f.listener.Subscribe(f.bus)
	return f
}

func (f *listenerFixture) addMonitor(id, name string) {
	f.monitors.monitors[id] = &monitor.Model{ID: id, Name: name, Type: "http"}
}

// tagMonitors gives every monitor the tag
func (f *listenerFixture) tagMonitors(id, name string) {
	f.tags.tags[id] = &tag.Model{ID: id, Name: name}
	f.monitorTags.rels = append(f.monitorTags.rels, &monitor_tag.Model{TagID: id})
}

// attach links a notification channel to a monitor
func (f *listenerFixture) attach(monitorID string, channel *Model) {
	f.channels.channels[channel.ID] = channel
	f.link(monitorID, channel.ID)
}

// link links a channel ID to a monitor without the channel existing
func (f *listenerFixture) link(monitorID, channelID string) {
	f.links.links[monitorID] = append(f.links.links[monitorID], &monitor_notification.Model{
		ID:             fmt.Sprintf("mn-%s-%s", monitorID, channelID),
		MonitorID:      monitorID,
		NotificationID: channelID,
	})
}

func (f *listenerFixture) publishImportantHeartbeat(hb *heartbeat.Model) {
	f.bus.Publish(events.Event{Type: events.ImportantHeartbeat, Payload: hb})
}

func (f *listenerFixture) publishCertificateExpiry(event *certificate.CertificateExpiryEvent) {
	f.bus.Publish(events.Event{Type: events.CertificateExpiry, Payload: event})
}

func testChannel(id, channelType string) *Model {
	config := channelConfig(id)
	return &Model{ID: id, Name: "Channel " + id, Type: channelType, Active: true, Config: &config}
}

func channelConfig(id string) string {
	return fmt.Sprintf(`{"channel": %q}`, id)
}

func downHeartbeat(monitorID string) *heartbeat.Model {
	return &heartbeat.Model{
		ID:        "hb-1",
		MonitorID: monitorID,
		Status:    shared.MonitorStatusDown,
		Msg:       "connection refused",
		Important: true,
		Time:      time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC),
	}
}

func certificateExpiry(monitorID string) *certificate.CertificateExpiryEvent {
	return &certificate.CertificateExpiryEvent{
		MonitorID:   monitorID,
		MonitorName: "Public API",
		CertInfo: &certificate.CertificateInfo{
			Subject:  "CN=api.example.com,O=Example Corp",
			Issuer:   "CN=R11,O=Let's Encrypt,C=US",
			CertType: "server",
			ValidTo:  time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC),
			ValidFor: []string{"api.example.com", "www.example.com"},
		},
		DaysRemaining: 7,
		TargetDays:    7,
	}
}

func TestListener_ImportantHeartbeatSendsToTheMonitorsChannels(t *testing.T) {
	f := newListenerFixture(t)
	slack, email := &recordingProvider{}, &recordingProvider{}
	registerTestProvider(t, "test-slack", slack)
	registerTestProvider(t, "test-email", email)
	f.addMonitor("mon-1", "Public API")
	f.addMonitor("mon-2", "Billing")
	f.attach("mon-1", testChannel("a", "test-slack"))
	f.attach("mon-1", testChannel("b", "test-email"))
	f.attach("mon-2", testChannel("c", "test-slack"))
	f.tagMonitors("t1", "team:core")
	hb := downHeartbeat("mon-1")

	f.publishImportantHeartbeat(hb)

	assert.Equal(t, []string{channelConfig("a")}, slack.sentConfigs())
	assert.Equal(t, []string{channelConfig("b")}, email.sentConfigs())
	sent := slack.sent[0]
	assert.Equal(t, "connection refused", sent.message)
	assert.Equal(t, "Public API", sent.monitor.Name)
	assert.Equal(t, []string{"team:core"}, sent.monitor.Tags)
	assert.Equal(t, hb, sent.heartbeat)
}

func TestListener_ImportantHeartbeatFromAnotherInstance(t *testing.T) {
	// The Redis event bus delivers events published by other instances as JSON
	f := newListenerFixture(t)
	provider := &recordingProvider{}
	registerTestProvider(t, "test-slack", provider)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-slack"))
	payload, err := json.Marshal(downHeartbeat("mon-1"))
	require.NoError(t, err)

	f.bus.Publish(events.Event{Type: events.ImportantHeartbeat, Payload: json.RawMessage(payload)})

	require.Equal(t, []string{channelConfig("a")}, provider.sentConfigs())
	assert.Equal(t, downHeartbeat("mon-1"), provider.sent[0].heartbeat)
}

func TestListener_SkipsChannelsThatCannotSend(t *testing.T) {
	healthy := &recordingProvider{}
	rejecting := &recordingProvider{validateErr: errors.New("webhook_url is required")}

	cases := []struct {
		name  string
		setup func(f *listenerFixture)
	}{
		{"channel no longer exists", func(f *listenerFixture) {
			f.link("mon-1", "deleted")
		}},
		{"channel lookup fails", func(f *listenerFixture) {
			f.attach("mon-1", testChannel("broken", "test-healthy"))
			f.channels.errs["broken"] = errors.New("connection reset")
		}},
		{"type has no provider", func(f *listenerFixture) {
			f.attach("mon-1", testChannel("unknown", "test-not-registered"))
		}},
		{"channel has no config", func(f *listenerFixture) {
			channel := testChannel("empty", "test-healthy")
			channel.Config = nil
			f.attach("mon-1", channel)
		}},
		{"config fails validation", func(f *listenerFixture) {
			f.attach("mon-1", testChannel("invalid", "test-rejecting"))
		}},
	}

	for _, tc := range cases {
		publishers := map[string]func(f *listenerFixture){
			"important heartbeat": func(f *listenerFixture) { f.publishImportantHeartbeat(downHeartbeat("mon-1")) },
			"certificate expiry":  func(f *listenerFixture) { f.publishCertificateExpiry(certificateExpiry("mon-1")) },
		}
		for eventName, publish := range publishers {
			t.Run(eventName+"/"+tc.name, func(t *testing.T) {
				healthy.sent, rejecting.sent = nil, nil
				f := newListenerFixture(t)
				registerTestProvider(t, "test-healthy", healthy)
				registerTestProvider(t, "test-rejecting", rejecting)
				f.addMonitor("mon-1", "Public API")
				tc.setup(f)
				f.attach("mon-1", testChannel("ok", "test-healthy"))

				publish(f)

				assert.Equal(t, []string{channelConfig("ok")}, healthy.sentConfigs())
				assert.Empty(t, rejecting.sentConfigs())
			})
		}
	}
}

func TestListener_FailedSendDoesNotStopOtherChannels(t *testing.T) {
	f := newListenerFixture(t)
	failing := &recordingProvider{send: func(context.Context) error { return errors.New("503 Service Unavailable") }}
	healthy := &recordingProvider{}
	registerTestProvider(t, "test-failing", failing)
	registerTestProvider(t, "test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-failing"))
	f.attach("mon-1", testChannel("b", "test-healthy"))

	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	assert.Equal(t, []string{channelConfig("a")}, failing.sentConfigs())
	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

func TestListener_PanickingChannelDoesNotStopOtherChannels(t *testing.T) {
	f := newListenerFixture(t)
	panicking := &recordingProvider{send: func(context.Context) error { panic("interface conversion") }}
	healthy := &recordingProvider{}
	registerTestProvider(t, "test-panicking", panicking)
	registerTestProvider(t, "test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-panicking"))
	f.attach("mon-1", testChannel("b", "test-healthy"))

	require.NotPanics(t, func() { f.publishCertificateExpiry(certificateExpiry("mon-1")) })

	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

func TestListener_SendsNothingWithoutTheMonitor(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *listenerFixture)
	}{
		{"monitor not found", func(f *listenerFixture) {
			f.attach("mon-1", testChannel("a", "test-healthy"))
		}},
		{"links lookup fails", func(f *listenerFixture) {
			f.addMonitor("mon-1", "Public API")
			f.attach("mon-1", testChannel("a", "test-healthy"))
			f.links.err = errors.New("connection reset")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newListenerFixture(t)
			provider := &recordingProvider{}
			registerTestProvider(t, "test-healthy", provider)
			tc.setup(f)

			f.publishImportantHeartbeat(downHeartbeat("mon-1"))
			f.publishCertificateExpiry(certificateExpiry("mon-1"))

			assert.Empty(t, provider.sentConfigs())
		})
	}
}

func TestListener_IgnoresEventsWithTheWrongPayload(t *testing.T) {
	f := newListenerFixture(t)
	provider := &recordingProvider{}
	registerTestProvider(t, "test-healthy", provider)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-healthy"))

	f.bus.Publish(events.Event{Type: events.ImportantHeartbeat, Payload: "mon-1"})
	f.bus.Publish(events.Event{Type: events.CertificateExpiry, Payload: json.RawMessage(`{"monitor_id": 1}`)})

	assert.Empty(t, provider.sentConfigs())
}

func TestListener_CertificateExpirySendsNotificationToTheMonitorsChannels(t *testing.T) {
	f := newListenerFixture(t)
	slack, email := &recordingProvider{}, &recordingProvider{}
	registerTestProvider(t, "test-slack", slack)
	registerTestProvider(t, "test-email", email)
	f.addMonitor("mon-1", "Public API")
	f.addMonitor("mon-2", "Billing")
	f.attach("mon-1", testChannel("a", "test-slack"))
	f.attach("mon-1", testChannel("b", "test-email"))
	f.attach("mon-2", testChannel("c", "test-slack"))
	f.tagMonitors("t1", "team:core")

	f.publishCertificateExpiry(certificateExpiry("mon-1"))

	assert.Equal(t, []string{channelConfig("a")}, slack.sentConfigs())
	assert.Equal(t, []string{channelConfig("b")}, email.sentConfigs())
	sent := email.sent[0]
	assert.Equal(t, "🚨 Certificate Expiry Warning\n\n"+
		"Monitor: Public API\n"+
		"Certificate: api.example.com (server)\n"+
		"Expires in: 7 days\n"+
		"Valid until: 2026-10-24 09:30:00\n"+
		"Notification threshold: 7 days\n"+
		"Valid for: api.example.com (+1 more)\n"+
		"Issuer: CN=R11,O=Let's Encrypt,C=US", sent.message)
	assert.Equal(t, "Public API", sent.monitor.Name)
	assert.Equal(t, []string{"team:core"}, sent.monitor.Tags)
	assert.Nil(t, sent.heartbeat, "a certificate expiry notification carries no heartbeat")
}

func TestListener_HungChannelDoesNotHoldUpOtherChannels(t *testing.T) {
	// The webhook endpoint never answers
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer hung.Close()
	defer close(release)

	f := newListenerFixture(t)
	f.listener.sendTimeout = 200 * time.Millisecond
	healthy := &recordingProvider{}
	registerTestProvider(t, "test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	webhookConfig := `{"webhook_url": "` + hung.URL + `", "webhook_content_type": "json"}`
	f.attach("mon-1", &Model{ID: "hook", Name: "Webhook", Type: "webhook", Active: true, Config: &webhookConfig})
	f.attach("mon-1", testChannel("b", "test-healthy"))

	start := time.Now()
	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	assert.Less(t, time.Since(start), 2*time.Second, "the send must stop at its deadline")
	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

func TestListener_LimitsConcurrentSends(t *testing.T) {
	f := newListenerFixture(t)
	release := make(chan struct{})
	var mu sync.Mutex
	active, peak := 0, 0
	provider := &recordingProvider{send: func(context.Context) error {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		return nil
	}}
	registerTestProvider(t, "test-slow", provider)
	f.addMonitor("mon-1", "Public API")
	channels := maxConcurrentSends + 3
	for i := range channels {
		f.attach("mon-1", testChannel(fmt.Sprint(i), "test-slow"))
	}

	done := make(chan struct{})
	go func() {
		f.publishImportantHeartbeat(downHeartbeat("mon-1"))
		close(done)
	}()
	activeSends := func() int {
		mu.Lock()
		defer mu.Unlock()
		return active
	}
	require.Eventually(t, func() bool { return activeSends() == maxConcurrentSends }, 5*time.Second, 10*time.Millisecond)
	assert.Never(t, func() bool { return activeSends() > maxConcurrentSends }, 100*time.Millisecond, 10*time.Millisecond)
	close(release)
	<-done

	assert.Equal(t, maxConcurrentSends, peak)
	assert.Len(t, provider.sentConfigs(), channels)
}

func TestEveryChannelTypeHasGoldenRequests(t *testing.T) {
	// Building the listener registers the providers, as at startup
	newListenerFixture(t)

	// The listener sends both of these to every channel type
	for _, event := range []string{"down", "certificate_expiry"} {
		for channelType := range NotificationChannelProviderRegistry {
			goldens, err := filepath.Glob(filepath.Join("providers", "testdata", "requests", channelType+"_*"+event+".golden"))
			require.NoError(t, err)
			assert.NotEmpty(t, goldens, "add a %s golden case for %q to providers/golden_test.go", event, channelType)
		}
	}
}

type fakeMonitorTagService struct {
	monitor_tag.Service
	rels []*monitor_tag.Model
	err  error
}

func (f *fakeMonitorTagService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_tag.Model, error) {
	return f.rels, f.err
}

type fakeTagService struct {
	tag.Service
	tags map[string]*tag.Model
	err  error
}

func (f *fakeTagService) FindByID(ctx context.Context, id string) (*tag.Model, error) {
	return f.tags[id], f.err
}

func newTagTestListener(monitorTags *fakeMonitorTagService, tags *fakeTagService) *NotificationEventListener {
	return &NotificationEventListener{
		monitorTagService: monitorTags,
		tagService:        tags,
		logger:            zap.NewNop().Sugar(),
	}
}

func TestLoadMonitorTags(t *testing.T) {
	tags := &fakeTagService{tags: map[string]*tag.Model{
		"t1": {ID: "t1", Name: "severity:critical"},
		"t2": {ID: "t2", Name: "country:ua"},
		"t3": {ID: "t3", Name: "team:core"},
	}}

	t.Run("sorts tag names", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t3"}, {TagID: "t1"}, {TagID: "t2"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{"country:ua", "severity:critical", "team:core"}, m.Tags)
	})

	t.Run("untagged monitor gets an empty list", func(t *testing.T) {
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(&fakeMonitorTagService{}, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{}, m.Tags)
	})

	t.Run("skips a relation whose tag is gone", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t1"}, {TagID: "deleted"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{"severity:critical"}, m.Tags)
	})

	t.Run("leaves tags empty when relations fail to load", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{err: errors.New("db down")}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Nil(t, m.Tags)
	})

	t.Run("leaves tags empty when a tag fails to load", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t1"}, {TagID: "t2"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, &fakeTagService{err: errors.New("db down")}).loadMonitorTags(context.Background(), m)

		assert.Nil(t, m.Tags)
	})
}
