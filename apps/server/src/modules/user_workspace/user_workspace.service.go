package user_workspace

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, userID, workspaceID, role string) (*Model, error)
	FindByUserID(ctx context.Context, userID string) ([]*Model, error)
	FindByWorkspaceID(ctx context.Context, workspaceID string) ([]*Model, error)
	FindByUserAndWorkspace(ctx context.Context, userID, workspaceID string) (*Model, error)
	UpdateRole(ctx context.Context, userID, workspaceID, role string) (*Model, error)
	RemoveUserFromWorkspace(ctx context.Context, userID, workspaceID string) error
	IsUserInWorkspace(ctx context.Context, userID, workspaceID string) (bool, error)
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
		logger: logger.Named("[user-workspace-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, userID, workspaceID, role string) (*Model, error) {
	// Check if relationship already exists
	existing, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("user is already in this workspace")
	}

	userWorkspace := &Model{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Role:        role,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	createdUserWorkspace, err := s.repo.Create(ctx, userWorkspace)
	if err != nil {
		s.logger.Errorw("Failed to create user-workspace relationship", "error", err, "userID", userID, "workspaceID", workspaceID)
		return nil, err
	}

	s.logger.Infow("User-workspace relationship created successfully", "userID", userID, "workspaceID", workspaceID, "role", role)
	return createdUserWorkspace, nil
}

func (s *ServiceImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	userWorkspaces, err := s.repo.FindByUserID(ctx, userID)
	if err != nil {
		s.logger.Errorw("Failed to find user workspaces", "error", err, "userID", userID)
		return nil, err
	}
	return userWorkspaces, nil
}

func (s *ServiceImpl) FindByWorkspaceID(ctx context.Context, workspaceID string) ([]*Model, error) {
	userWorkspaces, err := s.repo.FindByWorkspaceID(ctx, workspaceID)
	if err != nil {
		s.logger.Errorw("Failed to find workspace users", "error", err, "workspaceID", workspaceID)
		return nil, err
	}
	return userWorkspaces, nil
}

func (s *ServiceImpl) FindByUserAndWorkspace(ctx context.Context, userID, workspaceID string) (*Model, error) {
	userWorkspace, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		s.logger.Errorw("Failed to find user-workspace relationship", "error", err, "userID", userID, "workspaceID", workspaceID)
		return nil, err
	}
	return userWorkspace, nil
}

func (s *ServiceImpl) UpdateRole(ctx context.Context, userID, workspaceID, role string) (*Model, error) {
	// Check if relationship exists
	existing, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("user is not in this workspace")
	}

	updateModel := &UpdateModel{
		Role: &role,
	}

	err = s.repo.Update(ctx, userID, workspaceID, updateModel)
	if err != nil {
		s.logger.Errorw("Failed to update user role", "error", err, "userID", userID, "workspaceID", workspaceID)
		return nil, err
	}

	// Return updated relationship
	updatedUserWorkspace, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}

	s.logger.Infow("User role updated successfully", "userID", userID, "workspaceID", workspaceID, "role", role)
	return updatedUserWorkspace, nil
}

func (s *ServiceImpl) RemoveUserFromWorkspace(ctx context.Context, userID, workspaceID string) error {
	// Check if relationship exists
	existing, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	if existing == nil {
		return errors.New("user is not in this workspace")
	}

	err = s.repo.Delete(ctx, userID, workspaceID)
	if err != nil {
		s.logger.Errorw("Failed to remove user from workspace", "error", err, "userID", userID, "workspaceID", workspaceID)
		return err
	}

	s.logger.Infow("User removed from workspace successfully", "userID", userID, "workspaceID", workspaceID)
	return nil
}

func (s *ServiceImpl) IsUserInWorkspace(ctx context.Context, userID, workspaceID string) (bool, error) {
	userWorkspace, err := s.repo.FindByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return false, err
	}
	return userWorkspace != nil, nil
}

