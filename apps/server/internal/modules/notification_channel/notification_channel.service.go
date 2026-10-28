package notification_channel

import (
	"context"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/probe_notification"
	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Service works on notification channels, which belong to a workspace (docs/adr/0005 section 3).
//
// A method that serves a workspace's request takes the workspace right after the context, and
// answers ErrNotFound for a channel of another workspace. A method whose name ends in
// AnyWorkspace works across workspaces, for the paths that run for the whole install.
type Service interface {
	// Create makes the channel in the workspace, whatever workspace the channel says
	Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	// FindByIDAnyWorkspace is for the paths that send a notification to the channels linked to a
	// monitor or a probe
	FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	// FindAllAnyWorkspace returns a page of every channel of the install, for the usage ping
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
}

type ServiceImpl struct {
	repository                 Repository
	monitorNotificationService monitor_notification.Service
	probeNotificationService   probe_notification.Service
	logger                     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	monitorNotificationService monitor_notification.Service,
	probeNotificationService probe_notification.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		monitorNotificationService,
		probeNotificationService,
		logger.Named("[notification-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error) {
	createModel := &Model{
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	return mr.repository.Create(ctx, workspaceID, createModel)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, workspaceID, id)
}

func (mr *ServiceImpl) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByIDAnyWorkspace(ctx, id)
}

func (mr *ServiceImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return mr.repository.FindAllAnyWorkspace(ctx, page, limit)
}

func (mr *ServiceImpl) FindAll(
	ctx context.Context,
	workspaceID workspace.ID,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	entities, err := mr.repository.FindAll(ctx, workspaceID, page, limit, q)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error) {
	updateModel := &Model{
		ID:        id,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &entity.Config,
	}

	err := mr.repository.UpdateFull(ctx, workspaceID, id, updateModel)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error) {
	updateModel := &UpdateModel{
		ID:        &id,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    entity.Config,
	}

	err := mr.repository.UpdatePartial(ctx, workspaceID, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := mr.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if updatedModel == nil {
		return nil, ErrNotFound
	}

	return updatedModel, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	// A channel of another workspace is not deleted, and keeps its links
	err := mr.repository.Delete(ctx, workspaceID, id)
	if err != nil {
		return err
	}

	// Cascade delete monitor_notification relations. The channel is already gone, so a
	// failure here doesn't fail the delete: links left behind are skipped when sending.
	if err := mr.monitorNotificationService.DeleteByNotificationID(ctx, id); err != nil {
		mr.logger.Warnw("Failed to delete the deleted notification channel's monitor links", "id", id, "error", err)
	}

	// The same for the probes' links.
	if err := mr.probeNotificationService.DeleteByNotificationID(ctx, id); err != nil {
		mr.logger.Warnw("Failed to delete the deleted notification channel's probe links", "id", id, "error", err)
	}

	return nil
}
