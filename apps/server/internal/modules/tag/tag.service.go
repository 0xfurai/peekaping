package tag

import (
	"context"
	"errors"
	"vigi/internal/modules/monitor_tag"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto, orgID string) (*Model, error)
	FindByID(ctx context.Context, id string, orgID string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error)
	Delete(ctx context.Context, id string, orgID string) error
	FindByName(ctx context.Context, name string, orgID string) (*Model, error)
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

func (s *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto, orgID string) (*Model, error) {
	// Check if tag with same name already exists in this org
	existingTag, err := s.repository.FindByName(ctx, entity.Name, orgID)
	if err != nil {
		return nil, err
	}
	if existingTag != nil {
		return nil, errors.New("tag with this name already exists")
	}

	createModel := &Model{
		OrgID:       orgID,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	return s.repository.Create(ctx, createModel)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	return s.repository.FindByID(ctx, id, orgID)
}

func (s *ServiceImpl) FindByName(ctx context.Context, name string, orgID string) (*Model, error) {
	return s.repository.FindByName(ctx, name, orgID)
}

func (s *ServiceImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	orgID string,
) ([]*Model, error) {
	return s.repository.FindAll(ctx, page, limit, q, orgID)
}

func (s *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
	// Check if another tag with same name exists (exclude current tag)
	existingTag, err := s.repository.FindByName(ctx, entity.Name, orgID)
	if err != nil {
		return nil, err
	}
	if existingTag != nil && existingTag.ID != id {
		return nil, errors.New("tag with this name already exists")
	}

	updateModel := &Model{
		ID:          id,
		OrgID:       orgID,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	err = s.repository.UpdateFull(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (s *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
	// Check if another tag with same name exists (exclude current tag)
	if entity.Name != nil {
		existingTag, err := s.repository.FindByName(ctx, *entity.Name, orgID)
		if err != nil {
			return nil, err
		}
		if existingTag != nil && existingTag.ID != id {
			return nil, errors.New("tag with this name already exists")
		}
	}

	updateModel := &UpdateModel{
		ID:          &id,
		OrgID:       orgID,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	err := s.repository.UpdatePartial(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := s.repository.FindByID(ctx, id, orgID)
	if err != nil {
		return nil, err
	}

	return updatedModel, nil
}

func (s *ServiceImpl) Delete(ctx context.Context, id string, orgID string) error {
	// Verify tag exists and belongs to org
	tag, err := s.repository.FindByID(ctx, id, orgID)
	if err != nil {
		return err
	}
	if tag == nil {
		return errors.New("tag not found")
	}

	// Delete monitor_tag relations first
	err = s.monitorTagService.DeleteByTagID(ctx, id)
	if err != nil {
		s.logger.Warnw("Failed to delete monitor-tag relations", "tagID", id, "error", err)
	}

	return s.repository.Delete(ctx, id, orgID)
}
