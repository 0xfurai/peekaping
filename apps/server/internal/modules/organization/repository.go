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
	FindBySlug(ctx context.Context, slug string) (*Organization, error)
	Update(ctx context.Context, id string, organization *Organization) error
	Delete(ctx context.Context, id string) error

	AddMember(ctx context.Context, orgUser *OrganizationUser) error
	RemoveMember(ctx context.Context, orgID, userID string) error
	UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error
	FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error)
	FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error)
	FindMembership(ctx context.Context, orgID, userID string) (*OrganizationUser, error)

	CreateInvitation(ctx context.Context, invitation *Invitation) error
	FindInvitations(ctx context.Context, orgID string) ([]*Invitation, error)
	FindInvitationByToken(ctx context.Context, token string) (*Invitation, error)
	FindInvitationsByEmail(ctx context.Context, email string) ([]*Invitation, error)
	UpdateInvitationStatus(ctx context.Context, id string, status InvitationStatus) error
}

type sqlModel struct {
	bun.BaseModel `bun:"table:organizations,alias:o"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name,notnull"`
	Slug      string    `bun:"slug,unique,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type userSQLModel struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            string `bun:"id,pk"`
	Email         string `bun:"email"`
}

type organizationUserSQLModel struct {
	bun.BaseModel `bun:"table:organization_users,alias:ou"`

	OrganizationID string        `bun:"organization_id,pk"`
	UserID         string        `bun:"user_id,pk"`
	Role           string        `bun:"role,notnull"`
	CreatedAt      time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time     `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	User           *userSQLModel `bun:"rel:belongs-to,join:user_id=id"`
	Organization   *sqlModel     `bun:"rel:belongs-to,join:organization_id=id"`
}

type invitationSQLModel struct {
	bun.BaseModel `bun:"table:invitations,alias:i"`

	ID             string    `bun:"id,pk"`
	OrganizationID string    `bun:"organization_id,notnull"`
	Email          string    `bun:"email,notnull"`
	Role           string    `bun:"role,notnull"`
	Token          string    `bun:"token,unique,notnull"`
	Status         string    `bun:"status,notnull"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	ExpiresAt      time.Time `bun:"expires_at,notnull"`
	Organization   *sqlModel `bun:"rel:belongs-to,join:organization_id=id"`
}

func (r *SQLRepositoryImpl) FindMembers(ctx context.Context, orgID string) ([]*OrganizationUser, error) {
	var sms []*organizationUserSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Relation("User").
		Relation("Organization").
		Where("organization_id = ?", orgID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var users []*OrganizationUser
	for _, sm := range sms {
		domainUser := &OrganizationUser{
			OrganizationID: sm.OrganizationID,
			UserID:         sm.UserID,
			Role:           Role(sm.Role),
			CreatedAt:      sm.CreatedAt,
			UpdatedAt:      sm.UpdatedAt,
		}
		if sm.User != nil {
			domainUser.User = &User{
				ID:    sm.User.ID,
				Email: sm.User.Email,
			}
		}
		if sm.Organization != nil {
			domainUser.Organization = toDomainModel(sm.Organization)
		}
		users = append(users, domainUser)
	}
	return users, nil
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
		Slug:      sm.Slug,
		CreatedAt: sm.CreatedAt,
		UpdatedAt: sm.UpdatedAt,
	}
}

func toSQLModel(m *Organization) *sqlModel {
	return &sqlModel{
		ID:        m.ID,
		Name:      m.Name,
		Slug:      m.Slug,
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

func (r *SQLRepositoryImpl) FindBySlug(ctx context.Context, slug string) (*Organization, error) {
	sm := new(sqlModel)
	err := r.db.NewSelect().Model(sm).Where("slug = ?", slug).Scan(ctx)
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

func (r *SQLRepositoryImpl) FindUserOrganizations(ctx context.Context, userID string) ([]*OrganizationUser, error) {
	var sms []*organizationUserSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Relation("Organization").
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var users []*OrganizationUser
	for _, sm := range sms {
		domainUser := &OrganizationUser{
			OrganizationID: sm.OrganizationID,
			UserID:         sm.UserID,
			Role:           Role(sm.Role),
			CreatedAt:      sm.CreatedAt,
			UpdatedAt:      sm.UpdatedAt,
		}
		if sm.Organization != nil {
			domainUser.Organization = toDomainModel(sm.Organization)
		}
		users = append(users, domainUser)
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

func (r *SQLRepositoryImpl) CreateInvitation(ctx context.Context, invitation *Invitation) error {
	sm := &invitationSQLModel{
		ID:             invitation.ID,
		OrganizationID: invitation.OrganizationID,
		Email:          invitation.Email,
		Role:           string(invitation.Role),
		Token:          invitation.Token,
		Status:         string(invitation.Status),
		CreatedAt:      invitation.CreatedAt,
		ExpiresAt:      invitation.ExpiresAt,
	}

	_, err := r.db.NewInsert().Model(sm).Exec(ctx)
	return err
}

func (r *SQLRepositoryImpl) FindInvitations(ctx context.Context, orgID string) ([]*Invitation, error) {
	var sms []*invitationSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Where("organization_id = ?", orgID).
		Where("status = ?", "pending").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var invitations []*Invitation
	for _, sm := range sms {
		invitations = append(invitations, &Invitation{
			ID:             sm.ID,
			OrganizationID: sm.OrganizationID,
			Email:          sm.Email,
			Role:           Role(sm.Role),
			Token:          sm.Token,
			Status:         InvitationStatus(sm.Status),
			CreatedAt:      sm.CreatedAt,
			ExpiresAt:      sm.ExpiresAt,
		})
	}
	return invitations, nil
}

func (r *SQLRepositoryImpl) FindInvitationByToken(ctx context.Context, token string) (*Invitation, error) {
	sm := new(invitationSQLModel)
	err := r.db.NewSelect().
		Model(sm).
		Relation("Organization").
		Where("token = ?", token).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	inv := &Invitation{
		ID:             sm.ID,
		OrganizationID: sm.OrganizationID,
		Email:          sm.Email,
		Role:           Role(sm.Role),
		Token:          sm.Token,
		Status:         InvitationStatus(sm.Status),
		CreatedAt:      sm.CreatedAt,
		ExpiresAt:      sm.ExpiresAt,
	}

	if sm.Organization != nil {
		inv.Organization = toDomainModel(sm.Organization)
	}

	return inv, nil
}

func (r *SQLRepositoryImpl) FindInvitationsByEmail(ctx context.Context, email string) ([]*Invitation, error) {
	var sms []*invitationSQLModel
	err := r.db.NewSelect().
		Model(&sms).
		Relation("Organization").
		Where("email = ?", email).
		Where("status = ?", "pending").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	var invitations []*Invitation
	for _, sm := range sms {
		inv := &Invitation{
			ID:             sm.ID,
			OrganizationID: sm.OrganizationID,
			Email:          sm.Email,
			Role:           Role(sm.Role),
			Token:          sm.Token,
			Status:         InvitationStatus(sm.Status),
			CreatedAt:      sm.CreatedAt,
			ExpiresAt:      sm.ExpiresAt,
		}
		if sm.Organization != nil {
			inv.Organization = toDomainModel(sm.Organization)
		}
		invitations = append(invitations, inv)
	}
	return invitations, nil
}

func (r *SQLRepositoryImpl) UpdateInvitationStatus(ctx context.Context, id string, status InvitationStatus) error {
	_, err := r.db.NewUpdate().
		Model((*invitationSQLModel)(nil)).
		Set("status = ?", string(status)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}
