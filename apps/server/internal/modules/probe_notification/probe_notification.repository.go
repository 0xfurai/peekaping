package probe_notification

import "context"

type Repository interface {
	// FindByProbeID returns the probe's links, in the order they were made
	FindByProbeID(ctx context.Context, probeID string) ([]*Model, error)
	// Replace makes notificationIDs the probe's channels: it adds the channels it is given and
	// drops the others. A channel that doesn't exist fails it with ErrUnknownChannel before any
	// link changes. It runs on the transaction ctx carries (see dbtx), so a caller that opens one
	// makes the swap, and what it writes beside it, a single step.
	Replace(ctx context.Context, probeID string, notificationIDs []string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}
