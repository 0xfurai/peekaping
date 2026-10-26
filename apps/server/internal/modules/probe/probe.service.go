package probe

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/probe_notification"

	"go.uber.org/zap"
)

// Service manages probes: the places Peekaping runs checks from
type Service interface {
	// Create makes a probe and its token. The token is in the result only: after this the
	// service knows the token's hash alone.
	Create(ctx context.Context, req *CreateRequest) (*ModelWithToken, error)
	// FindAll returns every probe with its instances and notification channels, the local probe
	// first.
	FindAll(ctx context.Context) ([]*Model, error)
	// FindByID returns nil when there is no such probe
	FindByID(ctx context.Context, id string) (*Model, error)
	// Update returns ErrNotFound when there is no such probe, and ErrLocalProbe when it would
	// disable the local probe
	Update(ctx context.Context, id string, req *UpdateRequest) (*Model, error)
	// Delete returns ErrNotFound when there is no such probe, ErrLocalProbe for the local one, and
	// ErrProbeHasMonitors (a HasMonitorsError) when monitors run on it: it deletes nothing then
	Delete(ctx context.Context, id string) error
	// RotateToken replaces the probe's token with a new one, which is in the result only. The
	// old token finds nothing from then on. It returns ErrNotFound when there is no such probe,
	// and ErrLocalProbe for the local one, whose token PROBE_BOOTSTRAP_TOKEN sets.
	RotateToken(ctx context.Context, id string) (*ModelWithToken, error)
	// FindByToken returns the probe a token belongs to, or nil for an unknown token and for a
	// disabled probe, which never authenticates. It is the check the probe protocol's bearer
	// middleware makes on every request. The probe it returns has no instances or notification
	// channels: FindByID adds them. Its SupportedTypes are the last the probe's instances ran
	// between them, not what they run now: FindByID works the union out, and FindInstance has the
	// types of one instance.
	FindByToken(ctx context.Context, token string) (*Model, error)
	// Bootstrap makes token the local probe's token, as PROBE_BOOTSTRAP_TOKEN asks at startup. An
	// empty token leaves the local probe as it is.
	Bootstrap(ctx context.Context, token string) error
	// RegisterInstance records an instance that said hello with the probe's token, or updates
	// the one with the same id, with the monitor types its build can run. It returns
	// ErrInstanceTaken when another probe has an instance with that id, and records nothing then.
	RegisterInstance(ctx context.Context, probeID string, inst *Instance) error
	// FindInstance returns the probe's instance with the id, or nil when the probe has none
	FindInstance(ctx context.Context, probeID string, instanceID string) (*Instance, error)
	// TouchInstance stamps the time the probe's instance was last heard from. It returns false
	// when the probe has no instance with that id, which has to say hello first.
	TouchInstance(ctx context.Context, probeID string, instanceID string, at time.Time) (bool, error)
	// RetireInstance leaves the probe's instance last seen at seenAt, as when it says bye: the
	// caller passes a time outside the online window, so that the instance is out of the dealing
	// at once. The instance stays listed and the probe counts as online until its offline
	// threshold has passed since seenAt, so that a restart is not an outage. The sweep prunes the
	// instance once it has been silent for a day. An instance that isn't there is not an error.
	// byeAt is the time of the bye, when the instance's connection ends.
	RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time, byeAt time.Time) error
	// FindInstanceConnections returns the stretches of time the probe's instances were connected,
	// those last seen or ended at or after since, by instance and then by start. A hello starts one,
	// and so does a request after more than a minute of quiet; a connection with no EndedAt ended
	// at its last seen time.
	FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*InstanceConnection, error)
	// MarkOnline sets an offline probe online. It returns true only when it changed the status, so
	// that of the callers that race to bring a probe back (one request on each API replica, and
	// the sweep), exactly one announces it. A disabled probe stays offline. at is when the probe
	// came back: the time of the request that did, which the probe's history records.
	MarkOnline(ctx context.Context, id string, at time.Time) (changed bool, err error)
	// MarkOffline sets an online probe offline, and returns true only when it changed the status.
	// at is when the probe went offline: the time its instances were last seen.
	MarkOffline(ctx context.Context, id string, at time.Time) (changed bool, err error)
	// FindStatusChanges returns the probe's history from since on: its latest change at or before
	// since, which is the status it carries into the range, then every change after it
	FindStatusChanges(ctx context.Context, probeID string, since time.Time) ([]*StatusChange, error)
	// DeleteStatusChangesOlderThan removes the changes before the given time except each probe's
	// latest one before it, and returns how many it removed. The hourly cleanup calls it with the
	// end of the data retention period.
	DeleteStatusChangesOlderThan(ctx context.Context, before time.Time) (int, error)
}

// CreateRequest is a new probe
type CreateRequest struct {
	Name     string
	Location string
	// NotificationIDs are the notification channels that hear about the probe. A probe with none
	// notifies the channels of its active monitors instead.
	NotificationIDs []string
}

// UpdateRequest is what changes about a probe: nil fields stay as they are
type UpdateRequest struct {
	Name     *string
	Location *string
	Disabled *bool
	// NotificationIDs replaces the probe's notification channels. Nil leaves them, and an empty
	// slice clears them.
	NotificationIDs []string
}

type ServiceImpl struct {
	repo       Repository
	transactor dbtx.Transactor
	channels   probe_notification.Service
	now        func() time.Time
	logger     *zap.SugaredLogger
}

// MARK: Constructor
func NewService(repo Repository, transactor dbtx.Transactor, channels probe_notification.Service, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		channels:   channels,
		now:        time.Now,
		logger:     logger.Named("[probe-service]"),
	}
}

// MARK: Create
func (s *ServiceImpl) Create(ctx context.Context, req *CreateRequest) (*ModelWithToken, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	// The probe and its channels are stored together or not at all: the token authenticates from
	// the moment the probe is committed, and the caller is told that a failed create made none.
	var created *Model
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.repo.Create(ctx, &CreateModel{
			Name:        req.Name,
			Location:    req.Location,
			TokenHash:   hashToken(token),
			TokenPrefix: tokenDisplayPrefix(token),
		})
		if err != nil {
			return err
		}
		if len(req.NotificationIDs) == 0 {
			return nil
		}
		return s.channels.Replace(ctx, created.ID, req.NotificationIDs)
	})
	if err != nil {
		return nil, err
	}
	probe, err := s.withDetails(ctx, created)
	if err != nil {
		return nil, err
	}
	s.logger.Infow("Probe created", "probeId", probe.ID, "name", probe.Name)
	return &ModelWithToken{Model: *probe, Token: token}, nil
}

// MARK: FindAll
func (s *ServiceImpl) FindAll(ctx context.Context) ([]*Model, error) {
	probes, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	for i, p := range probes {
		if probes[i], err = s.withDetails(ctx, p); err != nil {
			return nil, err
		}
	}
	return probes, nil
}

// MARK: FindByID
func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	probe, err := s.repo.FindByID(ctx, id)
	if err != nil || probe == nil {
		return nil, err
	}
	return s.withDetails(ctx, probe)
}

// MARK: Update
func (s *ServiceImpl) Update(ctx context.Context, id string, req *UpdateRequest) (*Model, error) {
	// The probe and its channels change together or not at all: an unknown channel, or a failure
	// of either step, leaves the probe as it was.
	var updated *Model
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		existing, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return ErrNotFound
		}
		if id == LocalID && req.Disabled != nil && *req.Disabled {
			return ErrLocalProbe
		}
		updated, err = s.repo.Update(ctx, id, &UpdateModel{Name: req.Name, Location: req.Location, Disabled: req.Disabled})
		if err != nil {
			return err
		}
		if updated == nil {
			return ErrNotFound
		}
		if req.NotificationIDs == nil {
			return nil
		}
		return s.channels.Replace(ctx, id, req.NotificationIDs)
	})
	if err != nil {
		return nil, err
	}
	return s.withDetails(ctx, updated)
}

// MARK: Delete
func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	// The count and the delete are one transaction. The database holds the line as well: a monitor
	// assigned to the probe after the count makes the delete fail with ErrProbeHasMonitors, and
	// nothing is deleted.
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		existing, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return ErrNotFound
		}
		if id == LocalID {
			return ErrLocalProbe
		}
		monitors, err := s.repo.CountMonitors(ctx, id)
		if err != nil {
			return err
		}
		if monitors > 0 {
			return &HasMonitorsError{Monitors: monitors}
		}
		return s.repo.Delete(ctx, id)
	})
	var hasMonitors *HasMonitorsError
	if errors.Is(err, ErrProbeHasMonitors) && !errors.As(err, &hasMonitors) {
		// The refusal came from the database, so it rolled the transaction back, and PostgreSQL
		// would fail a read in it. The monitors are counted again outside it for the message.
		if monitors, countErr := s.repo.CountMonitors(ctx, id); countErr == nil && monitors > 0 {
			return &HasMonitorsError{Monitors: monitors}
		}
	}
	if err != nil {
		return err
	}
	s.logger.Infow("Probe deleted", "probeId", id)
	return nil
}

// MARK: RotateToken
func (s *ServiceImpl) RotateToken(ctx context.Context, id string) (*ModelWithToken, error) {
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrNotFound
	}
	if id == LocalID {
		return nil, ErrLocalProbe
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateToken(ctx, id, hashToken(token), tokenDisplayPrefix(token)); err != nil {
		return nil, err
	}
	rotated, err := s.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rotated == nil {
		return nil, ErrNotFound
	}
	s.logger.Infow("Probe token rotated", "probeId", id)
	return &ModelWithToken{Model: *rotated, Token: token}, nil
}

// MARK: FindByToken
func (s *ServiceImpl) FindByToken(ctx context.Context, token string) (*Model, error) {
	if token == "" {
		return nil, nil
	}
	hash := hashToken(token)
	probe, err := s.repo.FindByTokenHash(ctx, hash)
	if err != nil || probe == nil {
		return nil, err
	}
	// The hash is the lookup key, so this should always hold
	if subtle.ConstantTimeCompare([]byte(probe.TokenHash), []byte(hash)) != 1 {
		return nil, nil
	}
	if probe.Disabled {
		return nil, nil
	}
	return probe, nil
}

// MARK: Bootstrap
func (s *ServiceImpl) Bootstrap(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if len(token) < MinBootstrapTokenLength {
		return fmt.Errorf("PROBE_BOOTSTRAP_TOKEN must be at least %d characters", MinBootstrapTokenLength)
	}
	hash, prefix := hashToken(token), tokenDisplayPrefix(token)

	local, err := s.repo.FindByID(ctx, LocalID)
	if err != nil {
		return err
	}
	if local == nil {
		// The migration seeds the local probe, so this is a database that lost the row
		_, err = s.repo.Create(ctx, &CreateModel{ID: LocalID, Name: "Local", TokenHash: hash, TokenPrefix: prefix})
		return err
	}
	if local.TokenHash == hash && local.TokenPrefix == prefix {
		return nil
	}
	return s.repo.UpdateToken(ctx, LocalID, hash, prefix)
}

// MARK: RegisterInstance
func (s *ServiceImpl) RegisterInstance(ctx context.Context, probeID string, inst *Instance) error {
	// One transaction: a hello that fails halfway leaves neither the instance nor the types.
	// The instance goes first: a refused one must not change the probe's types.
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		// The instance's last seen time is now, to the hello: what has been quiet for a window
		// before it is not online
		quietBefore := inst.LastSeenAt.Add(-InstanceOnlineWindow)
		if err := s.repo.MakeRoomForInstance(ctx, probeID, inst.ID, MaxInstancesPerProbe, quietBefore); err != nil {
			return err
		}
		if err := s.repo.UpsertInstance(ctx, probeID, inst); err != nil {
			return err
		}
		// The probe's own list is what its online instances run between them as of this hello. It
		// is only read when no instance is online (see withDetails), as what the probe last ran.
		instances, err := s.repo.FindInstances(ctx, probeID)
		if err != nil {
			return err
		}
		return s.repo.SetSupportedTypes(ctx, probeID, UnionTypes(FilterOnline(instances, inst.LastSeenAt, InstanceOnlineWindow)))
	})
	if err != nil {
		return err
	}
	s.logger.Infow("Probe instance registered", "probeId", probeID, "instanceId", inst.ID, "hostname", inst.Hostname, "version", inst.Version)
	return nil
}

// MARK: FindInstance
func (s *ServiceImpl) FindInstance(ctx context.Context, probeID string, instanceID string) (*Instance, error) {
	return s.repo.FindInstance(ctx, probeID, instanceID)
}

// MARK: TouchInstance
func (s *ServiceImpl) TouchInstance(ctx context.Context, probeID string, instanceID string, at time.Time) (bool, error) {
	return s.repo.TouchInstance(ctx, probeID, instanceID, at)
}

// MARK: RetireInstance
func (s *ServiceImpl) RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time, byeAt time.Time) error {
	if err := s.repo.RetireInstance(ctx, probeID, instanceID, seenAt, byeAt); err != nil {
		return err
	}
	s.logger.Infow("Probe instance retired", "probeId", probeID, "instanceId", instanceID)
	return nil
}

// MARK: FindInstanceConnections
func (s *ServiceImpl) FindInstanceConnections(ctx context.Context, probeID string, since time.Time) ([]*InstanceConnection, error) {
	return s.repo.FindInstanceConnections(ctx, probeID, since)
}

// MARK: MarkOnline
func (s *ServiceImpl) MarkOnline(ctx context.Context, id string, at time.Time) (bool, error) {
	return s.repo.UpdateStatus(ctx, id, StatusOffline, StatusOnline, at)
}

// MARK: MarkOffline
func (s *ServiceImpl) MarkOffline(ctx context.Context, id string, at time.Time) (bool, error) {
	return s.repo.UpdateStatus(ctx, id, StatusOnline, StatusOffline, at)
}

// MARK: FindStatusChanges
func (s *ServiceImpl) FindStatusChanges(ctx context.Context, probeID string, since time.Time) ([]*StatusChange, error) {
	return s.repo.FindStatusChanges(ctx, probeID, since)
}

// MARK: DeleteStatusChangesOlderThan
func (s *ServiceImpl) DeleteStatusChangesOlderThan(ctx context.Context, before time.Time) (int, error) {
	return s.repo.DeleteStatusChangesOlderThan(ctx, before)
}

// withDetails adds a probe's instances and notification channels, and makes its supported types what
// its instances run now: the union over those online. They can't be stored, since an instance
// leaves the dealing by going quiet, which no write marks. With none online the probe keeps what
// its own list says, the union as of its last hello, so that a probe that is down is not taken to
// run every type.
func (s *ServiceImpl) withDetails(ctx context.Context, p *Model) (*Model, error) {
	instances, err := s.repo.FindInstances(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	links, err := s.channels.FindByProbeID(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	detailed := *p
	if online := FilterOnline(instances, s.now().UTC(), InstanceOnlineWindow); len(online) > 0 {
		detailed.SupportedTypes = UnionTypes(online)
	}
	detailed.Instances = instances
	detailed.NotificationIDs = make([]string, len(links))
	for i, link := range links {
		detailed.NotificationIDs[i] = link.NotificationID
	}
	return &detailed, nil
}
