package organization

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type MockOrganizationRepository struct {
	mock.Mock
}

func (m *MockOrganizationRepository) Create(ctx context.Context, organization *Organization) (*Organization, error) {
	args := m.Called(ctx, organization)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Organization), args.Error(1)
}

func (m *MockOrganizationRepository) FindByID(ctx context.Context, id string) (*Organization, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*Organization), args.Error(1)
}

func (m *MockOrganizationRepository) Update(ctx context.Context, id string, organization *Organization) error {
	args := m.Called(ctx, id, organization)
	return args.Error(0)
}

func (m *MockOrganizationRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockOrganizationRepository) AddMember(ctx context.Context, orgUser *OrganizationUser) error {
	args := m.Called(ctx, orgUser)
	return args.Error(0)
}

func (m *MockOrganizationRepository) RemoveMember(ctx context.Context, orgID, userID string) error {
	args := m.Called(ctx, orgID, userID)
	return args.Error(0)
}

func (m *MockOrganizationRepository) UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error {
	args := m.Called(ctx, orgID, userID, role)
	return args.Error(0)
}

func (m *MockOrganizationRepository) FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error) {
	args := m.Called(ctx, orgID)
	return args.Get(0).([]*OrganizationUser), args.Error(1)
}

func (m *MockOrganizationRepository) FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]*OrganizationUser), args.Error(1)
}

func (m *MockOrganizationRepository) FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error) {
	args := m.Called(ctx, orgID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*OrganizationUser), args.Error(1)
}

func setupService() (*ServiceImpl, *MockOrganizationRepository) {
	mockRepo := &MockOrganizationRepository{}
	logger := zap.NewNop().Sugar()
	service := NewService(mockRepo, logger).(*ServiceImpl)
	return service, mockRepo
}

func TestOrganizationService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		service, mockRepo := setupService()
		dto := &CreateOrganizationDto{
			Name: "Test Org",
		}
		creatorID := "user123"

		expectedOrg := &Organization{
			ID:        "org123",
			Name:      dto.Name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(o *Organization) bool {
			return o.Name == dto.Name
		})).Return(expectedOrg, nil)

		mockRepo.On("AddMember", ctx, mock.MatchedBy(func(ou *OrganizationUser) bool {
			return ou.OrganizationID == expectedOrg.ID &&
				ou.UserID == creatorID &&
				ou.Role == RoleAdmin
		})).Return(nil)

		result, err := service.Create(ctx, dto, creatorID)

		assert.NoError(t, err)
		assert.Equal(t, expectedOrg, result)
		mockRepo.AssertExpectations(t)
	})

	t.Run("rollback on member add failure", func(t *testing.T) {
		service, mockRepo := setupService()
		dto := &CreateOrganizationDto{Name: "Fail Org"}
		creatorID := "user123"

		expectedOrg := &Organization{ID: "org123", Name: "Fail Org"}

		mockRepo.On("Create", ctx, mock.Anything).Return(expectedOrg, nil)
		mockRepo.On("AddMember", ctx, mock.Anything).Return(assert.AnError)
		mockRepo.On("Delete", ctx, expectedOrg.ID).Return(nil)

		result, err := service.Create(ctx, dto, creatorID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRepo.AssertExpectations(t)
	})
}
