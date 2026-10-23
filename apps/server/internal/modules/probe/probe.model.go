package probe

import (
	"errors"
	"time"

	"peekaping/internal/modules/probe_notification"
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
	// ErrInstanceTaken is returned when a probe registers an instance id that another probe's
	// instance already has
	ErrInstanceTaken = errors.New("instance id belongs to another probe")
	// ErrUnsupportedStorage is returned on a MongoDB install, which has no probes (ADR 0001). It
	// is the error of the probe_notification module too, so that it means the same in both.
	ErrUnsupportedStorage = probe_notification.ErrUnsupportedStorage
)

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
	// SupportedTypes are the monitor types the probe can run. A probe reports them when it
	// connects.
	SupportedTypes []string
	// NotificationIDs are the notification channels that hear about the probe
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
	LastSeenAt      time.Time
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
