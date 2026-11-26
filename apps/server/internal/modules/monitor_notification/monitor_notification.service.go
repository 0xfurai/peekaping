package monitor_notification

import (
	"context"
	"fmt"
	"strings"

	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, monitorID string, notificationID string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Delete(ctx context.Context, id string) error
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	// FindByMonitorIDs is FindByMonitorID for many monitors at once, by monitor ID. A monitor with
	// no links has no entry.
	FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByNotificationID(ctx context.Context, notificationID string) error
	// RequireChannels is nil when every channel is in the workspace, and otherwise an error that
	// wraps ErrUnknownChannel and names the first that isn't. A monitor links only to channels of
	// its own workspace (docs/adr/0005 section 2), and a caller checks them all before it writes a link.
	RequireChannels(ctx context.Context, workspaceID workspace.ID, ids []string) error
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		logger.Named("[monitor-notification-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, monitorID string, notificationID string) (*Model, error) {
	createModel := &Model{
		MonitorID:      monitorID,
		NotificationID: notificationID,
	}

	return mr.repository.Create(ctx, createModel)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return mr.repository.Delete(ctx, id)
}

func (mr *ServiceImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	return mr.repository.FindByMonitorID(ctx, monitorID)
}

func (mr *ServiceImpl) FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error) {
	return mr.repository.FindByMonitorIDs(ctx, monitorIDs)
}

func (mr *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return mr.repository.DeleteByMonitorID(ctx, monitorID)
}

func (mr *ServiceImpl) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return mr.repository.DeleteByNotificationID(ctx, notificationID)
}

func (mr *ServiceImpl) RequireChannels(ctx context.Context, workspaceID workspace.ID, ids []string) error {
	// An ID that isn't a UUID can't be one a database has, and PostgreSQL fails the query on it
	// instead of finding nothing. The query takes the lower case form, so that a copy in another
	// case finds the same record on SQLite, which compares text, as it does on PostgreSQL.
	canonical := make([]string, len(ids))
	for i, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrUnknownChannel, id)
		}
		canonical[i] = parsed.String()
	}
	found, err := mr.repository.FindChannelIDsInWorkspace(ctx, workspaceID, canonical)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(found))
	for _, id := range found {
		exists[strings.ToLower(id)] = true
	}
	for i, id := range canonical {
		if !exists[id] {
			return fmt.Errorf("%w: %s", ErrUnknownChannel, ids[i])
		}
	}
	return nil
}
