package notification_channel

import (
	"context"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
)

type NotificationChannelProvider interface {
	Send(ctx context.Context, configJSON, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) error
	Validate(configJSON string) error
	Unmarshal(configJSON string) (any, error)
}

// WorkspaceValidator is a provider whose config can only be judged for the workspace that owns the
// channel, such as an address that must belong to one of its members. The controller calls it,
// when the provider has it, after Validate, with a context that names the workspace
// (providers.WithWorkspace). The listener gives the same context to Send.
type WorkspaceValidator interface {
	ValidateInWorkspace(ctx context.Context, configJSON string) error
}
