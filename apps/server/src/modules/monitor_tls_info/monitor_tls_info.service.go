package monitor_tls_info

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
)

type Service interface {
	// GetTLSInfo retrieves TLS info for a monitor
	GetTLSInfo(ctx context.Context, monitorID string) (*TLSInfo, error)

	// StoreTLSInfo stores TLS info for a monitor
	StoreTLSInfo(ctx context.Context, monitorID string, infoJSON string) error

	// StoreTLSInfoObject stores TLS info object as JSON for a monitor
	StoreTLSInfoObject(ctx context.Context, monitorID string, info interface{}) error

	// GetTLSInfoObject retrieves TLS info and unmarshals it into the provided object
	GetTLSInfoObject(ctx context.Context, monitorID string, obj interface{}) error

	// DeleteTLSInfo removes TLS info for a monitor
	DeleteTLSInfo(ctx context.Context, monitorID string) error

	// CleanupOldRecords removes old TLS info records
	CleanupOldRecords(ctx context.Context, olderThanDays int) error
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(repository Repository, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repository: repository,
		logger:     logger.Named("[monitor-tls-info-service]"),
	}
}

func (s *ServiceImpl) GetTLSInfo(ctx context.Context, monitorID string) (*TLSInfo, error) {
	s.logger.Debugf("Getting TLS info for monitor: %s", monitorID)

	model, err := s.repository.GetByMonitorID(ctx, monitorID)
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS info: %w", err)
	}

	if model == nil {
		return nil, nil // No TLS info found
	}

	// Parse the JSON string into TLSInfo struct
	var tlsInfo TLSInfo
	if err := json.Unmarshal([]byte(model.InfoJSON), &tlsInfo); err != nil {
		s.logger.Errorf("Failed to unmarshal TLS info for monitor %s: %v", monitorID, err)
		return nil, fmt.Errorf("failed to unmarshal TLS info: %w", err)
	}

	return &tlsInfo, nil
}

func (s *ServiceImpl) StoreTLSInfo(ctx context.Context, monitorID string, infoJSON string) error {
	s.logger.Debugf("Storing TLS info for monitor: %s", monitorID)

	_, err := s.repository.Upsert(ctx, monitorID, infoJSON)
	if err != nil {
		return fmt.Errorf("failed to store TLS info: %w", err)
	}

	s.logger.Debugf("Successfully stored TLS info for monitor: %s", monitorID)
	return nil
}

func (s *ServiceImpl) StoreTLSInfoObject(ctx context.Context, monitorID string, info interface{}) error {
	s.logger.Debugf("Storing TLS info object for monitor: %s", monitorID)

	// Marshal object to JSON
	jsonData, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal TLS info object: %w", err)
	}

	return s.StoreTLSInfo(ctx, monitorID, string(jsonData))
}

func (s *ServiceImpl) GetTLSInfoObject(ctx context.Context, monitorID string, obj interface{}) error {
	s.logger.Debugf("Getting TLS info object for monitor: %s", monitorID)

	model, err := s.repository.GetByMonitorID(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to get TLS info: %w", err)
	}

	if model == nil {
		return nil // No TLS info found
	}

	// Unmarshal JSON directly to target object
	if err := json.Unmarshal([]byte(model.InfoJSON), obj); err != nil {
		s.logger.Errorf("Failed to unmarshal TLS info for monitor %s: %v", monitorID, err)
		return fmt.Errorf("failed to unmarshal TLS info: %w", err)
	}

	return nil
}

func (s *ServiceImpl) DeleteTLSInfo(ctx context.Context, monitorID string) error {
	s.logger.Debugf("Deleting TLS info for monitor: %s", monitorID)

	err := s.repository.Delete(ctx, monitorID)
	if err != nil {
		return fmt.Errorf("failed to delete TLS info: %w", err)
	}

	s.logger.Debugf("Successfully deleted TLS info for monitor: %s", monitorID)
	return nil
}

func (s *ServiceImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	s.logger.Infof("Cleaning up TLS info records older than %d days", olderThanDays)

	err := s.repository.CleanupOldRecords(ctx, olderThanDays)
	if err != nil {
		return fmt.Errorf("failed to cleanup old TLS info records: %w", err)
	}

	s.logger.Debugf("Successfully cleaned up old TLS info records")
	return nil
}
