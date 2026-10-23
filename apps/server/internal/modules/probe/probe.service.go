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
	// first. On a MongoDB install it returns none.
	FindAll(ctx context.Context) ([]*Model, error)
	// FindByID returns nil when there is no such probe
	FindByID(ctx context.Context, id string) (*Model, error)
	// Update returns ErrNotFound when there is no such probe, and ErrLocalProbe when it would
	// disable the local probe
	Update(ctx context.Context, id string, req *UpdateRequest) (*Model, error)
	// Delete returns ErrNotFound when there is no such probe, and ErrLocalProbe for the local one
	Delete(ctx context.Context, id string) error
	// RotateToken replaces the probe's token with a new one, which is in the result only. The
	// old token finds nothing from then on. It returns ErrNotFound when there is no such probe,
	// and ErrLocalProbe for the local one, whose token PROBE_BOOTSTRAP_TOKEN sets.
	RotateToken(ctx context.Context, id string) (*ModelWithToken, error)
	// FindByToken returns the probe a token belongs to, or nil for an unknown token and for a
	// disabled probe, which never authenticates. It is the check the probe protocol's bearer
	// middleware makes on every request. The probe it returns has no instances or notification
	// channels: FindByID adds them.
	FindByToken(ctx context.Context, token string) (*Model, error)
	// Bootstrap makes token the local probe's token, as PROBE_BOOTSTRAP_TOKEN asks at startup. An
	// empty token leaves the local probe as it is.
	Bootstrap(ctx context.Context, token string) error
	// RegisterInstance records an instance that said hello with the probe's token, or updates
	// the one with the same id, and makes supportedTypes the monitor types the probe can run. It
	// returns ErrInstanceTaken when another probe has an instance with that id, and records
	// nothing then.
	RegisterInstance(ctx context.Context, probeID string, inst *Instance, supportedTypes []string) error
	// TouchInstance stamps the time the probe's instance was last heard from. It returns false
	// when the probe has no instance with that id, which has to say hello first.
	TouchInstance(ctx context.Context, probeID string, instanceID string, at time.Time) (bool, error)
	// RetireInstance leaves the probe's instance last seen at seenAt, as when it says bye: the
	// caller passes a time outside the online window, so that the instance is out of the dealing
	// at once. The instance stays listed and the probe counts as online until its offline
	// threshold has passed since seenAt, so that a restart is not an outage. The sweep prunes the
	// instance once it has been silent for a day. An instance that isn't there is not an error.
	RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) error
}

// CreateRequest is a new probe
type CreateRequest struct {
	Name     string
	Location string
	// NotificationIDs are the notification channels that hear about the probe
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
	logger     *zap.SugaredLogger
}

// MARK: Constructor
func NewService(repo Repository, transactor dbtx.Transactor, channels probe_notification.Service, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		channels:   channels,
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
	if errors.Is(err, ErrUnsupportedStorage) {
		// A MongoDB install has no probes
		return []*Model{}, nil
	}
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
	if err := s.repo.Delete(ctx, id); err != nil {
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
func (s *ServiceImpl) RegisterInstance(ctx context.Context, probeID string, inst *Instance, supportedTypes []string) error {
	// One transaction: a hello that fails halfway leaves neither the instance nor the types.
	// The instance goes first: a refused one must not change the probe's types.
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpsertInstance(ctx, probeID, inst); err != nil {
			return err
		}
		return s.repo.SetSupportedTypes(ctx, probeID, supportedTypes)
	})
	if err != nil {
		return err
	}
	s.logger.Infow("Probe instance registered", "probeId", probeID, "instanceId", inst.ID, "hostname", inst.Hostname, "version", inst.Version)
	return nil
}

// MARK: TouchInstance
func (s *ServiceImpl) TouchInstance(ctx context.Context, probeID string, instanceID string, at time.Time) (bool, error) {
	return s.repo.TouchInstance(ctx, probeID, instanceID, at)
}

// MARK: RetireInstance
func (s *ServiceImpl) RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) error {
	if err := s.repo.RetireInstance(ctx, probeID, instanceID, seenAt); err != nil {
		return err
	}
	s.logger.Infow("Probe instance retired", "probeId", probeID, "instanceId", instanceID)
	return nil
}

// withDetails adds a probe's instances and notification channels
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
	detailed.Instances = instances
	detailed.NotificationIDs = make([]string, len(links))
	for i, link := range links {
		detailed.NotificationIDs[i] = link.NotificationID
	}
	return &detailed, nil
}
