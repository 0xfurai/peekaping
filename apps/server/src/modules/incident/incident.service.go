package incident

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, dto *CreateIncidentDTO) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error)
	Update(ctx context.Context, id string, dto *UpdateIncidentDTO) (*Model, error)
	Delete(ctx context.Context, id string) error
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
		repository: repository,
		logger:     logger.Named("[incident-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, dto *CreateIncidentDTO) (*Model, error) {
	incident := &Model{
		Title:   dto.Title,
		Content: dto.Content,
		Style:   dto.Style,
		Pin:     true,
		Active:  true,
	}

	// Set optional fields
	if dto.Pin != nil {
		incident.Pin = *dto.Pin
	}
	if dto.Active != nil {
		incident.Active = *dto.Active
	}
	if dto.StatusPageID != nil {
		incident.StatusPageID = dto.StatusPageID
	}

	created, err := s.repository.Create(ctx, incident)
	if err != nil {
		s.logger.Errorw("Failed to create incident", "error", err, "dto", dto)
		return nil, err
	}

	s.logger.Infow("Incident created successfully", "id", created.ID, "title", created.Title)
	return created, nil
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	incident, err := s.repository.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to find incident by ID", "error", err, "id", id)
		return nil, err
	}

	return incident, nil
}

func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	incidents, err := s.repository.FindAll(ctx, page, limit, q)
	if err != nil {
		s.logger.Errorw("Failed to find incidents", "error", err, "page", page, "limit", limit, "q", q)
		return nil, err
	}

	return incidents, nil
}

func (s *ServiceImpl) FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error) {
	incidents, err := s.repository.FindByStatusPageID(ctx, statusPageID)
	if err != nil {
		s.logger.Errorw("Failed to find incidents by status page ID", "error", err, "status_page_id", statusPageID)
		return nil, err
	}

	return incidents, nil
}

func (s *ServiceImpl) Update(ctx context.Context, id string, dto *UpdateIncidentDTO) (*Model, error) {
	updateModel := &UpdateModel{
		Title:           dto.Title,
		Content:         dto.Content,
		Style:           dto.Style,
		CreatedDate:     dto.CreatedDate,
		LastUpdatedDate: dto.LastUpdatedDate,
		Pin:             dto.Pin,
		Active:          dto.Active,
		StatusPageID:    dto.StatusPageID,
	}

	// Set last updated date if not provided
	if dto.LastUpdatedDate == nil {
		now := time.Now()
		updateModel.LastUpdatedDate = &now
	}

	updated, err := s.repository.Update(ctx, id, updateModel)
	if err != nil {
		s.logger.Errorw("Failed to update incident", "error", err, "id", id, "dto", dto)
		return nil, err
	}

	s.logger.Infow("Incident updated successfully", "id", updated.ID, "title", updated.Title)
	return updated, nil
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	err := s.repository.Delete(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to delete incident", "error", err, "id", id)
		return err
	}

	s.logger.Infow("Incident deleted successfully", "id", id)
	return nil
}
