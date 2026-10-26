package setting

import "context"

type Repository interface {
	GetByKey(ctx context.Context, key string) (*Model, error)
	SetByKey(ctx context.Context, key string, entity *Model) (*Model, error)
	// CreateIfAbsent stores entity under key unless the key is already stored, in one statement, and
	// returns what is stored under the key afterwards: the new row, or the one that was there first.
	// Callers that race to create the same key, such as api replicas starting together, all get the
	// one value that won.
	CreateIfAbsent(ctx context.Context, key string, entity *Model) (*Model, error)
	DeleteByKey(ctx context.Context, key string) error
}
