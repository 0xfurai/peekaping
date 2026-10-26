package shared

import (
	"context"
	"time"
)

// Keys of the JWT signing secrets. Releases before the settings API was
// limited to user-editable keys served any key matching ^[A-Z0-9_]+$, so these
// contain a dot to stay out of reach of a pod from such a release, even on a
// case-insensitive database. They replace ACCESS_TOKEN_SECRET_KEY and
// REFRESH_TOKEN_SECRET_KEY, which those releases served and which may have
// been read; those rows are left unused.
const (
	AccessTokenSecretKey  = "auth.access_token_secret"
	RefreshTokenSecretKey = "auth.refresh_token_secret"
)

// TelemetryNoticeSeenKey holds "true" once the one-time notice about the usage ping and error
// reports is out of the way: the admin dismissed it, or made a choice for either feature. Until
// then an install that upgraded shows the notice. It is not user-editable, so the settings API can't
// reach it: the telemetry API is the only way to read or change it.
const TelemetryNoticeSeenKey = "TELEMETRY_NOTICE_SEEN"

type SettingService interface {
	GetByKey(ctx context.Context, key string) (*SettingModel, error)
	SetByKey(ctx context.Context, key string, entity *SettingModel) (*SettingModel, error)
	// CreateIfAbsent stores entity under key unless the key is already stored, and returns what is
	// stored under the key afterwards. Callers that race to create the same key all get the one
	// value that won, which SetByKey (update, then insert) can't promise.
	CreateIfAbsent(ctx context.Context, key string, entity *SettingModel) (*SettingModel, error)
	DeleteByKey(ctx context.Context, key string) error
	InitializeSettings(ctx context.Context) error
}

type SettingModel struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
