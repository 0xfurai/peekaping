package tag

import (
	"context"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Service works on tags, which belong to a workspace (docs/adr/0005 section 3). Every method takes
// the workspace right after the context, and answers ErrNotFound for a tag of another workspace.
// Tag names are unique within a workspace: a name another tag of the workspace has is ErrNameTaken.
type Service interface {
	// Create makes the tag in the workspace
	Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
	FindByName(ctx context.Context, workspaceID workspace.ID, name string) (*Model, error)
}

type ServiceImpl struct {
	repository        Repository
	monitorTagService monitor_tag.Service
	logger            *zap.SugaredLogger
}

func NewService(
	repository Repository,
	monitorTagService monitor_tag.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		monitorTagService,
		logger.Named("[tag-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error) {
	// Check if the workspace has a tag with the same name already
	existingTag, err := s.repository.FindByName(ctx, workspaceID, entity.Name)
	if err != nil {
		return nil, err
	}
	if existingTag != nil {
		return nil, ErrNameTaken
	}

	createModel := &Model{
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	return s.repository.Create(ctx, workspaceID, createModel)
}

func (s *ServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return s.repository.FindByID(ctx, workspaceID, id)
}

func (s *ServiceImpl) FindByName(ctx context.Context, workspaceID workspace.ID, name string) (*Model, error) {
	return s.repository.FindByName(ctx, workspaceID, name)
}

func (s *ServiceImpl) FindAll(
	ctx context.Context,
	workspaceID workspace.ID,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	return s.repository.FindAll(ctx, workspaceID, page, limit, q)
}

func (s *ServiceImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error) {
	// The tag comes first: a tag of another workspace is not found whatever name the request has,
	// or the answer would tell what names this workspace holds (docs/adr/0005 section 8)
	if err := s.requireTag(ctx, workspaceID, id); err != nil {
		return nil, err
	}

	// Check if another tag of the workspace has the same name (exclude current tag)
	existingTag, err := s.repository.FindByName(ctx, workspaceID, entity.Name)
	if err != nil {
		return nil, err
	}
	if existingTag != nil && existingTag.ID != id {
		return nil, ErrNameTaken
	}

	updateModel := &Model{
		ID:          id,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	err = s.repository.UpdateFull(ctx, workspaceID, id, updateModel)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (s *ServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error) {
	if err := s.requireTag(ctx, workspaceID, id); err != nil {
		return nil, err
	}

	// Check if another tag of the workspace has the same name (exclude current tag)
	if entity.Name != nil {
		existingTag, err := s.repository.FindByName(ctx, workspaceID, *entity.Name)
		if err != nil {
			return nil, err
		}
		if existingTag != nil && existingTag.ID != id {
			return nil, ErrNameTaken
		}
	}

	updateModel := &UpdateModel{
		ID:          &id,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	err := s.repository.UpdatePartial(ctx, workspaceID, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := s.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if updatedModel == nil {
		return nil, ErrNotFound
	}

	return updatedModel, nil
}

// requireTag is ErrNotFound unless the tag is in the workspace
func (s *ServiceImpl) requireTag(ctx context.Context, workspaceID workspace.ID, id string) error {
	found, err := s.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if found == nil {
		return ErrNotFound
	}
	return nil
}

func (s *ServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	// A tag of another workspace is not deleted, and keeps its links
	if err := s.repository.Delete(ctx, workspaceID, id); err != nil {
		return err
	}

	// The links go with the tag: the database deletes them too, so a failure here is only logged
	if err := s.monitorTagService.DeleteByTagID(ctx, id); err != nil {
		s.logger.Warnw("Failed to delete monitor-tag relations", "tagID", id, "error", err)
	}
	return nil
}
