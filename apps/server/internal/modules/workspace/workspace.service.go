package workspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, name string) (*Model, error)
	CreateDefault(ctx context.Context, userEmail string) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByIDs(ctx context.Context, workspaceIDs []string) ([]*Model, error)
	FindAll(ctx context.Context, page int, limit int) ([]*Model, error)
	Update(ctx context.Context, id string, name string) (*Model, error)
	Delete(ctx context.Context, id string) error
}

type ServiceImpl struct {
	repo   Repository
	logger *zap.SugaredLogger
}

func NewService(
	repo Repository,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repo:   repo,
		logger: logger.Named("[workspace-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, name string) (*Model, error) {
	workspace := &Model{
		Name:      name,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	createdWorkspace, err := s.repo.Create(ctx, workspace)
	if err != nil {
		s.logger.Errorw("Failed to create workspace", "error", err, "name", name)
		return nil, err
	}

	s.logger.Infow("Workspace created successfully", "workspaceID", createdWorkspace.ID, "name", name)
	return createdWorkspace, nil
}

func (s *ServiceImpl) CreateDefault(ctx context.Context, userEmail string) (*Model, error) {
	// Create default workspace name based on user email
	workspaceName := fmt.Sprintf("%s's Workspace", userEmail)

	return s.Create(ctx, workspaceName)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	workspace, err := s.repo.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to find workspace by ID", "error", err, "id", id)
		return nil, err
	}
	if workspace == nil {
		return nil, errors.New("workspace not found")
	}
	return workspace, nil
}

func (s *ServiceImpl) FindByIDs(ctx context.Context, workspaceIDs []string) ([]*Model, error) {
	workspaces, err := s.repo.FindByIDs(ctx, workspaceIDs)
	if err != nil {
		s.logger.Errorw("Failed to find workspaces by IDs", "error", err, "workspaceIDs", workspaceIDs)
		return nil, err
	}
	return workspaces, nil
}

func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int) ([]*Model, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	workspaces, err := s.repo.FindAll(ctx, page, limit)
	if err != nil {
		s.logger.Errorw("Failed to find all workspaces", "error", err)
		return nil, err
	}
	return workspaces, nil
}

func (s *ServiceImpl) Update(ctx context.Context, id string, name string) (*Model, error) {
	// Check if workspace exists
	existingWorkspace, err := s.repo.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to find workspace for update", "error", err, "id", id)
		return nil, err
	}
	if existingWorkspace == nil {
		return nil, errors.New("workspace not found")
	}

	updateModel := &UpdateModel{
		Name: &name,
	}

	err = s.repo.Update(ctx, id, updateModel)
	if err != nil {
		s.logger.Errorw("Failed to update workspace", "error", err, "id", id)
		return nil, err
	}

	// Return updated workspace
	updatedWorkspace, err := s.repo.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to fetch updated workspace", "error", err, "id", id)
		return nil, err
	}

	s.logger.Infow("Workspace updated successfully", "workspaceID", id)
	return updatedWorkspace, nil
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	// Check if workspace exists
	existingWorkspace, err := s.repo.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to find workspace for deletion", "error", err, "id", id)
		return err
	}
	if existingWorkspace == nil {
		return errors.New("workspace not found")
	}

	err = s.repo.Delete(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to delete workspace", "error", err, "id", id)
		return err
	}

	s.logger.Infow("Workspace deleted successfully", "workspaceID", id)
	return nil
}
