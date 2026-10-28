package auth

import (
	"context"
	"errors"
)

// ErrAdminExists is returned by CreateFirst when the install already has a user.
var ErrAdminExists = errors.New("admin already exists")

// ErrUserNotFound is for a request about a user that does not exist
var ErrUserNotFound = errors.New("user not found")

// ErrEmailTaken is returned by Register for an email that already has an account, in any case. With
// an invite, that user signs in and accepts the invite instead.
var ErrEmailTaken = errors.New("an account with this email exists: sign in and accept the invite")

type Repository interface {
	Create(ctx context.Context, user *Model) (*Model, error)
	// CreateFirst creates the user only while no user exists, and returns
	// ErrAdminExists otherwise. The check and the insert are one step, so of any
	// number of concurrent calls on a new install exactly one creates its user.
	CreateFirst(ctx context.Context, user *Model) (*Model, error)
	FindByEmail(ctx context.Context, email string) (*Model, error)
	// FindByEmailIgnoringCase finds a user whose email is this one once both are trimmed and
	// lower-cased, or exactly this one. Accounts keep the case their owner typed.
	FindByEmailIgnoringCase(ctx context.Context, email string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAllCount(ctx context.Context) (int64, error)
	Update(ctx context.Context, id string, entity *UpdateModel) error
}
