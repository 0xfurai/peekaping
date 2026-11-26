package api_key

import (
	"context"
	"errors"

	"peekaping/internal/modules/workspace"
)

// ErrNotFound is for a key that the workspace doesn't have, whether another workspace has it or
// no workspace does
var ErrNotFound = errors.New("API key not found")

type Repository interface {
	Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error)
	// FindAll, FindByID, Update and Delete reach only the keys of the workspace. Update and Delete
	// return ErrNotFound for a key it doesn't have.
	FindAll(ctx context.Context, workspaceID workspace.ID) ([]*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	Update(ctx context.Context, workspaceID workspace.ID, id string, update *UpdateModel) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
	// FindByIDAnyWorkspace finds the key a token names, whatever workspace it is in. Only the
	// check of a request's key calls it: the key is what says which workspace the request is in.
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	// ConsumeUsage counts one use of the key and sets its last used time, unless
	// the key has used up its max usage count. It reports whether the use was
	// counted, so a use is checked against the limit and counted in one step.
	ConsumeUsage(ctx context.Context, id string) (bool, error)
	UpdateKeyHash(ctx context.Context, id string, keyHash string, displayKey string) error
}
