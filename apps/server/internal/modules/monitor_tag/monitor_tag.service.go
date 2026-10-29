package monitor_tag

import (
	"context"
	"fmt"
	"strings"

	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, monitorID string, tagID string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	Delete(ctx context.Context, id string) error
	FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error)
	// FindByMonitorIDs is FindByMonitorID for many monitors at once, by monitor ID. A monitor with
	// no links has no entry.
	FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error)
	FindByTagID(ctx context.Context, tagID string) ([]*Model, error)
	DeleteByMonitorID(ctx context.Context, monitorID string) error
	DeleteByTagID(ctx context.Context, tagID string) error
	DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error
	// RequireTags is nil when every tag is in the workspace, and otherwise an error that wraps
	// ErrUnknownTag and names the first that isn't. A monitor links only to tags of its own
	// workspace (docs/adr/0005 section 2), and a caller checks them all before it writes a link.
	RequireTags(ctx context.Context, workspaceID workspace.ID, ids []string) error
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
		logger.Named("[monitor-tag-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, monitorID string, tagID string) (*Model, error) {
	createModel := &Model{
		MonitorID: monitorID,
		TagID:     tagID,
	}

	return s.repository.Create(ctx, createModel)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	return s.repository.Delete(ctx, id)
}

func (s *ServiceImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	return s.repository.FindByMonitorID(ctx, monitorID)
}

func (s *ServiceImpl) FindByMonitorIDs(ctx context.Context, monitorIDs []string) (map[string][]*Model, error) {
	return s.repository.FindByMonitorIDs(ctx, monitorIDs)
}

func (s *ServiceImpl) FindByTagID(ctx context.Context, tagID string) ([]*Model, error) {
	return s.repository.FindByTagID(ctx, tagID)
}

func (s *ServiceImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	return s.repository.DeleteByMonitorID(ctx, monitorID)
}

func (s *ServiceImpl) DeleteByTagID(ctx context.Context, tagID string) error {
	return s.repository.DeleteByTagID(ctx, tagID)
}

func (s *ServiceImpl) DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error {
	return s.repository.DeleteByMonitorAndTag(ctx, monitorID, tagID)
}

func (s *ServiceImpl) RequireTags(ctx context.Context, workspaceID workspace.ID, ids []string) error {
	// An ID that isn't a UUID can't be one a database has, and PostgreSQL fails the query on it
	// instead of finding nothing. The query takes the lower case form, so that a copy in another
	// case finds the same record on SQLite, which compares text, as it does on PostgreSQL.
	canonical := make([]string, len(ids))
	for i, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrUnknownTag, id)
		}
		canonical[i] = parsed.String()
	}
	found, err := s.repository.FindTagIDsInWorkspace(ctx, workspaceID, canonical)
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(found))
	for _, id := range found {
		exists[strings.ToLower(id)] = true
	}
	for i, id := range canonical {
		if !exists[id] {
			return fmt.Errorf("%w: %s", ErrUnknownTag, ids[i])
		}
	}
	return nil
}
