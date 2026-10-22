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
)

// NotificationEventListener handles notification events
type NotificationEventListener struct {
	service                    Service
	monitorSvc                 monitor.Service
	heartbeatService           heartbeat.Service
	monitorNotificationService monitor_notification.Service
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
}

// staleTimeFormat is how stale-monitor notifications show the time of the last check
const staleTimeFormat = "2006-01-02 15:04:05 UTC"

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
	}
	// A stale-monitor notification carries no heartbeat
	l.sendToChannels(ctx, notificationChannels, message, monitorModel, nil)
}

func (l *NotificationEventListener) handleNotifyEvent(ctx context.Context, hb heartbeat.Model) {
	l.logger.Infof("Notification event received for monitor: %s", hb.MonitorID)

	notificationChannels, monitorModel := l.findChannels(ctx, hb.MonitorID)
	if monitorModel == nil {
		return
	}

	l.sendToChannels(ctx, notificationChannels, hb.Msg, monitorModel, &hb)
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

	var notificationChannels []*Model
	for _, mn := range monitorNotifications {
		l.logger.Infof("Monitor notification: %s", mn.NotificationID)
		notification, err := l.service.FindByID(ctx, mn.NotificationID)
		if err != nil {
			l.logger.Errorf("Failed to get notification by ID: %s, error: %v", mn.NotificationID, err)
			continue
		}
		if notification != nil {
			notificationChannels = append(notificationChannels, notification)
		} else {
			l.logger.Warnf("Notification not found for monitor-notification: %s", mn.NotificationID)
		}
	}

	// Fetch monitor details for context
	monitorModel, err := l.monitorSvc.FindByID(ctx, monitorID)
	if err != nil || monitorModel == nil {
		l.logger.Warnf("Monitor not found for notification context: %s", monitorID)
		return nil, nil
	}
	l.loadMonitorTags(ctx, monitorModel)

	return notificationChannels, monitorModel
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
