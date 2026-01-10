package notification_channel

import (
	"context"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
)

type NotificationChannelProvider interface {
	Send(ctx context.Context, configJSON, message string, monitor *monitor.Model, heartbeat *heartbeat.Model) error
	Validate(configJSON string) error
	Unmarshal(configJSON string) (any, error)
}
