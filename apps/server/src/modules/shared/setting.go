package shared

import (
	"context"
	"time"
)

type SettingService interface {
	GetByKey(ctx context.Context, key string) (*SettingModel, error)
	SetByKey(ctx context.Context, key string, entity *SettingCreateUpdateDto) (*SettingModel, error)
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

type SettingCreateUpdateDto struct {
	Value string `json:"value"`
	Type  string `json:"type" validate:"required,oneof=string int bool json"`
}
