package tag

import (
	"context"
	"errors"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, id string) error
	FindByName(ctx context.Context, name string) (*Model, error)
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
		logger.Named("[tag-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	// Check if tag with same name already exists
	existingTag, err := s.repository.FindByName(ctx, entity.Name)
	if err != nil {
		return nil, err
	}
	if existingTag != nil {
		return nil, errors.New("tag with this name already exists")
	}

	createModel := &Model{
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	return s.repository.Create(ctx, createModel)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *ServiceImpl) FindByName(ctx context.Context, name string) (*Model, error) {
	return s.repository.FindByName(ctx, name)
}

func (s *ServiceImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	return s.repository.FindAll(ctx, page, limit, q)
}

func (s *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	// Check if another tag with same name exists (exclude current tag)
	existingTag, err := s.repository.FindByName(ctx, entity.Name)
	if err != nil {
		return nil, err
	}
	if existingTag != nil && existingTag.ID != id {
		return nil, errors.New("tag with this name already exists")
	}

	updateModel := &Model{
		ID:          id,
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

func (s *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	// Check if another tag with same name exists (exclude current tag)
	if entity.Name != nil {
		existingTag, err := s.repository.FindByName(ctx, *entity.Name)
		if err != nil {
			return nil, err
		}
		if existingTag != nil && existingTag.ID != id {
			return nil, errors.New("tag with this name already exists")
		}
	}

	updateModel := &UpdateModel{
		ID:          &id,
		Name:        entity.Name,
		Color:       entity.Color,
		Description: entity.Description,
	}

	err := s.repository.UpdatePartial(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return updatedModel, nil
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	return s.repository.Delete(ctx, id)
}
