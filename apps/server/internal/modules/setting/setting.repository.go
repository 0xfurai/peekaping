package setting

import "context"

type Repository interface {
	GetByKey(ctx context.Context, key string) (*Model, error)
	SetByKey(ctx context.Context, key string, entity *CreateUpdateDto) (*Model, error)
	DeleteByKey(ctx context.Context, key string) error
}
