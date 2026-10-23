package probe

import "context"

// Repository stores probes. The models it returns hold the probe's own columns: the service adds
// the notification channels, and the instances through FindInstances.
type Repository interface {
	Create(ctx context.Context, probe *CreateModel) (*Model, error)
	FindAll(ctx context.Context) ([]*Model, error)
	// FindByID returns nil when there is no such probe
	FindByID(ctx context.Context, id string) (*Model, error)
	// FindByTokenHash returns nil when no probe has this token hash
	FindByTokenHash(ctx context.Context, tokenHash string) (*Model, error)
	// Update returns nil when there is no such probe
	Update(ctx context.Context, id string, update *UpdateModel) (*Model, error)
	// UpdateToken replaces the probe's token hash and prefix, so the old token finds nothing
	UpdateToken(ctx context.Context, id string, tokenHash string, tokenPrefix string) error
	// Delete removes the probe, with its instances and its notification channel links
	Delete(ctx context.Context, id string) error
	// FindInstances returns the probe's instances, the most recently seen first. Nothing writes
	// them yet: the probe protocol (#252) registers them.
	FindInstances(ctx context.Context, probeID string) ([]*Instance, error)
}
