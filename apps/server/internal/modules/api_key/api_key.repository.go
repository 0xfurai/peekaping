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
	// ConsumeUsage counts one use of the key and sets its last used time, unless
	// the key has used up its max usage count. It reports whether the use was
	// counted, so a use is checked against the limit and counted in one step.
	ConsumeUsage(ctx context.Context, id string) (bool, error)
	UpdateKeyHash(ctx context.Context, id string, keyHash string, displayKey string) error
}
