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
	// FindInstance returns the probe's instance with the id, or nil when the probe has none
	FindInstance(ctx context.Context, probeID string, instanceID string) (*Instance, error)
	// UpsertInstance records an instance of the probe, or updates every column of the one the
	// id already names. It returns ErrInstanceTaken, and changes nothing, when that instance
	// belongs to another probe. The hello is a request of the instance, so it extends the
	// instance's connection or starts one, in the same transaction (see TouchInstance).
	UpsertInstance(ctx context.Context, probeID string, inst *Instance) error
	// MakeRoomForInstance leaves the probe room for one more instance, called inside the
	// transaction that then upserts it: when the probe holds limit instances or more, it deletes
	// the oldest ones last seen before quietBefore, as many as it takes. It returns
	// ErrTooManyInstances, and deletes nothing, when there are not that many to delete.
	MakeRoomForInstance(ctx context.Context, probeID string, instanceID string, limit int, quietBefore time.Time) error
	// TouchInstance sets the last seen time of the probe's instance. It returns false when the
	// probe has no such instance. In the same transaction, the one ctx carries or one of its own, it
	// extends the instance's open connection when that was seen within InstanceOnlineWindow of
	// seenAt, and starts a new connection otherwise.
	TouchInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) (bool, error)
	// RetireInstance sets the last seen time of the probe's instance, as TouchInstance does, but
	// for an instance that is stopping: seenAt is a time outside the online window, which takes
	// the instance out of the dealing at once and leaves the row for the sweep to prune. An
	// instance that isn't there is not an error. endedAt is the time of the bye itself, which ends
	// the instance's open connection: seenAt is a made up time, and the connection is not
	// to end at it.
	RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time, endedAt time.Time) error
	// SetSupportedTypes replaces the monitor types the probe reports it can run. Nil means none.
	SetSupportedTypes(ctx context.Context, probeID string, types []string) error
	// UpdateStatus moves the probe from one status to another in a single conditional update. It
	// returns true when the probe was in the status from and is now in to, and false when it was
	// not (or does not exist): of two callers racing to make the same change, one hears true. A
	// disabled probe can be set offline but not online, since nothing can have heard from it: its
	// token is refused, and a request that began before an admin disabled it must not bring it back.
	//
	// at is when the status really changed, which is recorded in the probe's history: in the same
	// transaction as the update, the one ctx carries or one of its own, and only when the update
	// changed the probe. A time before the probe's latest recorded change is recorded as that
	// change's time, so that the probe's periods never run backwards.
	UpdateStatus(ctx context.Context, id string, from Status, to Status, at time.Time) (bool, error)
	// FindStatusChanges returns the probe's history from since on: its latest change at or before
	// since, which is the status it carries into the range (none when the probe has no change that
	// old), then every change after since. They are in the order they happened.
	FindStatusChanges(ctx context.Context, probeID string, since time.Time) ([]*StatusChange, error)
	// DeleteStatusChangesOlderThan removes the changes before the given time, except each probe's
	// latest one before it, so that a probe's status at the start of any range after it is still
	// known. It returns how many it removed.
	DeleteStatusChangesOlderThan(ctx context.Context, before time.Time) (int, error)
	// FindInstanceConnections returns the connections of the probe's instances that were last
	// seen, or ended, at or after since, ordered by instance and then by when they started. Those
	// of an instance that was forgotten (see DeleteInstancesUnseenSince) are gone with it.
	FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*InstanceConnection, error)
	// DeleteInstancesUnseenSince removes the instances of every probe that were last seen before
	// the given time, with their connections, and returns how many instances it removed
	DeleteInstancesUnseenSince(ctx context.Context, before time.Time) (int, error)
}
