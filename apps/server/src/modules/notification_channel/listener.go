package notification_channel

import (
	"context"
	"fmt"
	"peekaping/src/config"
	"peekaping/src/modules/certificate"
	"peekaping/src/modules/events"
	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor"
	"peekaping/src/modules/monitor_notification"
	"peekaping/src/modules/notification_channel/providers"
	"strings"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// NotificationEventListener handles notification events
type NotificationEventListener struct {
	service                    Service
	monitorSvc                 monitor.Service
	heartbeatService           heartbeat.Service
	monitorNotificationService monitor_notification.Service
	logger                     *zap.SugaredLogger
}

type NotificationEventListenerParams struct {
	dig.In
	Service                    Service
	MonitorSvc                 monitor.Service
	HeartbeatService           heartbeat.Service
	MonitorNotificationService monitor_notification.Service
	Logger                     *zap.SugaredLogger
	Config                     *config.Config
}

func NewNotificationEventListener(p NotificationEventListenerParams) *NotificationEventListener {
	RegisterNotificationChannelProvider("smtp", providers.NewEmailSender(p.Logger))
	RegisterNotificationChannelProvider("telegram", providers.NewTelegramSender(p.Logger))
	RegisterNotificationChannelProvider("webhook", providers.NewWebhookSender(p.Logger))
	RegisterNotificationChannelProvider("slack", providers.NewSlackSender(p.Logger, p.Config))
	RegisterNotificationChannelProvider("ntfy", providers.NewNTFYSender(p.Logger))
	RegisterNotificationChannelProvider("pagerduty", providers.NewPagerDutySender(p.Logger, p.Config))
	RegisterNotificationChannelProvider("opsgenie", providers.NewOpsgenieSender(p.Logger))
	RegisterNotificationChannelProvider("google_chat", providers.NewGoogleChatSender(p.Logger, p.Config))
	RegisterNotificationChannelProvider("grafana_oncall", providers.NewGrafanaOncallSender(p.Logger))
	RegisterNotificationChannelProvider("signal", providers.NewSignalSender(p.Logger))
	RegisterNotificationChannelProvider("gotify", providers.NewGotifySender(p.Logger))
	RegisterNotificationChannelProvider("pushover", providers.NewPushoverSender(p.Logger))
	RegisterNotificationChannelProvider("mattermost", providers.NewMattermostSender(p.Logger))
	RegisterNotificationChannelProvider("matrix", providers.NewMatrixSender(p.Logger))
	RegisterNotificationChannelProvider("discord", providers.NewDiscordSender(p.Logger))
	RegisterNotificationChannelProvider("wecom", providers.NewWeComSender(p.Logger))
	RegisterNotificationChannelProvider("whatsapp", providers.NewWhatsAppSender(p.Logger))
  RegisterNotificationChannelProvider("twilio", providers.NewTwilioSender(p.Logger))

	return &NotificationEventListener{
		service:                    p.Service,
		monitorSvc:                 p.MonitorSvc,
		heartbeatService:           p.HeartbeatService,
		monitorNotificationService: p.MonitorNotificationService,
		logger:                     p.Logger,
	}
}

// Subscribe subscribes to NotifyEvent and sends notifications
func (l *NotificationEventListener) Subscribe(eventBus *events.EventBus) {
	eventBus.Subscribe(events.MonitorStatusChanged, l.handleNotifyEvent)
	eventBus.Subscribe(events.CertificateExpiry, l.handleCertificateExpiryEvent)
}

func (l *NotificationEventListener) handleNotifyEvent(event events.Event) {
	ctx := context.Background()

	hb, ok := event.Payload.(*heartbeat.Model)
	if !ok {
		l.logger.Errorf("Invalid handleNotifyEvent event payload type: %v", event.Payload)
		return
	}

	monitorID := hb.MonitorID

	l.logger.Infof("Notification event received for monitor: %s", monitorID)

	// Get monitor-notification records
	monitorNotifications, err := l.monitorNotificationService.FindByMonitorID(ctx, monitorID)
	if err != nil {
		l.logger.Errorf("Failed to get monitor-notification records: %v", err)
		return
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
		l.logger.Warn("Monitor not found for notification context")
		return
	}

	for _, notificationChannel := range notificationChannels {
		integration, ok := GetNotificationChannelProvider(notificationChannel.Type)
		if !ok {
			l.logger.Warnf("No integration registered for notification type: %s", notificationChannel.Type)
			continue
		}
		if notificationChannel.Config == nil {
			l.logger.Warnf("No config for notification: %s", notificationChannel.Name)
			continue
		}

		// validate config
		if err := integration.Validate(*notificationChannel.Config); err != nil {
			l.logger.Errorf("Failed to validate notification config: %s, error: %v", notificationChannel.Name, err)
			continue
		}

		err := integration.Send(ctx, *notificationChannel.Config, hb.Msg, monitorModel, hb)
		if err != nil {
			l.logger.Errorf("Failed to send notification: %s, error: %v", notificationChannel.Name, err)
		} else {
			l.logger.Infof("Notification sent to: %s for monitor: %s", notificationChannel.Name, monitorID)
		}
	}
}

func (l *NotificationEventListener) handleCertificateExpiryEvent(event events.Event) {
	ctx := context.Background()

	certEvent, ok := event.Payload.(*certificate.CertificateExpiryEvent)
	if !ok {
		l.logger.Errorf("Invalid certificate expiry event payload type: %v", event.Payload)
		return
	}

	l.logger.Infof("Certificate expiry event received for monitor: %s", certEvent.MonitorID)

	// Get monitor-notification records
	monitorNotifications, err := l.monitorNotificationService.FindByMonitorID(ctx, certEvent.MonitorID)
	if err != nil {
		l.logger.Errorf("Failed to get monitor-notification records: %v", err)
		return
	}

	if len(monitorNotifications) == 0 {
		l.logger.Debugf("No notification channels configured for monitor %s", certEvent.MonitorID)
		return
	}

	// Get notification channels
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
	monitorModel, err := l.monitorSvc.FindByID(ctx, certEvent.MonitorID)
	if err != nil || monitorModel == nil {
		l.logger.Warn("Monitor not found for certificate expiry notification context")
		return
	}

	// Send notifications through all configured channels
	for _, notificationChannel := range notificationChannels {
		integration, ok := GetNotificationChannelProvider(notificationChannel.Type)
		if !ok {
			l.logger.Warnf("No integration registered for notification type: %s", notificationChannel.Type)
			continue
		}
		if notificationChannel.Config == nil {
			l.logger.Warnf("No config for notification: %s", notificationChannel.Name)
			continue
		}

		// Validate config
		if err := integration.Validate(*notificationChannel.Config); err != nil {
			l.logger.Errorf("Failed to validate notification config: %s, error: %v", notificationChannel.Name, err)
			continue
		}

		// Create a formatted message for certificate expiry
		message := l.formatCertificateExpiryMessage(certEvent, monitorModel)

		// Send notification (we pass nil for heartbeat since this is a certificate expiry notification)
		err := integration.Send(ctx, *notificationChannel.Config, message, monitorModel, nil)
		if err != nil {
			l.logger.Errorf("Failed to send certificate expiry notification: %s, error: %v", notificationChannel.Name, err)
		} else {
			l.logger.Infof("Certificate expiry notification sent to: %s for monitor: %s", notificationChannel.Name, certEvent.MonitorID)
		}
	}
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
