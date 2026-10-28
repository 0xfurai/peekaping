package auth

import (
	"context"
	"errors"
)

// ErrAdminExists is returned by CreateFirst when the install already has a user.
var ErrAdminExists = errors.New("admin already exists")

// ErrEmailTaken is returned by Register for an invite whose email already has an account: that
// user signs in and opens the link instead.
var ErrEmailTaken = errors.New("an account with this email exists already: sign in and open the invite link")

type Repository interface {
	Create(ctx context.Context, user *Model) (*Model, error)
	// CreateFirst creates the user only while no user exists, and returns
	// ErrAdminExists otherwise. The check and the insert are one step, so of any
	// number of concurrent calls on a new install exactly one creates its user.
	CreateFirst(ctx context.Context, user *Model) (*Model, error)
	FindByEmail(ctx context.Context, email string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAllCount(ctx context.Context) (int64, error)
	Update(ctx context.Context, id string, entity *UpdateModel) error
}
