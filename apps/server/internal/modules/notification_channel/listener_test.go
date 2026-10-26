package notification_channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/notification_channel/providers"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stale"
	"peekaping/internal/modules/tag"
	"peekaping/internal/reporting"
	"peekaping/internal/reporting/reportingtest"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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

// FindActiveByProbe pages through the active monitors of the probe, newest id first, the way the
// repository does. The local probe also runs the monitors that have no probe rows.
func (s *fakeMonitorService) FindActiveByProbe(_ context.Context, probeID string, page, limit int) ([]*monitor.Model, error) {
	ids := make([]string, 0, len(s.monitors))
	for id := range s.monitors {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	var found []*monitor.Model
	for _, id := range ids {
		m := s.monitors[id]
		runsOnProbe := slices.Contains(m.ProbeIDs, probeID) || (probeID == shared.LocalProbeID && len(m.ProbeIDs) == 0)
		if m.Active && runsOnProbe {
			found = append(found, m)
		}
	}
	start := min(page*limit, len(found))
	end := min(start+limit, len(found))
	return found[start:end], nil
}

type fakeMonitorNotificationService struct {
	monitor_notification.Service
	links map[string][]*monitor_notification.Model
	err   error
}

func (s *fakeMonitorNotificationService) FindByMonitorID(_ context.Context, monitorID string) ([]*monitor_notification.Model, error) {
	return s.links[monitorID], s.err
}

func (s *fakeMonitorNotificationService) FindByMonitorIDs(_ context.Context, monitorIDs []string) (map[string][]*monitor_notification.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	found := map[string][]*monitor_notification.Model{}
	for _, id := range monitorIDs {
		if links := s.links[id]; len(links) > 0 {
			found[id] = links
		}
	}
	return found, nil
}

type fakeProbeNotificationService struct {
	probe_notification.Service
	links map[string][]*probe_notification.Model
	err   error
}

func (s *fakeProbeNotificationService) FindByProbeID(_ context.Context, probeID string) ([]*probe_notification.Model, error) {
	return s.links[probeID], s.err
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

// sentMessages returns the messages of the notifications the provider sent
func (p *recordingProvider) sentMessages() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	messages := []string{}
	for _, n := range p.sent {
		messages = append(messages, n.message)
	}
	return messages
}

// sentMessagesByChannel returns the messages the provider sent, grouped by the config of the
// channel they went to
func (p *recordingProvider) sentMessagesByChannel() map[string][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	byChannel := map[string][]string{}
	for _, n := range p.sent {
		byChannel[n.config] = append(byChannel[n.config], n.message)
	}
	return byChannel
}

type listenerFixture struct {
	t           *testing.T
	listener    *NotificationEventListener
	providers   map[string]NotificationChannelProvider
	bus         *eventstest.Bus
	channels    *fakeChannelService
	monitors    *fakeMonitorService
	links       *fakeMonitorNotificationService
	probeLinks  *fakeProbeNotificationService
	monitorTags *fakeMonitorTagService
	tags        *fakeTagService
}

func newListenerFixture(t *testing.T) *listenerFixture {
	t.Helper()
	f := &listenerFixture{
		t:           t,
		bus:         eventstest.NewBus(),
		channels:    &fakeChannelService{channels: map[string]*Model{}, errs: map[string]error{}},
		monitors:    &fakeMonitorService{monitors: map[string]*monitor.Model{}},
		links:       &fakeMonitorNotificationService{links: map[string][]*monitor_notification.Model{}},
		probeLinks:  &fakeProbeNotificationService{links: map[string][]*probe_notification.Model{}},
		monitorTags: &fakeMonitorTagService{},
		tags:        &fakeTagService{tags: map[string]*tag.Model{}},
		providers:   map[string]NotificationChannelProvider{},
	}
	f.listener = NewNotificationEventListener(NotificationEventListenerParams{
		Service:                    f.channels,
		MonitorSvc:                 f.monitors,
		MonitorNotificationService: f.links,
		ProbeNotificationService:   f.probeLinks,
		MonitorTagService:          f.monitorTags,
		TagService:                 f.tags,
		Providers:                  newTestRegistry(f.providers),
		Logger:                     zap.NewNop().Sugar(),
	})
	f.listener.Subscribe(f.bus)
	return f
}

// addProvider gives the fixture's listener a provider for a channel type
func (f *listenerFixture) addProvider(channelType string, provider NotificationChannelProvider) {
	f.providers[channelType] = provider
}

// addProbeMonitor adds an active monitor that runs on the probes
func (f *listenerFixture) addProbeMonitor(id string, probeIDs ...string) {
	f.monitors.monitors[id] = &monitor.Model{ID: id, Name: id, Type: "http", Active: true, ProbeIDs: probeIDs}
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

// attachProbe links a notification channel to a probe
func (f *listenerFixture) attachProbe(probeID string, channel *Model) {
	f.channels.channels[channel.ID] = channel
	f.probeLinks.links[probeID] = append(f.probeLinks.links[probeID], &probe_notification.Model{ProbeID: probeID, NotificationID: channel.ID})
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
	require.NoError(f.t, events.Publish(context.Background(), f.bus, events.ImportantHeartbeat, *hb))
}

func (f *listenerFixture) publishCertificateExpiry(event *certificate.CertificateExpiryEvent) {
	require.NoError(f.t, events.Publish(context.Background(), f.bus, certificate.CertificateExpiry, *event))
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

func staleMonitor(monitorID string) stale.Event {
	return stale.Event{
		MonitorID:   monitorID,
		MonitorName: "Public API",
		Since:       time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC),
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
	f.addProvider("test-slack", slack)
	f.addProvider("test-email", email)
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
	// The event bus delivers events published by other processes as JSON
	f := newListenerFixture(t)
	provider := &recordingProvider{}
	f.addProvider("test-slack", provider)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-slack"))
	payload, err := json.Marshal(downHeartbeat("mon-1"))
	require.NoError(t, err)

	require.NoError(t, f.bus.PublishJSON(events.ImportantHeartbeat.Name(), string(payload)))

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
				f.addProvider("test-healthy", healthy)
				f.addProvider("test-rejecting", rejecting)
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

// A channel that's switched off (active: false) gets no notification, for any event, and the
// monitor's other channels still do
func TestListener_SkipsInactiveChannels(t *testing.T) {
	staleEventJSON := func(t *testing.T) string {
		payload, err := json.Marshal(staleMonitor("mon-1"))
		require.NoError(t, err)
		return string(payload)
	}
	publishers := []struct {
		name    string
		publish func(t *testing.T, f *listenerFixture)
	}{
		{"important heartbeat", func(t *testing.T, f *listenerFixture) { f.publishImportantHeartbeat(downHeartbeat("mon-1")) }},
		{"certificate expiry", func(t *testing.T, f *listenerFixture) { f.publishCertificateExpiry(certificateExpiry("mon-1")) }},
		{"stale monitor", func(t *testing.T, f *listenerFixture) {
			require.NoError(t, f.bus.PublishJSON(stale.MonitorStale.Name(), staleEventJSON(t)))
		}},
		{"stale monitor checked again", func(t *testing.T, f *listenerFixture) {
			require.NoError(t, f.bus.PublishJSON(stale.MonitorStaleRecovered.Name(), staleEventJSON(t)))
		}},
	}

	for _, tc := range publishers {
		t.Run(tc.name, func(t *testing.T) {
			f := newListenerFixture(t)
			active, inactive := &recordingProvider{}, &recordingProvider{}
			f.addProvider("test-active", active)
			f.addProvider("test-inactive", inactive)
			f.addMonitor("mon-1", "Public API")
			// The switched-off channel comes first, so skipping it must not stop the channels after it
			switchedOff := testChannel("off", "test-inactive")
			switchedOff.Active = false
			f.attach("mon-1", switchedOff)
			f.attach("mon-1", testChannel("on", "test-active"))

			tc.publish(t, f)

			assert.Equal(t, []string{channelConfig("on")}, active.sentConfigs())
			assert.Empty(t, inactive.sentConfigs())
		})
	}
}

func TestListener_FailedSendDoesNotStopOtherChannels(t *testing.T) {
	f := newListenerFixture(t)
	failing := &recordingProvider{send: func(context.Context) error { return errors.New("503 Service Unavailable") }}
	healthy := &recordingProvider{}
	f.addProvider("test-failing", failing)
	f.addProvider("test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-failing"))
	f.attach("mon-1", testChannel("b", "test-healthy"))

	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	assert.Equal(t, []string{channelConfig("a")}, failing.sentConfigs())
	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

func TestListener_FailedSendLogsNoRequestURL(t *testing.T) {
	f := newListenerFixture(t)
	core, logs := observer.New(zap.DebugLevel)
	f.listener.logger = zap.New(core).Sugar()
	const botToken = "123456:secret-bot-token"
	// What a provider returns when its HTTP client fails: a wrapper around the client's *url.Error
	failing := &recordingProvider{send: func(context.Context) error {
		return fmt.Errorf("failed to send telegram message: %w", &url.Error{
			Op:  "Get",
			URL: "https://api.telegram.org/bot" + botToken + "/sendMessage?chat_id=42",
			Err: errors.New("dial tcp 149.154.167.220:443: connect: connection refused"),
		})
	}}
	f.addProvider("test-failing", failing)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-failing"))

	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	failures := logs.FilterLevelExact(zap.ErrorLevel).All()
	require.Len(t, failures, 1)
	assert.Contains(t, failures[0].Message, "Failed to send notification")
	assert.Contains(t, failures[0].Message, "connection refused", "the log keeps the reason the send failed")
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, botToken)
		assert.NotContains(t, entry.Message, "api.telegram.org")
	}
}

func TestListener_PanickingChannelDoesNotStopOtherChannels(t *testing.T) {
	f := newListenerFixture(t)
	panicking := &recordingProvider{send: func(context.Context) error { panic("interface conversion") }}
	healthy := &recordingProvider{}
	f.addProvider("test-panicking", panicking)
	f.addProvider("test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-panicking"))
	f.attach("mon-1", testChannel("b", "test-healthy"))

	require.NotPanics(t, func() { f.publishCertificateExpiry(certificateExpiry("mon-1")) })

	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

func TestListener_ReportsAPanickingChannel(t *testing.T) {
	recorder := reportingtest.Install(t)
	f := newListenerFixture(t)
	f.addProvider("test-panicking", &recordingProvider{send: func(context.Context) error { panic("interface conversion") }})
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-panicking"))

	require.NotPanics(t, func() { f.publishCertificateExpiry(certificateExpiry("mon-1")) })

	events := recorder.Wait(t, 1)
	assert.Equal(t, "notification", events[0].Tags["component"])
	assert.Equal(t, "string", events[0].Exception[0].Type)
	frames := events[0].Exception[0].Stacktrace.Frames
	assert.Contains(t, frames[len(frames)-1].Function, "TestListener_ReportsAPanickingChannel", "the channel's send is where it panicked")
}

func TestListener_ReportsNothingForAPanickingChannelWhileReportingIsOff(t *testing.T) {
	recorder := reportingtest.Install(t)
	reporting.SetEnabled(false)
	f := newListenerFixture(t)
	panicking := &recordingProvider{send: func(context.Context) error { panic("interface conversion") }}
	f.addProvider("test-panicking", panicking)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-panicking"))

	require.NotPanics(t, func() { f.publishCertificateExpiry(certificateExpiry("mon-1")) })

	assert.Empty(t, recorder.Events())
}

// A channel that fails to send is a misconfigured channel, which the product tells the admin about
func TestListener_ReportsNothingForAChannelThatFailsToSend(t *testing.T) {
	recorder := reportingtest.Install(t)
	f := newListenerFixture(t)
	f.addProvider("test-failing", &recordingProvider{send: func(context.Context) error { return errors.New("503 Service Unavailable") }})
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-failing"))

	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	assert.Empty(t, recorder.Events())
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
			f.addProvider("test-healthy", provider)
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
	f.addProvider("test-healthy", provider)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-healthy"))

	assert.Error(t, f.bus.PublishJSON(events.ImportantHeartbeat.Name(), `"mon-1"`))
	assert.Error(t, f.bus.PublishJSON(certificate.CertificateExpiry.Name(), `{"monitor_id": 1}`))

	assert.Empty(t, provider.sentConfigs())
}

func TestListener_CertificateExpirySendsNotificationToTheMonitorsChannels(t *testing.T) {
	f := newListenerFixture(t)
	slack, email := &recordingProvider{}, &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProvider("test-email", email)
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

func TestListener_StaleMonitorSendsNotificationToTheMonitorsChannels(t *testing.T) {
	cases := []struct {
		topic   events.Topic[stale.Event]
		message string
	}{
		{stale.MonitorStale, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC"},
		{stale.MonitorStaleRecovered, "Peekaping is checking Public API again. It was not checked from 2026-10-18 11:56:00 UTC"},
	}
	for _, tc := range cases {
		t.Run(tc.topic.Name(), func(t *testing.T) {
			f := newListenerFixture(t)
			slack := &recordingProvider{}
			f.addProvider("test-slack", slack)
			f.addMonitor("mon-1", "Public API")
			f.addMonitor("mon-2", "Billing")
			f.attach("mon-1", testChannel("a", "test-slack"))
			f.attach("mon-2", testChannel("c", "test-slack"))
			// Another replica published it, so it arrives as JSON
			payload, err := json.Marshal(stale.Event{MonitorID: "mon-1", MonitorName: "Public API", Since: time.Date(2026, 10, 18, 11, 56, 0, 0, time.UTC)})
			require.NoError(t, err)

			require.NoError(t, f.bus.PublishJSON(tc.topic.Name(), string(payload)))

			require.Equal(t, []string{channelConfig("a")}, slack.sentConfigs())
			sent := slack.sent[0]
			assert.Equal(t, tc.message, sent.message)
			assert.Equal(t, "Public API", sent.monitor.Name)
			assert.Nil(t, sent.heartbeat, "a stale-monitor notification carries no heartbeat")
		})
	}
}

func TestListener_StaleMonitorSaysWhichProbesDoNotRunItsType(t *testing.T) {
	cases := []struct {
		name    string
		probes  []string
		message string
	}{
		{"one probe", []string{"Frankfurt"}, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC: probe Frankfurt doesn't run http monitors"},
		{"two probes", []string{"Frankfurt", "Lagos"}, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC: probes Frankfurt and Lagos don't run http monitors"},
		{"three probes", []string{"Frankfurt", "Lagos", "Quito"}, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC: probes Frankfurt, Lagos and Quito don't run http monitors"},
		{"no probe", nil, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC"},
		{"an empty list", []string{}, "Peekaping has not checked Public API since 2026-10-18 11:56:00 UTC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newListenerFixture(t)
			slack := &recordingProvider{}
			f.addProvider("test-slack", slack)
			f.addMonitor("mon-1", "Public API")
			f.attach("mon-1", testChannel("a", "test-slack"))
			// Another replica published it, so it arrives as JSON
			payload, err := json.Marshal(stale.Event{MonitorID: "mon-1", MonitorName: "Public API", Since: time.Date(2026, 10, 18, 11, 56, 0, 0, time.UTC), UnsupportedProbes: tc.probes})
			require.NoError(t, err)

			require.NoError(t, f.bus.PublishJSON(stale.MonitorStale.Name(), string(payload)))

			require.Len(t, slack.sent, 1)
			assert.Equal(t, tc.message, slack.sent[0].message)
		})
	}
}

func TestListener_StaleMonitorCheckedAgainDoesNotSayWhichProbesDoNotRunItsType(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addMonitor("mon-1", "Public API")
	f.attach("mon-1", testChannel("a", "test-slack"))
	payload, err := json.Marshal(stale.Event{MonitorID: "mon-1", MonitorName: "Public API", Since: time.Date(2026, 10, 18, 11, 56, 0, 0, time.UTC), UnsupportedProbes: []string{"Frankfurt"}})
	require.NoError(t, err)

	require.NoError(t, f.bus.PublishJSON(stale.MonitorStaleRecovered.Name(), string(payload)))

	require.Len(t, slack.sent, 1)
	assert.Equal(t, "Peekaping is checking Public API again. It was not checked from 2026-10-18 11:56:00 UTC", slack.sent[0].message)
}

func probeEvent(probeID string) probe.Event {
	return probe.Event{ProbeID: probeID, ProbeName: "Frankfurt", Since: time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)}
}

func TestNotificationListener_ProbeOfflineAndOnline(t *testing.T) {
	cases := []struct {
		topic   events.Topic[probe.Event]
		status  shared.MonitorStatus
		message string
	}{
		{probe.ProbeOffline, shared.MonitorStatusDown, "Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC"},
		{probe.ProbeOnline, shared.MonitorStatusUp, "Probe Frankfurt is online"},
	}
	for _, tc := range cases {
		t.Run(tc.topic.Name(), func(t *testing.T) {
			f := newListenerFixture(t)
			slack, email := &recordingProvider{}, &recordingProvider{}
			f.addProvider("test-slack", slack)
			f.addProvider("test-email", email)
			f.attachProbe("probe-1", testChannel("a", "test-slack"))
			f.attachProbe("probe-1", testChannel("b", "test-email"))
			f.attachProbe("probe-2", testChannel("c", "test-slack"))
			// The sweep, or an api replica, publishes it, so it arrives as JSON
			payload, err := json.Marshal(probeEvent("probe-1"))
			require.NoError(t, err)

			require.NoError(t, f.bus.PublishJSON(tc.topic.Name(), string(payload)))

			assert.Equal(t, []string{channelConfig("a")}, slack.sentConfigs(), "the probe's channels, not another probe's")
			assert.Equal(t, []string{channelConfig("b")}, email.sentConfigs())
			sent := slack.sent[0]
			assert.Equal(t, tc.message, sent.message)
			require.NotNil(t, sent.monitor, "the providers read the monitor's name and id, so they get a stand-in")
			assert.Equal(t, "Probe Frankfurt", sent.monitor.Name)
			assert.Equal(t, "probe-1", sent.monitor.ID, "which keeps one probe's notification apart from another's in a channel that deduplicates")
			assert.True(t, sent.monitor.IsProbeEvent(), "so a channel type links the notification to the Probes page, not to a monitor page that doesn't exist")
			assert.Equal(t, tc.status, sent.monitor.Status, "so a channel type that treats Up and Down differently can tell a probe going offline from one coming back")
			assert.Nil(t, sent.heartbeat, "a probe notification carries no heartbeat")
		})
		t.Run(tc.topic.Name()+" for a probe with no channels", func(t *testing.T) {
			f := newListenerFixture(t)
			slack := &recordingProvider{}
			f.addProvider("test-slack", slack)
			f.attachProbe("probe-2", testChannel("c", "test-slack"))

			f.publishProbeEvent(tc.topic, probeEvent("probe-1"))

			assert.Empty(t, slack.sentConfigs())
		})
	}
}

// A probe with no channels of its own tells the channels of its monitors, so a probe going offline
// doesn't leave its monitors unchecked with no alert.
func TestNotificationListener_ProbeWithoutChannelsNotifiesItsMonitorsChannels(t *testing.T) {
	f := newListenerFixture(t)
	slack, email := &recordingProvider{}, &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProvider("test-email", email)
	f.addProbeMonitor("mon-1", "probe-1")
	f.addProbeMonitor("mon-2", "probe-1")
	f.attach("mon-1", testChannel("a", "test-slack"))
	f.attach("mon-2", testChannel("b", "test-email"))

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))

	assert.Equal(t, map[string][]string{channelConfig("a"): {"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC"}}, slack.sentMessagesByChannel())
	assert.Equal(t, map[string][]string{channelConfig("b"): {"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC"}}, email.sentMessagesByChannel())

	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	assert.Equal(t, map[string][]string{channelConfig("a"): {"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC", "Probe Frankfurt is online"}}, slack.sentMessagesByChannel())
	assert.Equal(t, map[string][]string{channelConfig("b"): {"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC", "Probe Frankfurt is online"}}, email.sentMessagesByChannel())

	// Still the stand-in, not a monitor, so a channel that opens an incident per monitor id opens one per probe
	for _, sent := range append(slack.sent, email.sent...) {
		require.NotNil(t, sent.monitor)
		assert.Equal(t, "probe-1", sent.monitor.ID)
		assert.Equal(t, "Probe Frankfurt", sent.monitor.Name)
		assert.True(t, sent.monitor.IsProbeEvent())
		assert.Nil(t, sent.heartbeat)
	}
}

func TestNotificationListener_ProbeFallbackSendsToASharedChannelOnce(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProbeMonitor("mon-1", "probe-1")
	f.addProbeMonitor("mon-2", "probe-1")
	sharedChannel := testChannel("a", "test-slack")
	f.attach("mon-1", sharedChannel)
	f.attach("mon-2", sharedChannel)

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))
	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	assert.Equal(t, []string{
		"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC",
		"Probe Frankfurt is online",
	}, slack.sentMessages(), "one offline and one online, not one for each monitor")
}

func TestNotificationListener_ProbeFallbackSkipsPausedMonitorsAndOtherProbes(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProbeMonitor("mon-active", "probe-1")
	f.addProbeMonitor("mon-paused", "probe-1")
	f.monitors.monitors["mon-paused"].Active = false
	f.addProbeMonitor("mon-elsewhere", "probe-2")
	f.attach("mon-active", testChannel("active", "test-slack"))
	f.attach("mon-paused", testChannel("paused", "test-slack"))
	f.attach("mon-elsewhere", testChannel("elsewhere", "test-slack"))

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))
	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	assert.Equal(t, []string{channelConfig("active"), channelConfig("active")}, slack.sentConfigs(),
		"only the active monitor of the probe, for the offline and the online notification")
}

func TestNotificationListener_ProbeWithChannelsDoesNotNotifyItsMonitorsChannels(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.attachProbe("probe-1", testChannel("c", "test-slack"))
	f.addProbeMonitor("mon-1", "probe-1")
	f.attach("mon-1", testChannel("a", "test-slack"))

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))
	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	assert.Equal(t, []string{channelConfig("c"), channelConfig("c")}, slack.sentConfigs())
}

func TestNotificationListener_ProbeWithNoChannelsAndNoMonitorChannelsSendsNothingAndLogsIt(t *testing.T) {
	cases := map[string]func(f *listenerFixture){
		"no monitors":               func(f *listenerFixture) {},
		"monitors without channels": func(f *listenerFixture) { f.addProbeMonitor("mon-1", "probe-1") },
		"only a paused monitor has a channel": func(f *listenerFixture) {
			f.addProbeMonitor("mon-1", "probe-1")
			f.monitors.monitors["mon-1"].Active = false
			f.attach("mon-1", testChannel("a", "test-slack"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newListenerFixture(t)
			core, logs := observer.New(zap.DebugLevel)
			f.listener.logger = zap.New(core).Sugar()
			slack := &recordingProvider{}
			f.addProvider("test-slack", slack)
			setup(f)

			f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))

			assert.Empty(t, slack.sentConfigs())
			assert.Len(t, logs.FilterMessage("No notification channels configured for probe probe-1 or its monitors").All(), 1)
		})
	}
}

func TestNotificationListener_ProbeFallbackSendsNothingWhenTheMonitorsChannelsCannotBeRead(t *testing.T) {
	f := newListenerFixture(t)
	core, logs := observer.New(zap.DebugLevel)
	f.listener.logger = zap.New(core).Sugar()
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProbeMonitor("mon-1", "probe-1")
	f.attach("mon-1", testChannel("a", "test-slack"))
	f.links.err = errors.New("database is locked")

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))

	assert.Empty(t, slack.sentConfigs())
	assert.Len(t, logs.FilterLevelExact(zap.ErrorLevel).All(), 1)
}

func TestNotificationListener_LocalProbeFallbackIncludesMonitorsWithoutProbeRows(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.addProbeMonitor("mon-no-rows")
	f.addProbeMonitor("mon-local", shared.LocalProbeID)
	f.addProbeMonitor("mon-remote", "probe-1")
	f.attach("mon-no-rows", testChannel("no-rows", "test-slack"))
	f.attach("mon-local", testChannel("local", "test-slack"))
	f.attach("mon-remote", testChannel("remote", "test-slack"))

	f.publishProbeEvent(probe.ProbeOffline, probe.Event{ProbeID: shared.LocalProbeID, ProbeName: "Local", Since: time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)})

	assert.ElementsMatch(t, []string{channelConfig("no-rows"), channelConfig("local")}, slack.sentConfigs())
	assert.ElementsMatch(t, []string{"Probe Local has been offline since 2026-10-22 08:30:00 UTC", "Probe Local has been offline since 2026-10-22 08:30:00 UTC"}, slack.sentMessages())
}

func TestNotificationListener_ProbeFallbackReadsEveryPageOfMonitors(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	// One more than a page of monitors, each with a channel of its own
	const monitorCount = 101
	for i := range monitorCount {
		id := fmt.Sprintf("mon-%03d", i)
		f.addProbeMonitor(id, "probe-1")
		f.attach(id, testChannel(fmt.Sprintf("c-%03d", i), "test-slack"))
	}

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))

	assert.Len(t, slack.sentConfigs(), monitorCount)
}

// The real PagerDuty sender decides from the stand-in's status, since a probe's notification has no
// heartbeat, so this checks what PagerDuty receives: one incident, opened and then resolved.
func TestNotificationListener_ProbeOfflineThenOnlineOpensAndResolvesOnePagerDutyIncident(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	pagerDuty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer pagerDuty.Close()

	f := newListenerFixture(t)
	f.addProvider("pagerduty", providers.NewPagerDutySender(zap.NewNop().Sugar(), &config.Config{ClientURL: "https://peekaping.example.com"}, providers.NewHTTPClient()))
	pagerDutyConfig := fmt.Sprintf(`{"pagerduty_integration_key": "routing-key", "pagerduty_integration_url": %q, "pagerduty_auto_resolve": "resolve"}`, pagerDuty.URL)
	f.attachProbe("probe-1", &Model{ID: "pd", Name: "PagerDuty", Type: "pagerduty", Active: true, Config: &pagerDutyConfig})

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))
	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 2, "one trigger and one resolve")
	assert.Equal(t, "trigger", received[0]["event_action"])
	assert.Equal(t, "resolve", received[1]["event_action"])
	assert.Equal(t, "Peekaping/probe-1", received[0]["dedup_key"])
	assert.Equal(t, "Peekaping/probe-1", received[1]["dedup_key"], "the resolve closes the incident the trigger opened")
}

func TestNotificationListener_ProbeEventsSkipChannelsThatCannotSend(t *testing.T) {
	f := newListenerFixture(t)
	healthy, rejecting := &recordingProvider{}, &recordingProvider{validateErr: errors.New("webhook_url is required")}
	f.addProvider("test-healthy", healthy)
	f.addProvider("test-rejecting", rejecting)
	f.attachProbe("probe-1", testChannel("ok", "test-healthy"))
	f.attachProbe("probe-1", testChannel("invalid", "test-rejecting"))
	inactive := testChannel("off", "test-healthy")
	inactive.Active = false
	f.attachProbe("probe-1", inactive)
	f.probeLinks.links["probe-1"] = append(f.probeLinks.links["probe-1"], &probe_notification.Model{ProbeID: "probe-1", NotificationID: "deleted"})

	f.publishProbeEvent(probe.ProbeOffline, probeEvent("probe-1"))

	assert.Equal(t, []string{channelConfig("ok")}, healthy.sentConfigs())
	assert.Empty(t, rejecting.sentConfigs())
}

func TestNotificationListener_ProbeEventsSendNothingWhenTheChannelsCannotBeRead(t *testing.T) {
	f := newListenerFixture(t)
	slack := &recordingProvider{}
	f.addProvider("test-slack", slack)
	f.attachProbe("probe-1", testChannel("a", "test-slack"))
	f.probeLinks.err = errors.New("database is locked")

	f.publishProbeEvent(probe.ProbeOnline, probeEvent("probe-1"))

	assert.Empty(t, slack.sentConfigs())
}

func (f *listenerFixture) publishProbeEvent(topic events.Topic[probe.Event], event probe.Event) {
	require.NoError(f.t, events.Publish(context.Background(), f.bus, topic, event))
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
	f.addProvider("webhook", providers.NewWebhookSender(zap.NewNop().Sugar(), providers.NewHTTPClient()))
	f.addProvider("test-healthy", healthy)
	f.addMonitor("mon-1", "Public API")
	webhookConfig := `{"webhook_url": "` + hung.URL + `", "webhook_content_type": "json"}`
	f.attach("mon-1", &Model{ID: "hook", Name: "Webhook", Type: "webhook", Active: true, Config: &webhookConfig})
	f.attach("mon-1", testChannel("b", "test-healthy"))

	start := time.Now()
	f.publishImportantHeartbeat(downHeartbeat("mon-1"))

	assert.Less(t, time.Since(start), 2*time.Second, "the send must stop at its deadline")
	assert.Equal(t, []string{channelConfig("b")}, healthy.sentConfigs())
}

// A channel that takes its whole send timeout delays that monitor's later notifications, in
// order, and no other monitor's. This runs the listener on the Redis event bus.
func TestListener_ManyStuckMonitorsDontHoldUpTheOthers(t *testing.T) {
	f := newListenerFixture(t)
	release := make(chan struct{})
	stuck := &recordingProvider{send: func(context.Context) error { <-release; return nil }}
	fast := &recordingProvider{}
	f.addProvider("test-stuck", stuck)
	f.addProvider("test-fast", fast)
	// More stuck monitors than the bus's default worker limit
	const stuckMonitors = events.DefaultWorkers + 4
	for i := range stuckMonitors {
		id := fmt.Sprintf("stuck-%d", i)
		f.addMonitor(id, id)
		f.attach(id, testChannel("s-"+id, "test-stuck"))
	}
	f.addMonitor("mon-fast", "Billing")
	f.attach("mon-fast", testChannel("f", "test-fast"))

	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	bus := infra.NewRedisEventBus(client, zap.NewNop().Sugar())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, bus.Close(ctx))
	})
	t.Cleanup(func() { close(release) })
	f.listener.Subscribe(bus)

	for i := range stuckMonitors {
		require.NoError(t, events.Publish(context.Background(), bus, events.ImportantHeartbeat, *downHeartbeat(fmt.Sprintf("stuck-%d", i))))
	}
	require.Eventually(t, func() bool { return len(stuck.sentConfigs()) == stuckMonitors }, 5*time.Second, 5*time.Millisecond,
		"every stuck monitor's send has started")
	require.NoError(t, events.Publish(context.Background(), bus, events.ImportantHeartbeat, *downHeartbeat("mon-fast")))

	require.Eventually(t, func() bool { return len(fast.sentConfigs()) == 1 }, 5*time.Second, 5*time.Millisecond,
		"a healthy monitor's notification goes out while the others are stuck")
}

func TestListener_ASlowChannelDelaysOnlyItsOwnMonitor(t *testing.T) {
	f := newListenerFixture(t)
	release := make(chan struct{})
	slow := &recordingProvider{send: func(context.Context) error { <-release; return nil }}
	fast := &recordingProvider{}
	f.addProvider("test-slow", slow)
	f.addProvider("test-fast", fast)
	f.addMonitor("mon-1", "Public API")
	f.addMonitor("mon-2", "Billing")
	f.attach("mon-1", testChannel("a", "test-slow"))
	f.attach("mon-2", testChannel("b", "test-fast"))

	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	bus := infra.NewRedisEventBus(client, zap.NewNop().Sugar())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, bus.Close(ctx))
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.listener.Subscribe(bus)
	publish := func(hb *heartbeat.Model) {
		require.NoError(t, events.Publish(context.Background(), bus, events.ImportantHeartbeat, *hb))
	}
	down, up := downHeartbeat("mon-1"), downHeartbeat("mon-1")
	down.Msg, up.Msg, up.Status = "went down", "back up", shared.MonitorStatusUp

	publish(down)
	require.Eventually(t, func() bool { return len(slow.sentConfigs()) == 1 }, 5*time.Second, 5*time.Millisecond)
	publish(up)
	publish(downHeartbeat("mon-2"))

	require.Eventually(t, func() bool { return len(fast.sentConfigs()) == 1 }, 5*time.Second, 5*time.Millisecond,
		"another monitor's notification goes out while the slow channel is stuck")
	assert.Never(t, func() bool { return len(slow.sentConfigs()) > 1 }, 200*time.Millisecond, 10*time.Millisecond,
		"the monitor's later notification waits for the stuck one")
	close(release)
	require.Eventually(t, func() bool { return len(slow.sentConfigs()) == 2 }, 5*time.Second, 5*time.Millisecond)
	slow.mu.Lock()
	defer slow.mu.Unlock()
	assert.Equal(t, []string{"went down", "back up"}, []string{slow.sent[0].message, slow.sent[1].message}, "in the order they happened")
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
	f.addProvider("test-slow", provider)
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

// redisConfig is the config of a cluster binary whose Redis is at addr
func redisConfig(t *testing.T, addr string) *config.Config {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	return &config.Config{RedisHost: host, RedisPort: port, QueueConcurrency: 4, Timezone: "UTC"}
}

// newClusterBus returns the event bus of one cluster process, as the app wires it: the Redis
// event bus on its own client, with the notification topics routed to the notification queue
func newClusterBus(t *testing.T, cfg *config.Config) *QueuedBus {
	t.Helper()
	logger := zap.NewNop().Sugar()
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(cfg.RedisHost, cfg.RedisPort)})
	t.Cleanup(func() { _ = client.Close() })
	asynqClient, err := infra.ProvideAsynqClient(cfg, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asynqClient.Close() })

	bus := NewQueuedBus(infra.NewRedisEventBus(client, logger), infra.ProvideQueueService(asynqClient, logger))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, bus.Close(ctx))
	})
	return bus
}

// apiReplica is one cmd/api process: its event bus, and the consumer that takes the
// notification queue's tasks into it
type apiReplica struct {
	bus      *QueuedBus
	consumer *QueueConsumer
}

// start makes the replica take notification tasks, until the test ends
func (r *apiReplica) start(t *testing.T) {
	t.Helper()
	require.NoError(t, r.consumer.Start())
	t.Cleanup(r.consumer.Stop)
}

// newAPIReplicas builds n api replicas the way cluster mode does: each has its own services,
// its own Redis event bus and its own notification listener, all over the same Redis. Monitor
// "mon-1" has a channel of type "test-slack" for each of channelIDs, and the replicas send
// through the one slack provider. The replicas don't take notification tasks yet.
func newAPIReplicas(t *testing.T, redisAddr string, slack NotificationChannelProvider, n int, channelIDs ...string) []*apiReplica {
	t.Helper()
	cfg := redisConfig(t, redisAddr)
	replicas := make([]*apiReplica, n)
	for i := range replicas {
		f := newListenerFixture(t)
		f.addProvider("test-slack", slack)
		f.addMonitor("mon-1", "Public API")
		for _, id := range channelIDs {
			f.attach("mon-1", testChannel(id, "test-slack"))
			f.attachProbe("probe-1", testChannel(id, "test-slack"))
		}
		bus := newClusterBus(t, cfg)
		f.listener.Subscribe(bus)
		server, err := infra.ProvideNotificationAsynqServer(cfg, zap.NewNop().Sugar())
		require.NoError(t, err)
		replicas[i] = &apiReplica{bus: bus, consumer: NewQueueConsumer(server, bus, zap.NewNop().Sugar())}
	}
	return replicas
}

// startAPIReplicas starts n api replicas that take notification tasks, see newAPIReplicas
func startAPIReplicas(t *testing.T, redisAddr string, slack NotificationChannelProvider, n int, channelIDs ...string) []*apiReplica {
	t.Helper()
	replicas := newAPIReplicas(t, redisAddr, slack, n, channelIDs...)
	for _, replica := range replicas {
		replica.start(t)
	}
	return replicas
}

const (
	// settle is how long the provider's send count holds still before settledSends takes it as final
	settle = 500 * time.Millisecond
	// asynqPoll is how long asynq waits before it looks at an empty queue again. A window shorter
	// than this can pass with no replica having polled.
	asynqPoll = time.Second
)

// settledSends waits for the provider to send want notifications, then for its send count to
// hold still for the hold time, and returns the count. Waiting for the count to settle, not for
// a number, catches replicas that send a moment after the first.
func settledSends(t *testing.T, provider *recordingProvider, want int, hold time.Duration) int {
	t.Helper()
	require.Eventually(t, func() bool { return len(provider.sentConfigs()) >= want }, 5*time.Second, 5*time.Millisecond,
		"the api replicas sent fewer notifications than expected")
	count, stableSince := len(provider.sentConfigs()), time.Now()
	for time.Since(stableSince) < hold {
		time.Sleep(10 * time.Millisecond)
		if n := len(provider.sentConfigs()); n != count {
			count, stableSince = n, time.Now()
		}
	}
	return count
}

// Redis pub/sub hands a published event to every api replica, so each one's listener would send
// the notification. The ingester publishes each important heartbeat once, and each of the
// monitor's channels must get one notification for it: a queue gives the event to one replica.
func TestNotificationListener_ThreeAPIReplicasSendEachImportantHeartbeatOncePerChannel(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a", "b")
	ingester := newClusterBus(t, redisConfig(t, redisServer.Addr()))
	down, up := downHeartbeat("mon-1"), downHeartbeat("mon-1")
	down.Msg, up.Msg, up.Status = "went down", "back up", shared.MonitorStatusUp

	require.NoError(t, events.Publish(context.Background(), ingester, events.ImportantHeartbeat, *down))
	require.NoError(t, events.Publish(context.Background(), ingester, events.ImportantHeartbeat, *up))

	// Two channels, two heartbeats: four notifications. The replicas don't agree on an order.
	assert.Equal(t, 4, settledSends(t, provider, 4, settle), "notifications sent for a Down and an Up heartbeat to 2 channels, with 3 api replicas")
	byChannel := provider.sentMessagesByChannel()
	assert.Len(t, byChannel, 2, "channels that got a notification")
	assert.ElementsMatch(t, []string{"went down", "back up"}, byChannel[channelConfig("a")], "notifications sent to channel a")
	assert.ElementsMatch(t, []string{"went down", "back up"}, byChannel[channelConfig("b")], "notifications sent to channel b")
}

func TestNotificationListener_ThreeAPIReplicasSendACertificateExpiryNotificationOnce(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	ingester := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), ingester, certificate.CertificateExpiry, *certificateExpiry("mon-1")))

	assert.Equal(t, 1, settledSends(t, provider, 1, settle), "notifications sent for one certificate expiry, with 3 api replicas")
}

func TestNotificationListener_ThreeAPIReplicasSendAStaleMonitorsNotCheckedSinceNotificationOnce(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	publisher := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), publisher, stale.MonitorStale, staleMonitor("mon-1")))

	assert.Equal(t, 1, settledSends(t, provider, 1, settle), "notifications sent for one stale monitor, with 3 api replicas")
	assert.Equal(t, []string{"Peekaping has not checked Public API since 2026-10-17 09:30:00 UTC"}, provider.sentMessages())
}

func TestNotificationListener_ThreeAPIReplicasSendAStaleMonitorsCheckingAgainNotificationOnce(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	publisher := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), publisher, stale.MonitorStaleRecovered, staleMonitor("mon-1")))

	assert.Equal(t, 1, settledSends(t, provider, 1, settle), "notifications sent for one recovered stale monitor, with 3 api replicas")
	assert.Equal(t, []string{"Peekaping is checking Public API again. It was not checked from 2026-10-17 09:30:00 UTC"}, provider.sentMessages())
}

func TestNotificationListener_ThreeAPIReplicasSendAProbeOfflineNotificationOnce(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	sweep := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), sweep, probe.ProbeOffline, probeEvent("probe-1")))

	assert.Equal(t, 1, settledSends(t, provider, 1, settle), "notifications sent for one offline probe, with 3 api replicas")
	assert.Equal(t, []string{"Probe Frankfurt has been offline since 2026-10-22 08:30:00 UTC"}, provider.sentMessages())
}

func TestNotificationListener_ThreeAPIReplicasSendAProbeOnlineNotificationOnce(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	startAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	replica := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), replica, probe.ProbeOnline, probeEvent("probe-1")))

	assert.Equal(t, 1, settledSends(t, provider, 1, settle), "notifications sent for one probe coming back, with 3 api replicas")
	assert.Equal(t, []string{"Probe Frankfurt is online"}, provider.sentMessages())
}

// The queue keeps a notification raised while no api replica runs, which a pub/sub message
// published to no subscriber would not
func TestNotificationListener_ANotificationRaisedWithoutAnAPIReplicaIsSentOnceOneStarts(t *testing.T) {
	provider := &recordingProvider{}
	redisServer := miniredis.RunT(t)
	replicas := newAPIReplicas(t, redisServer.Addr(), provider, 3, "a")
	ingester := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), ingester, events.ImportantHeartbeat, *downHeartbeat("mon-1")))
	// Longer than asynq's poll of an empty queue, so a send that waits for a poll would show
	assert.Never(t, func() bool { return len(provider.sentConfigs()) > 0 }, 2*asynqPoll, 10*time.Millisecond,
		"nothing is sent while no api replica takes tasks")
	replicas[0].start(t)

	// The count holds still past a further poll, so a second send of the task would show
	assert.Equal(t, 1, settledSends(t, provider, 1, 2*asynqPoll), "notifications sent once the first api replica started")
	replicas[1].start(t)
	replicas[2].start(t)
	assert.Equal(t, 1, settledSends(t, provider, 1, 2*asynqPoll), "notifications sent once all three api replicas started")
}

// Only the notification topics go through the queue. The websocket in every api replica pushes
// each heartbeat to its own clients, so every api replica must get it.
func TestNotificationListener_EveryAPIReplicaStillGetsAHeartbeat(t *testing.T) {
	redisServer := miniredis.RunT(t)
	replicas := startAPIReplicas(t, redisServer.Addr(), &recordingProvider{}, 3, "a")
	received := make([]chan heartbeat.Model, len(replicas))
	for i, replica := range replicas {
		received[i] = make(chan heartbeat.Model, 2)
		events.Subscribe(events.NewSubscriber(replica.bus, "websocket"), events.HeartbeatEvent, func(_ context.Context, hb heartbeat.Model) {
			received[i] <- hb
		})
	}
	ingester := newClusterBus(t, redisConfig(t, redisServer.Addr()))

	require.NoError(t, events.Publish(context.Background(), ingester, events.HeartbeatEvent, *downHeartbeat("mon-1")))

	for i := range replicas {
		select {
		case hb := <-received[i]:
			assert.Equal(t, downHeartbeat("mon-1"), &hb, "heartbeat received by api replica %d", i)
		case <-time.After(5 * time.Second):
			t.Fatalf("api replica %d got no heartbeat", i)
		}
	}
	for i := range replicas {
		assert.Empty(t, received[i], "api replica %d got the heartbeat more than once", i)
	}
}

func TestEveryChannelTypeHasGoldenRequests(t *testing.T) {
	registry := NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{})

	// The listener sends both of these to every channel type
	for _, event := range []string{"down", "certificate_expiry"} {
		for _, channelType := range registry.Types() {
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
