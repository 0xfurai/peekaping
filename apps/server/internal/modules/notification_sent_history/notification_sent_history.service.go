package notification_sent_history

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

type Service interface {
	// CheckIfNotificationSent checks if a notification was already sent for the given criteria
	CheckIfNotificationSent(ctx context.Context, notificationType string, monitorID string, targetDays int) (bool, error)

	// RecordNotificationSent records that a notification was sent
	RecordNotificationSent(ctx context.Context, notificationType string, monitorID string, targetDays int) error

	// ClearNotificationHistory clears notification history for a specific monitor and type
	ClearNotificationHistory(ctx context.Context, monitorID string, notificationType string) error

	// CleanupOldRecords removes old notification records
	CleanupOldRecords(ctx context.Context, olderThanDays int) error

	// GetNotificationHistory gets all notification history for a monitor and type
	GetNotificationHistory(ctx context.Context, monitorID string, notificationType string) ([]*Model, error)
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(repository Repository, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repository: repository,
		logger:     logger.Named("[notification-sent-history-service]"),
	}
}

func (s *ServiceImpl) CheckIfNotificationSent(ctx context.Context, notificationType string, monitorID string, targetDays int) (bool, error) {
	s.logger.Debugf("Checking if notification sent for monitor %s, type %s, days %d", monitorID, notificationType, targetDays)

	sent, err := s.repository.CheckIfSent(ctx, notificationType, monitorID, targetDays)
	if err != nil {
		return false, fmt.Errorf("failed to check if notification was sent: %w", err)
	}

	if sent {
		s.logger.Debugf("Notification already sent for monitor %s, type %s, days %d", monitorID, notificationType, targetDays)
	} else {
		s.logger.Debugf("No notification sent yet for monitor %s, type %s, days %d", monitorID, notificationType, targetDays)
	}

	return sent, nil
}

func (s *ServiceImpl) RecordNotificationSent(ctx context.Context, notificationType string, monitorID string, targetDays int) error {
	s.logger.Infof("Recording notification sent for monitor %s, type %s, days %d", monitorID, notificationType, targetDays)

	dto := &CreateDto{
		Type:      notificationType,
		MonitorID: monitorID,
		Days:      targetDays,
	}

	err := s.repository.RecordSent(ctx, dto)
	if err != nil {
		return fmt.Errorf("failed to record notification sent: %w", err)
	}

	s.logger.Debugf("Successfully recorded notification sent for monitor %s", monitorID)
	return nil
}

func (s *ServiceImpl) ClearNotificationHistory(ctx context.Context, monitorID string, notificationType string) error {
	s.logger.Infof("Clearing notification history for monitor %s, type %s", monitorID, notificationType)

	err := s.repository.ClearByMonitorAndType(ctx, monitorID, notificationType)
	if err != nil {
		return fmt.Errorf("failed to clear notification history: %w", err)
	}

	s.logger.Debugf("Successfully cleared notification history for monitor %s", monitorID)
	return nil
}

func (s *ServiceImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	s.logger.Infof("Cleaning up notification records older than %d days", olderThanDays)

	err := s.repository.CleanupOldRecords(ctx, olderThanDays)
	if err != nil {
		return fmt.Errorf("failed to cleanup old notification records: %w", err)
	}

	s.logger.Debugf("Successfully cleaned up old notification records")
	return nil
}

func (s *ServiceImpl) GetNotificationHistory(ctx context.Context, monitorID string, notificationType string) ([]*Model, error) {
	s.logger.Debugf("Getting notification history for monitor %s, type %s", monitorID, notificationType)

	history, err := s.repository.GetByMonitorAndType(ctx, monitorID, notificationType)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification history: %w", err)
	}

	s.logger.Debugf("Found %d notification history records for monitor %s", len(history), monitorID)
	return history, nil
}
