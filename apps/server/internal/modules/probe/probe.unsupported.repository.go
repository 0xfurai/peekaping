package probe

import (
	"context"
	"time"
)

// UnsupportedRepositoryImpl is the repository of a MongoDB install, which has no probes: the
// first feature that is SQL-only under ADR 0001. Every method returns ErrUnsupportedStorage, so
// that such an install starts, lists no probes, and answers the other probe routes with 501. No
// Mongo code is written for it.
type UnsupportedRepositoryImpl struct{}

// NewUnsupportedRepository is what RegisterRepositoryByDBType provides for MongoDB
func NewUnsupportedRepository() Repository {
	return &UnsupportedRepositoryImpl{}
}

func (r *UnsupportedRepositoryImpl) Create(ctx context.Context, probe *CreateModel) (*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) FindAll(ctx context.Context) ([]*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) FindByTokenHash(ctx context.Context, tokenHash string) (*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) Update(ctx context.Context, id string, update *UpdateModel) (*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) UpdateToken(ctx context.Context, id string, tokenHash string, tokenPrefix string) error {
	return ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) Delete(ctx context.Context, id string) error {
	return ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) CountMonitors(ctx context.Context, id string) (int, error) {
	return 0, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) FindInstances(ctx context.Context, probeID string) ([]*Instance, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) UpsertInstance(ctx context.Context, probeID string, inst *Instance) error {
	return ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) TouchInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) (bool, error) {
	return false, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) RetireInstance(ctx context.Context, probeID string, instanceID string, seenAt time.Time) error {
	return ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) SetSupportedTypes(ctx context.Context, probeID string, types []string) error {
	return ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) UpdateStatus(ctx context.Context, id string, from Status, to Status) (bool, error) {
	return false, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) DeleteInstancesUnseenSince(ctx context.Context, before time.Time) (int, error) {
	return 0, ErrUnsupportedStorage
}
