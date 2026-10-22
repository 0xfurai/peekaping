package probe_notification

import "context"

type Repository interface {
	// FindByProbeID returns the probe's links, in the order they were made
	FindByProbeID(ctx context.Context, probeID string) ([]*Model, error)
	// Replace makes notificationIDs the probe's channels, in one step: it adds the channels it is
	// given and drops the others. A channel that doesn't exist fails it with ErrUnknownChannel,
	// and changes nothing.
	Replace(ctx context.Context, probeID string, notificationIDs []string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}
