package organization

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type OrganizationRepository interface {
	Create(ctx context.Context, organization *Organization) (*Organization, error)
	FindByID(ctx context.Context, id string) (*Organization, error)
	Update(ctx context.Context, id string, organization *Organization) error
	Delete(ctx context.Context, id string) error

	AddMember(ctx context.Context, orgUser *OrganizationUser) error
	RemoveMember(ctx context.Context, orgID, userID string) error
	UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error
	FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error)
	FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error)
	FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error)
}

type sqlModel struct {
	bun.BaseModel `bun:"table:organizations,alias:o"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type organizationUserSQLModel struct {
	bun.BaseModel `bun:"table:organization_users,alias:ou"`

	OrganizationID string    `bun:"organization_id,pk"`
	UserID         string    `bun:"user_id,pk"`
	Role           string    `bun:"role,notnull"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type SQLRepositoryImpl struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) OrganizationRepository {
	return &SQLRepositoryImpl{db: db}
}

func toDomainModel(sm *sqlModel) *Organization {
	return &Organization{
		ID:        sm.ID,
		Name:      sm.Name,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

func toSQLModel(m *Organization) *sqlModel {
	return &sqlModel{
		ID:        m.ID,
		Name:      m.Name,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func (r *SQLRepositoryImpl) Create(ctx context.Context, organization *Organization) (*Organization, error) {
	sm := toSQLModel(organization)
	if sm.ID == "" {
		sm.ID = uuid.New().String()
	}
	sm.CreatedAt = time.Now()
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewInsert().Model(sm).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string) (*Organization, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("id = ?", id).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return toDomainModel(sm), nil
}

func (r *SQLRepositoryImpl) Update(ctx context.Context, id string, organization *Organization) error {
	sm := toSQLModel(organization)
	sm.UpdatedAt = time.Now()

	_, err := r.db.NewUpdate().
		Model(sm).
		Where("id = ?", id).
		ExcludeColumn("id", "created_at").
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*sqlModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) AddMember(ctx context.Context, orgUser *OrganizationUser) error {
	sm := &organizationUserSQLModel{
		OrganizationID: orgUser.OrganizationID,
		UserID:         orgUser.UserID,
		Role:           string(orgUser.Role),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_, err := r.db.NewInsert().Model(sm).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) RemoveMember(ctx context.Context, orgID, userID string) error {
	_, err := r.db.NewDelete().
		Model((*organizationUserSQLModel)(nil)).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error {
	_, err := r.db.NewUpdate().
		Model((*organizationUserSQLModel)(nil)).
		Set("role = ?", string(role)).
		Set("updated_at = ?", time.Now()).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error) {
	var sms []*organizationUserSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("organization_id = ?", orgID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var users []*OrganizationUser
	for _, sm := range sms {
		users = append(users, &OrganizationUser{
			OrganizationID: sm.OrganizationID,
			UserID:         sm.UserID,
			Role:           Role(sm.Role),
			CreatedAt:      sm.CreatedAt,
			UpdatedAt:      sm.UpdatedAt,
		})
	}
	return users, nil
}

func (r *SQLRepositoryImpl) FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error) {
	var sms []*organizationUserSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var users []*OrganizationUser
	for _, sm := range sms {
		users = append(users, &OrganizationUser{
			OrganizationID: sm.OrganizationID,
			UserID:         sm.UserID,
			Role:           Role(sm.Role),
			CreatedAt:      sm.CreatedAt,
			UpdatedAt:      sm.UpdatedAt,
		})
	}
	return users, nil
}

func (r *SQLRepositoryImpl) FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error) {
	sm := new(organizationUserSQLModel)
	err := r.db.NewSelect().
		Model(sm).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return &OrganizationUser{
		OrganizationID: sm.OrganizationID,
		UserID:         sm.UserID,
		Role:           Role(sm.Role),
		CreatedAt:      sm.CreatedAt,
		UpdatedAt:      sm.UpdatedAt,
	}, nil
}
