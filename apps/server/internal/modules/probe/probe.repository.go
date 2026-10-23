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
	// Delete removes the probe, with its instances and its notification channel links. A probe that
	// monitors run on stays: the database refuses to delete it, and Delete returns
	// ErrProbeHasMonitors, without the count.
	Delete(ctx context.Context, id string) error
	// CountMonitors returns how many monitors, paused ones included, run on the probe
	CountMonitors(ctx context.Context, id string) (int, error)
	// FindInstances returns the probe's instances, the most recently seen first
	FindInstances(ctx context.Context, probeID string) ([]*Instance, error)
	// UpsertInstance records an instance of the probe, or updates every column of the one the
	// id already names. It returns ErrInstanceTaken, and changes nothing, when that instance
	// belongs to another probe.
	UpsertInstance(ctx context.Context, probeID string, inst *Instance) error
	// TouchInstance sets the last seen time of the probe's instance. It returns false when the
	// probe has no such instance.
	TouchInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) (bool, error)
	// RetireInstance sets the last seen time of the probe's instance, as TouchInstance does, but
	// for an instance that is stopping: seenAt is a time outside the online window, which takes
	// the instance out of the dealing at once and leaves the row for the sweep to prune. An
	// instance that isn't there is not an error.
	RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) error
	// SetSupportedTypes replaces the monitor types the probe reports it can run. Nil means none.
	SetSupportedTypes(ctx context.Context, probeID string, types []string) error
	// UpdateStatus moves the probe from one status to another in a single conditional update. It
	// returns true when the probe was in the status from and is now in to, and false when it was
	// not (or does not exist): of two callers racing to make the same change, one hears true. A
	// disabled probe can be set offline but not online, since nothing can have heard from it: its
	// token is refused, and a request that began before an admin disabled it must not bring it back.
	UpdateStatus(ctx context.Context, id string, from Status, to Status) (bool, error)
	// DeleteInstancesUnseenSince removes the instances of every probe that were last seen before
	// the given time, and returns how many it removed
	DeleteInstancesUnseenSince(ctx context.Context, before time.Time) (int, error)
}
