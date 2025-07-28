package certificate

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"peekaping/src/modules/events"
	"peekaping/src/modules/monitor_tls_info"
	"peekaping/src/modules/notification_sent_history"
	"peekaping/src/modules/shared"
	"strings"

	"go.uber.org/zap"
)

type Service interface {
	CheckCertificateExpiry(ctx context.Context, tlsInfo *TLSInfo, monitorID string, monitorName string) error
	UpdateTLSInfo(ctx context.Context, monitorID string, tlsInfo *TLSInfo) error
	GetNotificationDays(ctx context.Context) ([]int, error)
	SetNotificationDays(ctx context.Context, days []int) error
}

type ServiceImpl struct {
	settingService             shared.SettingService
	notificationService        NotificationService
	notificationHistoryService notification_sent_history.Service
	tlsInfoService             monitor_tls_info.Service
	logger                     *zap.SugaredLogger
}

// NotificationService interface for sending certificate expiry notifications
type NotificationService interface {
	SendCertificateExpiryNotification(ctx context.Context, monitorID string, monitorName string, certInfo *CertificateInfo, daysRemaining int, targetDays int) error
}

func NewService(
	settingService shared.SettingService,
	notificationService NotificationService,
	notificationHistoryService notification_sent_history.Service,
	tlsInfoService monitor_tls_info.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		settingService:             settingService,
		notificationService:        notificationService,
		notificationHistoryService: notificationHistoryService,
		tlsInfoService:             tlsInfoService,
		logger:                     logger.Named("[certificate-service]"),
	}
}

// CheckCertificateExpiry checks certificate expiry and sends notifications if needed
func (s *ServiceImpl) CheckCertificateExpiry(ctx context.Context, tlsInfo *TLSInfo, monitorID string, monitorName string) error {
	if tlsInfo == nil || tlsInfo.CertInfo == nil {
		s.logger.Debug("No TLS info or certificate info available")
		return nil
	}

	// Get notification days from settings
	notifyDays, err := s.GetNotificationDays(ctx)
	if err != nil {
		s.logger.Errorf("Failed to get notification days: %v", err)
		return err
	}

	if len(notifyDays) == 0 {
		s.logger.Debug("No notification days configured, skipping certificate expiry check")
		return nil
	}

	// Walk through the certificate chain
	certInfo := tlsInfo.CertInfo
	for certInfo != nil {
		if err := s.checkSingleCertificate(ctx, certInfo, monitorID, monitorName, notifyDays); err != nil {
			s.logger.Errorf("Failed to check certificate %s: %v", certInfo.Subject, err)
		}
		certInfo = certInfo.IssuerCertificate
	}

	return nil
}

// checkSingleCertificate checks a single certificate for expiry with notification deduplication
func (s *ServiceImpl) checkSingleCertificate(ctx context.Context, certInfo *CertificateInfo, monitorID string, monitorName string, notifyDays []int) error {
	// Skip root certificates as they're typically managed by the system
	if IsRootCertificate(certInfo.Fingerprint256) {
		s.logger.Debugf("Skipping root certificate: %s", certInfo.Subject)
		return nil
	}

	subjectCN := extractCommonName(certInfo.Subject)

	// Check each notification threshold with deduplication
	for _, targetDays := range notifyDays {
		if certInfo.DaysRemaining <= targetDays && certInfo.DaysRemaining >= 0 {
			// Check if we already sent a notification for this threshold
			alreadySent, err := s.notificationHistoryService.CheckIfNotificationSent(ctx, "certificate", monitorID, targetDays)
			if err != nil {
				s.logger.Errorf("Failed to check notification history: %v", err)
				continue
			}

			if alreadySent {
				s.logger.Debugf("Certificate notification already sent for monitor %s, threshold %d days", monitorID, targetDays)
				continue
			}

			s.logger.Infof("Sending certificate expiry notification: %s expires in %d days (threshold: %d)", subjectCN, certInfo.DaysRemaining, targetDays)

			// Send notification
			if err := s.notificationService.SendCertificateExpiryNotification(
				ctx, monitorID, monitorName, certInfo, certInfo.DaysRemaining, targetDays,
			); err != nil {
				s.logger.Errorf("Failed to send certificate expiry notification: %v", err)
				continue
			}

			// Record that we sent the notification
			if err := s.notificationHistoryService.RecordNotificationSent(ctx, "certificate", monitorID, targetDays); err != nil {
				s.logger.Errorf("Failed to record notification sent: %v", err)
			}
		}
	}

	return nil
}

// UpdateTLSInfo updates TLS info and handles certificate changes (clears notification history if cert changed)
func (s *ServiceImpl) UpdateTLSInfo(ctx context.Context, monitorID string, tlsInfo *TLSInfo) error {
	// Get previous TLS info from settings to compare certificate fingerprints
	previousTLSInfo, err := s.getPreviousTLSInfo(ctx, monitorID)
	if err != nil {
		s.logger.Errorf("Failed to get previous TLS info: %v", err)
		// Continue anyway, just don't clear history
	}

	// Check if certificate has changed (different fingerprint)
	if previousTLSInfo != nil && previousTLSInfo.CertInfo != nil &&
		tlsInfo != nil && tlsInfo.CertInfo != nil {
		if previousTLSInfo.CertInfo.Fingerprint256 != tlsInfo.CertInfo.Fingerprint256 {
			s.logger.Infof("Certificate changed for monitor %s, clearing notification history", monitorID)
			// Clear notification history for certificate type
			if err := s.notificationHistoryService.ClearNotificationHistory(ctx, monitorID, "certificate"); err != nil {
				s.logger.Errorf("Failed to clear notification history: %v", err)
			}
		}
	}

	// Store the new TLS info
	return s.storeTLSInfo(ctx, monitorID, tlsInfo)
}

// getPreviousTLSInfo retrieves previously stored TLS info for a monitor
func (s *ServiceImpl) getPreviousTLSInfo(ctx context.Context, monitorID string) (*TLSInfo, error) {
	var tlsInfo TLSInfo
	err := s.tlsInfoService.GetTLSInfoObject(ctx, monitorID, &tlsInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS info: %w", err)
	}

	// Check if we got any data (the service returns nil error even if no data found)
	if tlsInfo.CertInfo == nil {
		return nil, nil // No previous info
	}

	return &tlsInfo, nil
}

// storeTLSInfo stores TLS info for a monitor
func (s *ServiceImpl) storeTLSInfo(ctx context.Context, monitorID string, tlsInfo *TLSInfo) error {
	err := s.tlsInfoService.StoreTLSInfoObject(ctx, monitorID, tlsInfo)
	if err != nil {
		return fmt.Errorf("failed to store TLS info: %w", err)
	}

	return nil
}

// GetNotificationDays retrieves the certificate expiry notification days from settings
func (s *ServiceImpl) GetNotificationDays(ctx context.Context) ([]int, error) {
	const settingKey = "cert_expiry_notify_days"

	setting, err := s.settingService.GetByKey(ctx, settingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate notification days setting: %w", err)
	}

	// If setting doesn't exist, initialize with default values
	if setting == nil {
		defaultDays := []int{7, 14, 21}
		if err := s.SetNotificationDays(ctx, defaultDays); err != nil {
			s.logger.Errorf("Failed to initialize default notification days: %v", err)
		}
		return defaultDays, nil
	}

	// Parse the JSON array from the setting value
	var days []int
	if err := json.Unmarshal([]byte(setting.Value), &days); err != nil {
		s.logger.Errorf("Failed to parse notification days from setting: %v", err)
		// Return default on parse error
		return []int{7, 14, 21}, nil
	}

	return days, nil
}

// SetNotificationDays sets the certificate expiry notification days in settings
func (s *ServiceImpl) SetNotificationDays(ctx context.Context, days []int) error {
	const settingKey = "cert_expiry_notify_days"

	// Convert to JSON
	jsonData, err := json.Marshal(days)
	if err != nil {
		return fmt.Errorf("failed to marshal notification days: %w", err)
	}

	// Save to settings
	dto := &shared.SettingCreateUpdateDto{
		Value: string(jsonData),
		Type:  "json",
	}

	_, err = s.settingService.SetByKey(ctx, settingKey, dto)
	if err != nil {
		return fmt.Errorf("failed to save notification days setting: %w", err)
	}

	s.logger.Infof("Updated certificate expiry notification days: %v", days)
	return nil
}

// ExtractCertificateFromTLSConn extracts certificate information from a TLS connection
func ExtractCertificateFromTLSConn(conn *tls.Conn) *TLSInfo {
	if conn == nil {
		return &TLSInfo{Valid: false}
	}

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return &TLSInfo{Valid: false}
	}

	// Get the first certificate (server certificate)
	serverCert := state.PeerCertificates[0]

	// Check if the certificate chain is verified
	verified := len(state.VerifiedChains) > 0

	return ParseCertificateChain(serverCert, verified)
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

// Helper function to check if certificate expiry notification is enabled
func (s *ServiceImpl) IsExpiryNotificationEnabled(ctx context.Context) bool {
	days, err := s.GetNotificationDays(ctx)
	if err != nil {
		s.logger.Errorf("Failed to check if expiry notification is enabled: %v", err)
		return false
	}
	return len(days) > 0
}

// CertificateExpiryEvent represents a certificate expiry event payload
type CertificateExpiryEvent struct {
	MonitorID     string           `json:"monitor_id"`
	MonitorName   string           `json:"monitor_name"`
	CertInfo      *CertificateInfo `json:"cert_info"`
	DaysRemaining int              `json:"days_remaining"`
	TargetDays    int              `json:"target_days"`
	Message       string           `json:"message"`
}

// EventBasedNotificationService integrates with the existing notification system via events
type EventBasedNotificationService struct {
	eventBus *events.EventBus
	logger   *zap.SugaredLogger
}

func NewEventBasedNotificationService(eventBus *events.EventBus, logger *zap.SugaredLogger) NotificationService {
	return &EventBasedNotificationService{
		eventBus: eventBus,
		logger:   logger,
	}
}

func (s *EventBasedNotificationService) SendCertificateExpiryNotification(
	ctx context.Context,
	monitorID string,
	monitorName string,
	certInfo *CertificateInfo,
	daysRemaining int,
	targetDays int,
) error {
	subjectCN := extractCommonName(certInfo.Subject)
	message := fmt.Sprintf(
		"Certificate expiry warning: Certificate '%s' (%s) expires in %d days",
		subjectCN,
		certInfo.CertType,
		daysRemaining,
	)

	// Create certificate expiry event
	event := &CertificateExpiryEvent{
		MonitorID:     monitorID,
		MonitorName:   monitorName,
		CertInfo:      certInfo,
		DaysRemaining: daysRemaining,
		TargetDays:    targetDays,
		Message:       message,
	}

	// Publish the event
	s.eventBus.Publish(events.Event{
		Type:    events.CertificateExpiry,
		Payload: event,
	})

	s.logger.Infof("Published certificate expiry event for monitor %s: %s", monitorName, message)
	return nil
}

// SimpleNotificationService is a basic implementation of NotificationService for backward compatibility
type SimpleNotificationService struct {
	logger *zap.SugaredLogger
}

func (s *SimpleNotificationService) SendCertificateExpiryNotification(
	ctx context.Context,
	monitorID string,
	monitorName string,
	certInfo *CertificateInfo,
	daysRemaining int,
	targetDays int,
) error {
	subjectCN := extractCommonName(certInfo.Subject)
	message := fmt.Sprintf(
		"Certificate expiry warning for monitor '%s': Certificate '%s' (%s) expires in %d days (threshold: %d days)",
		monitorName,
		subjectCN,
		certInfo.CertType,
		daysRemaining,
		targetDays,
	)

	s.logger.Warnf("Certificate Expiry Notification: %s", message)
	return nil
}
