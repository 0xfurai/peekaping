package setting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"peekaping/internal/modules/shared"
	"strconv"

	"go.uber.org/zap"
)

type Service = shared.SettingService

// DataRetentionPeriodKey holds the data retention period: how many days
// heartbeats are kept.
const DataRetentionPeriodKey = "KEEP_DATA_PERIOD_DAYS"

// The data retention periods the settings API accepts. Zero or less would
// delete every heartbeat, and so would a period so long that the cutoff date
// overflows into the future.
const (
	MinDataRetentionDays = 1
	MaxDataRetentionDays = 36500
)

// userEditableKeys are the only settings the settings API can reach. Every
// other key, such as the JWT signing secrets, is internal to the server.
var userEditableKeys = map[string]bool{
	DataRetentionPeriodKey: true,
}

// ErrSettingNotFound is returned when the settings API asks to change or
// delete a key that isn't user-editable.
var ErrSettingNotFound = errors.New("setting not found")

// ErrInvalidValue is returned when the settings API asks to store a value its
// key can't hold.
var ErrInvalidValue = errors.New("invalid setting value")

// ParseDataRetentionPeriod returns the days a data retention period value holds,
// or ErrInvalidValue if it isn't a whole number from MinDataRetentionDays to
// MaxDataRetentionDays.
func ParseDataRetentionPeriod(value string) (int, error) {
	days, err := strconv.Atoi(value)
	if err != nil || days < MinDataRetentionDays || days > MaxDataRetentionDays {
		return 0, fmt.Errorf("%w: %s must be a whole number of days from %d to %d", ErrInvalidValue, DataRetentionPeriodKey, MinDataRetentionDays, MaxDataRetentionDays)
	}
	return days, nil
}

// APIService is the settings service as the settings API sees it: keys that
// aren't user-editable behave as if they don't exist.
type APIService interface {
	IsUserEditable(key string) bool
	GetByKey(ctx context.Context, key string) (*Model, error)
	SetByKey(ctx context.Context, key string, entity *Model) (*Model, error)
	DeleteByKey(ctx context.Context, key string) error
}

type APIServiceImpl struct {
	service Service
}

func NewAPIService(service Service) APIService {
	return &APIServiceImpl{service}
}

// IsUserEditable reports whether the settings API can reach key.
func (s *APIServiceImpl) IsUserEditable(key string) bool {
	return userEditableKeys[key]
}

func (s *APIServiceImpl) GetByKey(ctx context.Context, key string) (*Model, error) {
	if !userEditableKeys[key] {
		return nil, nil
	}
	return s.service.GetByKey(ctx, key)
}

func (s *APIServiceImpl) SetByKey(ctx context.Context, key string, entity *Model) (*Model, error) {
	if !userEditableKeys[key] {
		return nil, ErrSettingNotFound
	}
	if key == DataRetentionPeriodKey {
		if entity.Type != "int" {
			return nil, fmt.Errorf("%w: %s must have type int", ErrInvalidValue, key)
		}
		if _, err := ParseDataRetentionPeriod(entity.Value); err != nil {
			return nil, err
		}
	}
	return s.service.SetByKey(ctx, key, entity)
}

func (s *APIServiceImpl) DeleteByKey(ctx context.Context, key string) error {
	if !userEditableKeys[key] {
		return ErrSettingNotFound
	}
	return s.service.DeleteByKey(ctx, key)
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		logger.Named("[setting-service]"),
	}
}

func (mr *ServiceImpl) GetByKey(ctx context.Context, key string) (*Model, error) {
	return mr.repository.GetByKey(ctx, key)
}

func (mr *ServiceImpl) SetByKey(ctx context.Context, key string, entity *Model) (*Model, error) {
	return mr.repository.SetByKey(ctx, key, entity)
}

func (mr *ServiceImpl) CreateIfAbsent(ctx context.Context, key string, entity *Model) (*Model, error) {
	return mr.repository.CreateIfAbsent(ctx, key, entity)
}

func (mr *ServiceImpl) DeleteByKey(ctx context.Context, key string) error {
	return mr.repository.DeleteByKey(ctx, key)
}

func (mr *ServiceImpl) InitializeSettings(ctx context.Context) error {
	mr.logger.Info("Initializing settings...")

	// Initialize access token expiration (15 minutes default)
	if err := mr.initializeDefaultSetting(ctx, "ACCESS_TOKEN_EXPIRED_IN", "15m", "string"); err != nil {
		return fmt.Errorf("failed to initialize access token expiration: %w", err)
	}

	// Initialize refresh token expiration (720 hours / 30 days default)
	if err := mr.initializeDefaultSetting(ctx, "REFRESH_TOKEN_EXPIRED_IN", "720h", "string"); err != nil {
		return fmt.Errorf("failed to initialize refresh token expiration: %w", err)
	}

	// Initialize access token secret key
	if err := mr.initializeSecretKey(ctx, shared.AccessTokenSecretKey); err != nil {
		return fmt.Errorf("failed to initialize access token secret key: %w", err)
	}

	// Initialize refresh token secret key
	if err := mr.initializeSecretKey(ctx, shared.RefreshTokenSecretKey); err != nil {
		return fmt.Errorf("failed to initialize refresh token secret key: %w", err)
	}

	// Initialize certificate expiry notification days (7, 14, 21 days default)
	if err := mr.initializeDefaultSetting(ctx, "cert_expiry_notify_days", "[7,14,21]", "json"); err != nil {
		return fmt.Errorf("failed to initialize certificate expiry notification days: %w", err)
	}

	mr.logger.Info("Settings initialized successfully")
	return nil
}

// Helper method to initialize a setting with a default value if it doesn't exist
func (mr *ServiceImpl) initializeDefaultSetting(ctx context.Context, key, defaultValue, settingType string) error {
	existing, err := mr.repository.GetByKey(ctx, key)
	if err != nil {
		return err
	}

	if existing == nil {
		_, err = mr.repository.SetByKey(ctx, key, &Model{
			Value: defaultValue,
			Type:  settingType,
		})
		if err != nil {
			return err
		}
		mr.logger.Infof("Created default setting %s=%s", key, defaultValue)
	}

	return nil
}

// Helper method to initialize a secret key if it doesn't exist
func (mr *ServiceImpl) initializeSecretKey(ctx context.Context, key string) error {
	existing, err := mr.repository.GetByKey(ctx, key)
	if err != nil {
		return err
	}

	if existing == nil || existing.Value == "" {
		secretKey, err := mr.generateSecretKey()
		if err != nil {
			return err
		}

		_, err = mr.repository.SetByKey(ctx, key, &Model{
			Value: secretKey,
			Type:  "string",
		})
		if err != nil {
			return err
		}
		mr.logger.Infof("Generated secure secret key for %s", key)
	}

	return nil
}

// Generate a cryptographically secure 32-byte (256-bit) secret key
func (mr *ServiceImpl) generateSecretKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
