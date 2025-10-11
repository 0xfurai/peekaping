package api_key

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error)
	FindAll(ctx context.Context) ([]*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Update(ctx context.Context, id string, update *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id string) error
	UpdateKeyHash(ctx context.Context, id string, keyHash string, displayKey string) error
}
