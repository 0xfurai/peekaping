package notification_channel

import (
	"context"
	"fmt"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/events"
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
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

const (
	// maxConcurrentSends bounds how many channels one event sends to at once
	maxConcurrentSends = 8
	// sendTimeout bounds each channel's send, so a channel that never answers can't hold up an event
	sendTimeout = 15 * time.Second
	// notificationWorkers is how many monitors' notifications go out at once. Sends wait on the
	// network, up to sendTimeout each, so the limit is well above the bus's default: a few
	// monitors with a dead channel mustn't hold up everyone else's notifications.
	notificationWorkers = 128
	// probeMonitorPageSize is how many of a probe's monitors are read at a time, when the probe
	// has no channels and its monitors' channels are told instead
	probeMonitorPageSize = 100
)

// NotificationEventListener handles notification events
type NotificationEventListener struct {
	service                    Service
	monitorSvc                 monitor.Service
	heartbeatService           heartbeat.Service
	monitorNotificationService monitor_notification.Service
	probeNotificationService   probe_notification.Service
	monitorTagService          monitor_tag.Service
	tagService                 tag.Service
	providers                  *ProviderRegistry
	logger                     *zap.SugaredLogger
	sendTimeout                time.Duration
}

type NotificationEventListenerParams struct {
	dig.In
	Service                    Service
	MonitorSvc                 monitor.Service
	HeartbeatService           heartbeat.Service
	MonitorNotificationService monitor_notification.Service
	ProbeNotificationService   probe_notification.Service
	MonitorTagService          monitor_tag.Service
	TagService                 tag.Service
	Providers                  *ProviderRegistry
	Logger                     *zap.SugaredLogger
}

func NewNotificationEventListener(p NotificationEventListenerParams) *NotificationEventListener {
	return &NotificationEventListener{
		service:                    p.Service,
		monitorSvc:                 p.MonitorSvc,
		heartbeatService:           p.HeartbeatService,
		monitorNotificationService: p.MonitorNotificationService,
		probeNotificationService:   p.ProbeNotificationService,
		monitorTagService:          p.MonitorTagService,
		tagService:                 p.TagService,
		providers:                  p.Providers,
		logger:                     p.Logger,
		sendTimeout:                sendTimeout,
	}
}

// Subscribe subscribes to NotifyEvent and sends notifications
//
// The handlers share one subscriber, so one monitor's notifications go out in the order the
// events happened, and a channel that takes its whole send timeout delays only that
// monitor's later notifications.
func (l *NotificationEventListener) Subscribe(eventBus events.EventBus) {
	subscriber := events.NewSubscriber(eventBus, "notification", events.SubscriberWorkers(notificationWorkers))
	events.Subscribe(subscriber, events.ImportantHeartbeat, l.handleNotifyEvent)
	events.Subscribe(subscriber, certificate.CertificateExpiry, l.handleCertificateExpiryEvent)
	events.Subscribe(subscriber, stale.MonitorStale, func(ctx context.Context, staleEvent stale.Event) {
		l.handleStaleMonitorEvent(ctx, staleEvent, false)
	})
	events.Subscribe(subscriber, stale.MonitorStaleRecovered, func(ctx context.Context, staleEvent stale.Event) {
		l.handleStaleMonitorEvent(ctx, staleEvent, true)
	})
	events.Subscribe(subscriber, probe.ProbeOffline, func(ctx context.Context, probeEvent probe.Event) {
		l.handleProbeEvent(ctx, probeEvent, false)
	})
	events.Subscribe(subscriber, probe.ProbeOnline, func(ctx context.Context, probeEvent probe.Event) {
		l.handleProbeEvent(ctx, probeEvent, true)
	})
}

// staleTimeFormat is how stale-monitor and probe notifications show a time
const staleTimeFormat = "2006-01-02 15:04:05 UTC"

// handleProbeEvent notifies that a probe went offline, or that it is online. The providers take a
// monitor, so a stand-in carries the probe: its name says it is a probe, and its id is the
// probe's, which keeps the notifications of two probes apart in a channel that deduplicates by it.
// Its type marks it as a probe's, so a provider that links to the monitor links to the Probes page.
// Its status tells offline (Down) from online (Up), for a provider that treats them differently,
// since the notification has no heartbeat to say so.
//
// A probe with channels of its own tells those. One without tells the channels of its active
// monitors, since a probe going offline doesn't mark them Down, and nothing else would say they
// stopped being checked. Each event reads them again instead of the online event remembering the
// offline one's: the two sets differ only when the probe's monitors changed while it was offline.
func (l *NotificationEventListener) handleProbeEvent(ctx context.Context, probeEvent probe.Event, online bool) {
	l.logger.Infof("Probe event received for probe: %s (online: %t)", probeEvent.ProbeID, online)

	links, err := l.probeNotificationService.FindByProbeID(ctx, probeEvent.ProbeID)
	if err != nil {
		l.logger.Errorf("Failed to get probe-notification records: %v", err)
		return
	}
	var ids []string
	if len(links) > 0 {
		ids = make([]string, len(links))
		for i, link := range links {
			ids[i] = link.NotificationID
		}
	} else {
		ids, err = l.probeMonitorChannelIDs(ctx, probeEvent.ProbeID)
		if err != nil {
			l.logger.Errorf("Failed to get the notification channels of the monitors of probe %s: %v", probeEvent.ProbeID, err)
			return
		}
	}
	if len(ids) == 0 {
		l.logger.Debugf("No notification channels configured for probe %s or its monitors", probeEvent.ProbeID)
		return
	}

	message := fmt.Sprintf("Probe %s has been offline since %s", probeEvent.ProbeName, probeEvent.Since.UTC().Format(staleTimeFormat))
	status := shared.MonitorStatusDown
	if online {
		message = fmt.Sprintf("Probe %s is online", probeEvent.ProbeName)
		status = shared.MonitorStatusUp
	}
	standIn := &monitor.Model{ID: probeEvent.ProbeID, Type: shared.MonitorTypeProbe, Name: "Probe " + probeEvent.ProbeName, Status: status}
	// A probe notification carries no heartbeat
	l.sendToChannels(ctx, l.channelsByID(ctx, ids), message, standIn, nil)
}

// probeMonitorChannelIDs returns the ids of the channels of the probe's active monitors, each once.
// They come in the order of the monitors, then of each monitor's links. A page of monitors is read
// with its links before the next page, to keep each read of the links small.
func (l *NotificationEventListener) probeMonitorChannelIDs(ctx context.Context, probeID string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for page := 0; ; page++ {
		monitors, err := l.monitorSvc.FindActiveByProbe(ctx, probeID, page, probeMonitorPageSize)
		if err != nil {
			return nil, err
		}
		monitorIDs := make([]string, len(monitors))
		for i, m := range monitors {
			monitorIDs[i] = m.ID
		}
		linksByMonitor, err := l.monitorNotificationService.FindByMonitorIDs(ctx, monitorIDs)
		if err != nil {
			return nil, err
		}
		for _, monitorID := range monitorIDs {
			for _, link := range linksByMonitor[monitorID] {
				if !seen[link.NotificationID] {
					seen[link.NotificationID] = true
					ids = append(ids, link.NotificationID)
				}
			}
		}
		if len(monitors) < probeMonitorPageSize {
			return ids, nil
		}
	}
}

// handleStaleMonitorEvent notifies that a monitor stopped being checked, or that it is being checked again
func (l *NotificationEventListener) handleStaleMonitorEvent(ctx context.Context, staleEvent stale.Event, recovered bool) {
	l.logger.Infof("Stale monitor event received for monitor: %s (recovered: %t)", staleEvent.MonitorID, recovered)

	notificationChannels, monitorModel := l.findChannels(ctx, staleEvent.MonitorID)
	if monitorModel == nil {
		return
	}

	since := staleEvent.Since.UTC().Format(staleTimeFormat)
	message := fmt.Sprintf("Peekaping has not checked %s since %s", monitorModel.Name, since)
	if recovered {
		message = fmt.Sprintf("Peekaping is checking %s again. It was not checked from %s", monitorModel.Name, since)
	} else if reason := unsupportedProbesReason(staleEvent.UnsupportedProbes, monitorModel.Type); reason != "" {
		message += ": " + reason
	}
	// A stale-monitor notification carries no heartbeat
	l.sendToChannels(ctx, notificationChannels, message, monitorModel, nil)
}

// unsupportedProbesReason says why no probe checks a monitor when probes don't run its type:
// "probe Frankfurt doesn't run docker monitors", "probes Frankfurt and Lagos don't run docker
// monitors". It is empty when no probe is named.
func unsupportedProbesReason(probeNames []string, monitorType string) string {
	switch len(probeNames) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("probe %s doesn't run %s monitors", probeNames[0], monitorType)
	default:
		last := len(probeNames) - 1
		return fmt.Sprintf("probes %s and %s don't run %s monitors", strings.Join(probeNames[:last], ", "), probeNames[last], monitorType)
	}
}

func (l *NotificationEventListener) handleNotifyEvent(ctx context.Context, hb heartbeat.Model) {
	l.logger.Infof("Notification event received for monitor: %s", hb.MonitorID)

	notificationChannels, monitorModel := l.findChannels(ctx, hb.MonitorID)
	if monitorModel == nil {
		return
	}

	l.sendToChannels(ctx, notificationChannels, withLocations(hb.Msg, monitorModel, hb.Status), monitorModel, &hb)
}

// withLocations adds to the message of a Down or Up notification where the monitor is Down or Up,
// when it runs on more than one probe: "Down at Frankfurt, Tokyo (2 of 3 locations): <message>".
// The heartbeat itself is not changed. The message of any other notification, and of a monitor on
// one probe, is as it was.
func withLocations(message string, m *monitor.Model, status shared.MonitorStatus) string {
	summary := providers.LocationSummary(m, status)
	switch {
	case summary == "":
		return message
	case message == "":
		return summary
	default:
		return summary + ": " + message
	}
}

func (l *NotificationEventListener) handleCertificateExpiryEvent(ctx context.Context, certEvent certificate.CertificateExpiryEvent) {
	l.logger.Infof("Certificate expiry event received for monitor: %s", certEvent.MonitorID)

	notificationChannels, monitorModel := l.findChannels(ctx, certEvent.MonitorID)
	if monitorModel == nil {
		return
	}

	// A certificate expiry notification carries no heartbeat
	message := l.formatCertificateExpiryMessage(&certEvent, monitorModel)
	l.sendToChannels(ctx, notificationChannels, message, monitorModel, nil)
}

// findChannels returns the notification channels attached to a monitor, and the monitor
// with its tags. The monitor is nil when there's nothing to send.
func (l *NotificationEventListener) findChannels(ctx context.Context, monitorID string) ([]*Model, *monitor.Model) {
	monitorNotifications, err := l.monitorNotificationService.FindByMonitorID(ctx, monitorID)
	if err != nil {
		l.logger.Errorf("Failed to get monitor-notification records: %v", err)
		return nil, nil
	}

	if len(monitorNotifications) == 0 {
		l.logger.Debugf("No notification channels configured for monitor %s", monitorID)
		return nil, nil
	}

	ids := make([]string, len(monitorNotifications))
	for i, mn := range monitorNotifications {
		l.logger.Infof("Monitor notification: %s", mn.NotificationID)
		ids[i] = mn.NotificationID
	}
	notificationChannels := l.channelsByID(ctx, ids)

	// Fetch monitor details for context
	monitorModel, err := l.monitorSvc.FindByID(ctx, monitorID)
	if err != nil || monitorModel == nil {
		l.logger.Warnf("Monitor not found for notification context: %s", monitorID)
		return nil, nil
	}
	l.loadMonitorTags(ctx, monitorModel)

	return notificationChannels, monitorModel
}

// channelsByID returns the notification channels with the given ids. A channel that can't be
// read, or that no longer exists, is logged and left out.
func (l *NotificationEventListener) channelsByID(ctx context.Context, ids []string) []*Model {
	var notificationChannels []*Model
	for _, id := range ids {
		notification, err := l.service.FindByID(ctx, id)
		if err != nil {
			l.logger.Errorf("Failed to get notification by ID: %s, error: %v", id, err)
			continue
		}
		if notification != nil {
			notificationChannels = append(notificationChannels, notification)
		} else {
			l.logger.Warnf("Notification not found for notification link: %s", id)
		}
	}
	return notificationChannels
}

// sendToChannels sends a notification to each active channel that can take it. Channels are sent
// to concurrently, each with its own deadline, so a slow channel doesn't hold up the others.
// It returns once every send has finished.
func (l *NotificationEventListener) sendToChannels(
	ctx context.Context,
	notificationChannels []*Model,
	message string,
	monitorModel *monitor.Model,
	hb *heartbeat.Model,
) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentSends)

	for _, notificationChannel := range notificationChannels {
		if !notificationChannel.Active {
			l.logger.Debugf("Skipping inactive notification channel: %s", notificationChannel.Name)
			continue
		}
		provider, ok := l.providers.Get(notificationChannel.Type)
		if !ok {
			l.logger.Warnf("No provider registered for notification type: %s", notificationChannel.Type)
			continue
		}
		if notificationChannel.Config == nil {
			l.logger.Warnf("No config for notification: %s", notificationChannel.Name)
			continue
		}

		// validate config
		if err := provider.Validate(*notificationChannel.Config); err != nil {
			l.logger.Errorf("Failed to validate notification config: %s, error: %v", notificationChannel.Name, err)
			continue
		}

		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			l.send(ctx, provider, notificationChannel, message, monitorModel, hb)
		}()
	}

	wg.Wait()
}

func (l *NotificationEventListener) send(
	ctx context.Context,
	provider NotificationChannelProvider,
	notificationChannel *Model,
	message string,
	monitorModel *monitor.Model,
	hb *heartbeat.Model,
) {
	// Sends run in their own goroutines, out of reach of the event bus's recover
	defer func() {
		if r := recover(); r != nil {
			l.logger.Errorf("Notification channel panicked: %s, panic: %v", notificationChannel.Name, r)
			reporting.CapturePanic("notification", r, debug.Stack())
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, l.sendTimeout)
	defer cancel()

	// An HTTP client's errors hold the request URL, and for a channel the URL holds a credential
	if err := providers.RedactURLErrors(provider.Send(ctx, *notificationChannel.Config, message, monitorModel, hb)); err != nil {
		l.logger.Errorf("Failed to send notification: %s, error: %v", notificationChannel.Name, err)
		return
	}
	l.logger.Infof("Notification sent to: %s for monitor: %s", notificationChannel.Name, monitorModel.ID)
}

// loadMonitorTags sets the names of the monitor's tags on it, sorted so that every
// notification for the monitor lists them in the same order. If the lookup fails, it logs
// the error and leaves the tags empty: the notification is still worth sending.
func (l *NotificationEventListener) loadMonitorTags(ctx context.Context, m *monitor.Model) {
	rels, err := l.monitorTagService.FindByMonitorID(ctx, m.ID)
	if err != nil {
		l.logger.Errorf("Failed to get tags for monitor %s, notifying without them: %v", m.ID, err)
		return
	}

	names := make([]string, 0, len(rels))
	for _, rel := range rels {
		t, err := l.tagService.FindByID(ctx, rel.TagID)
		if err != nil {
			l.logger.Errorf("Failed to get tag %s for monitor %s, notifying without tags: %v", rel.TagID, m.ID, err)
			return
		}
		if t != nil {
			names = append(names, t.Name)
		}
	}
	sort.Strings(names)
	m.Tags = names
}

// formatCertificateExpiryMessage creates a formatted message for certificate expiry notifications
func (l *NotificationEventListener) formatCertificateExpiryMessage(certEvent *certificate.CertificateExpiryEvent, monitor *monitor.Model) string {
	subjectCN := extractCommonName(certEvent.CertInfo.Subject)

	message := fmt.Sprintf(
		"🚨 Certificate Expiry Warning\n\n"+
			"Monitor: %s\n"+
			"Certificate: %s (%s)\n"+
			"Expires in: %d days\n"+
			"Valid until: %s\n"+
			"Notification threshold: %d days",
		certEvent.MonitorName,
		subjectCN,
		certEvent.CertInfo.CertType,
		certEvent.DaysRemaining,
		certEvent.CertInfo.ValidTo.Format("2006-01-02 15:04:05"),
		certEvent.TargetDays,
	)

	// Add additional certificate details
	if len(certEvent.CertInfo.ValidFor) > 0 {
		message += fmt.Sprintf("\nValid for: %s", certEvent.CertInfo.ValidFor[0])
		if len(certEvent.CertInfo.ValidFor) > 1 {
			message += fmt.Sprintf(" (+%d more)", len(certEvent.CertInfo.ValidFor)-1)
		}
	}

	message += fmt.Sprintf("\nIssuer: %s", certEvent.CertInfo.Issuer)

	return message
}

// extractCommonName extracts the common name from a certificate subject string
func extractCommonName(subject string) string {
	// Simple extraction - in a real implementation you might want to use proper DN parsing
	if idx := strings.Index(subject, "CN="); idx != -1 {
		cn := subject[idx+3:]
		if idx := strings.Index(cn, ","); idx != -1 {
			cn = cn[:idx]
		}
		return cn
	}
	return subject
}
