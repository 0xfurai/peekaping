package auth

import (
	"context"
)

type Repository interface {
	Create(ctx context.Context, user *Model) (*Model, error)
	FindByEmail(ctx context.Context, email string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAllCount(ctx context.Context) (int64, error)
	Update(ctx context.Context, id string, entity *UpdateModel) error
}
