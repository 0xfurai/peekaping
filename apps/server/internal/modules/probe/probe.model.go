package probe

import (
	"errors"
	"fmt"
	"time"
)

// LocalID is the id of the local probe: the checks that Peekaping runs itself. Every install has
// it, the migration seeds it, and it can't be deleted, disabled or have its token rotated.
const LocalID = "local"

// Status is whether a probe has an instance connected.
type Status string

const (
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
)

var (
	// ErrLocalProbe is returned when the local probe is deleted, disabled or has its token rotated
	ErrLocalProbe = errors.New("the local probe can't be changed this way")
	// ErrNotFound is returned when the probe an operation names doesn't exist
	ErrNotFound = errors.New("probe not found")
	// ErrProbeHasMonitors is returned when a probe that monitors run on is deleted. The error
	// that wraps it, HasMonitorsError, says how many.
	ErrProbeHasMonitors = errors.New("the probe has monitors")
	// ErrInstanceTaken is returned when a probe registers an instance id that another probe's
	// instance already has
	ErrInstanceTaken = errors.New("instance id belongs to another probe")
	// ErrTooManyInstances is returned when a probe registers an instance it doesn't hold yet while
	// it holds MaxInstancesPerProbe, all of them online: there is no instance to replace
	ErrTooManyInstances = errors.New("the probe already has the most instances it may have")
)

// HasMonitorsError is the ErrProbeHasMonitors of a probe with Monitors monitors on it
type HasMonitorsError struct {
	Monitors int
}

func (e *HasMonitorsError) Error() string {
	return fmt.Sprintf("%s: %d run on it", ErrProbeHasMonitors, e.Monitors)
}

func (e *HasMonitorsError) Is(target error) bool { return target == ErrProbeHasMonitors }

// Model is a probe: a place Peekaping runs checks from. A remote probe runs on another machine
// and authenticates with its token.
type Model struct {
	ID       string
	Name     string
	Location string
	// TokenHash is "sha256$" and the hex SHA-256 of the probe's token. It is empty for a probe
	// with no token, such as the local probe before a bootstrap token sets one.
	TokenHash string
	// TokenPrefix is the first characters of the token, which the API shows in place of it
	TokenPrefix string
	Disabled    bool
	Status      Status
	// SupportedTypes are the monitor types the probe can run: the union over its online
	// instances, each of which reports its own when it says hello. An empty list means every
	// type. The service works the union out when it adds the probe's details (FindByID and
	// FindAll); the repository's probe, and the one FindByToken returns, hold the union as of the
	// probe's last hello, which is what the probe runs while none of its instances is online.
	SupportedTypes []string
	// NotificationIDs are the notification channels that hear about the probe. A probe with none
	// notifies the channels of its active monitors instead.
	NotificationIDs []string
	// Instances are the processes that have connected with the probe's token
	Instances []*Instance
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Instance is one process that connected with a probe's token. A probe may have several.
type Instance struct {
	ID              string
	Hostname        string
	OS              string
	Arch            string
	Version         string
	ProtocolVersion int
	// SupportedTypes are the monitor types this instance's build can run, as its hello said. An
	// empty list means every type, as it does for a probe: an instance of an old probe reports
	// none.
	SupportedTypes []string
	LastSeenAt     time.Time
}

// StatusChange is one time a probe went online or offline: its status from At on, until the next
// change. The history of a probe starts with the change that made it, as offline, and the status a
// probe has at any time is that of its latest change at or before it.
type StatusChange struct {
	ProbeID string
	Status  Status
	// At is when the status really changed: the time the probe's instances were last seen, for a
	// probe that went offline, and not the time the server noticed
	At time.Time
}

// InstanceConnection is one stretch of time that a probe instance was connected. It starts when
// the instance says hello, or at its first request after more than InstanceOnlineWindow of quiet,
// and it ends when the instance says bye or at the instance's last request.
type InstanceConnection struct {
	InstanceID string
	ProbeID    string
	StartedAt  time.Time
	// LastSeenAt is the latest time the connection is known to have lasted to: the instance's last
	// request, or the bye. A connection with no EndedAt ended here, unless the instance is still
	// talking.
	LastSeenAt time.Time
	// EndedAt is when the instance said bye. It is nil for a connection that ended by going quiet,
	// and for one that is still open.
	EndedAt *time.Time
}

// ModelWithToken is a probe with its plain text token. Only creating a probe and rotating its
// token return it: the token is stored only as a hash.
type ModelWithToken struct {
	Model
	Token string
}

// CreateModel is what the repository needs to store a new probe
type CreateModel struct {
	// ID is the new probe's id. Empty means a new UUID.
	ID          string
	Name        string
	Location    string
	TokenHash   string
	TokenPrefix string
}

// UpdateModel is what can change about a probe: nil fields stay as they are
type UpdateModel struct {
	Name     *string
	Location *string
	Disabled *bool
}
