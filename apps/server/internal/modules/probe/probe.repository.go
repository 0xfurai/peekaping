package probe

import (
	"context"
	"time"
)

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
	// FindInstances returns the probe's instances, the most recently seen first
	FindInstances(ctx context.Context, probeID string) ([]*Instance, error)
	// UpsertInstance records an instance of the probe, or updates every column of the one the
	// id already names. It returns ErrInstanceTaken, and changes nothing, when that instance
	// belongs to another probe.
	UpsertInstance(ctx context.Context, probeID string, inst *Instance) error
	// TouchInstance sets the last seen time of the probe's instance. It returns false when the
	// probe has no such instance.
	TouchInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) (bool, error)
	// DeleteInstance removes the probe's instance. An instance that isn't there is not an error.
	DeleteInstance(ctx context.Context, probeID string, instanceID string) error
	// SetSupportedTypes replaces the monitor types the probe reports it can run. Nil means none.
	SetSupportedTypes(ctx context.Context, probeID string, types []string) error
}
