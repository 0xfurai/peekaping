package probe_notification

import "context"

// UnsupportedRepositoryImpl is the repository of a MongoDB install, which has no probes: the
// first feature that is SQL-only under ADR 0001. No Mongo code is written for it.
type UnsupportedRepositoryImpl struct{}

// NewUnsupportedRepository is what RegisterRepositoryByDBType provides for MongoDB
func NewUnsupportedRepository() Repository {
	return &UnsupportedRepositoryImpl{}
}

func (r *UnsupportedRepositoryImpl) FindByProbeID(ctx context.Context, probeID string) ([]*Model, error) {
	return nil, ErrUnsupportedStorage
}

func (r *UnsupportedRepositoryImpl) Replace(ctx context.Context, probeID string, notificationIDs []string) error {
	return ErrUnsupportedStorage
}

// DeleteByNotificationID has nothing to clear: no link can exist. It reports no error, so that
// deleting a notification channel on a MongoDB install doesn't log a failure each time.
func (r *UnsupportedRepositoryImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return nil
}
