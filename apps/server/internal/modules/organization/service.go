package organization

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, dto *CreateOrganizationDto, creatorUserID string) (*Organization, error)
	FindByID(ctx context.Context, id string) (*Organization, error)
	FindBySlug(ctx context.Context, slug string) (*Organization, error)
	Update(ctx context.Context, id string, dto *UpdateOrganizationDto) (*Organization, error)
	Delete(ctx context.Context, id string) error

	AddMember(ctx context.Context, orgID string, dto *AddMemberDto) error
	RemoveMember(ctx context.Context, orgID, userID string) error
	UpdateMemberRole(ctx context.Context, orgID, userID string, dto *UpdateMemberRoleDto) error
	FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error)
	FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error)
	FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error)
}

type ServiceImpl struct {
	repo   OrganizationRepository
	logger *zap.SugaredLogger
}

func NewService(repo OrganizationRepository, logger *zap.SugaredLogger) Service {
	return &ServiceImpl{
		repo:   repo,
		logger: logger.Named("[organization-service]"),
	}
}

func slugify(s string) string {
	// Convert to lowercase
	slug := strings.ToLower(s)
	// Replace spaces with dashes
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove non-alphanumeric characters (except dashes)
	reg := regexp.MustCompile("[^a-z0-9-]+")
	slug = reg.ReplaceAllString(slug, "")
	// Remove multiple consecutive dashes
	reg = regexp.MustCompile("-+")
	slug = reg.ReplaceAllString(slug, "-")
	// Trim dashes
	slug = strings.Trim(slug, "-")
	return slug
}

func (s *ServiceImpl) Create(ctx context.Context, dto *CreateOrganizationDto, creatorUserID string) (*Organization, error) {
	// Generate slug if not provided
	slug := dto.Slug
	if slug == "" {
		slug = slugify(dto.Name)
	}
	if slug == "" {
		// Fallback if name is empty or un-sluggable (unlikely)
		slug = "org-" + strings.ToLower(strings.ReplaceAll(dto.Name, " ", "-"))
	}

	org := &Organization{
		Name: dto.Name,
		Slug: slug,
	}

	if err := s.validateSlug(ctx, "", slug); err != nil {
		return nil, err
	}

	createdOrg, err := s.repo.Create(ctx, org)
	if err != nil {
		s.logger.Errorw("failed to create organization", "error", err)
		return nil, err
	}

	// Add creator as admin
	err = s.repo.AddMember(ctx, &OrganizationUser{
		OrganizationID: createdOrg.ID,
		UserID:         creatorUserID,
		Role:           RoleAdmin,
	})
	if err != nil {
		s.logger.Errorw("failed to add creator as admin", "org_id", createdOrg.ID, "user_id", creatorUserID, "error", err)
		// Try to rollback organization creation to avoid inconsistent state (basic compensation)
		_ = s.repo.Delete(ctx, createdOrg.ID)
		return nil, err
	}

	return createdOrg, nil
}

func (s *ServiceImpl) FindBySlug(ctx context.Context, slug string) (*Organization, error) {
	return s.repo.FindBySlug(ctx, slug)
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Organization, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *ServiceImpl) Update(ctx context.Context, id string, dto *UpdateOrganizationDto) (*Organization, error) {
	org, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if org == nil {
		// handle not found
		return nil, nil
	}

	if dto.Name != nil {
		org.Name = *dto.Name
	}
	if dto.Slug != nil {
		if err := s.validateSlug(ctx, id, *dto.Slug); err != nil {
			return nil, err
		}
		org.Slug = *dto.Slug
	}

	err = s.repo.Update(ctx, id, org)
	if err != nil {
		s.logger.Errorw("failed to update organization", "id", id, "error", err)
		return nil, err
	}

	return org, nil
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	// Logic to clean up monitors and other resources should be here or handled via cascade/events
	// For now just deleting the org
	return s.repo.Delete(ctx, id)
}

func (s *ServiceImpl) AddMember(ctx context.Context, orgID string, dto *AddMemberDto) error {
	// Verify if user exists (mocked or need user service dependency)
	// For now we assume UserID logic is handled by caller or we accept Email but need to resolve to ID.
	// Since the DTO has Email, we would need to look up User by Email.
	// TODO: Need User Service or Repository to lookup user by email.
	// For now, assuming AddMemberDto actually contains UserID for simplicity in this step, or we leave a TODO.
	// Correcting DTO usage: converting email to UserID is a requirement.

	// Assuming the DTO passed in MIGHT have UserID if we change it, but currently it has Email.
	// Let's assume we need to lookup the user.
	// Since I don't have UserService here yet, I will add a TODO note and assume the user exists for now
	// or fail if I can't look them up.
	// CRITICAL: The prompt didn't ask for UserService integration yet, so I will define the method but comment on the missing piece.

	s.logger.Warn("AddMember by Email not fully implemented without User lookup. Expecting pre-resolved UserID for now in separate method or fake it.")
	// Placeholder error to remind implementation
	return fmt.Errorf("user lookup by email not implemented")
}

func (s *ServiceImpl) RemoveMember(ctx context.Context, orgID, userID string) error {
	return s.repo.RemoveMember(ctx, orgID, userID)
}

func (s *ServiceImpl) UpdateMemberRole(ctx context.Context, orgID, userID string, dto *UpdateMemberRoleDto) error {
	return s.repo.UpdateMemberRole(ctx, orgID, userID, dto.Role)
}

func (s *ServiceImpl) FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error) {
	return s.repo.FindMembers(ctx, orgID)
}

func (s *ServiceImpl) FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error) {
	return s.repo.FindUserOrganizations(ctx, userID)
}

func (s *ServiceImpl) FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error) {
	return s.repo.FindMembership(ctx, orgID, userID)
}

// SlugAlreadyUsedError represents a validation error when a slug is already used
type SlugAlreadyUsedError struct {
	Code string `json:"code"`
	Slug string `json:"slug"`
}

func (e *SlugAlreadyUsedError) Error() string {
	return fmt.Sprintf(`{"code":"%s", "slug":"%s"}`, e.Code, e.Slug)
}

func slugAlreadyUsedError(slug string) *SlugAlreadyUsedError {
	return &SlugAlreadyUsedError{
		Code: "SLUG_EXISTS",
		Slug: slug,
	}
}

// validateSlug ensures that the provided slug is unique
func (s *ServiceImpl) validateSlug(ctx context.Context, orgID string, slug string) error {
	existing, err := s.repo.FindBySlug(ctx, slug)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != orgID {
		return slugAlreadyUsedError(slug)
	}
	return nil
}
