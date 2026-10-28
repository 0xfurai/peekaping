package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// InviteRepository stores the invites to workspaces. Every method joins the transaction its ctx
// carries. A token is stored only as the hash the caller computes.
type InviteRepository interface {
	// CreateInvite stores the invite with the hash of its token
	CreateInvite(ctx context.Context, invite *Invite, tokenHash string) error
	// ListPending returns the workspace's invites that were neither used nor revoked, the newest
	// first. Some of them may have expired.
	ListPending(ctx context.Context, id ID) ([]Invite, error)
	// FindByTokenHashAnyWorkspace returns the invite a token's hash names, with the name of its
	// workspace, or nil. The token alone says the workspace, as an API key's does, so this reads
	// across workspaces.
	FindByTokenHashAnyWorkspace(ctx context.Context, tokenHash string) (*Invite, error)
	// Revoke revokes the workspace's pending invite. It returns false when the workspace has no
	// such invite.
	Revoke(ctx context.Context, id ID, inviteID string, at time.Time) (bool, error)
	// RevokePendingForEmail revokes the workspace's pending invites for the email
	RevokePendingForEmail(ctx context.Context, id ID, email string, at time.Time) error
	// MarkAccepted records that the user joined through the invite. It returns false when the
	// invite was used or revoked already, which is how a link works only once.
	MarkAccepted(ctx context.Context, inviteID, userID string, at time.Time) (bool, error)
}

type inviteModel struct {
	bun.BaseModel `bun:"table:workspace_invites,alias:i"`

	ID          string     `bun:"id,pk"`
	WorkspaceID string     `bun:"workspace_id,notnull"`
	Email       string     `bun:"email,notnull"`
	Role        string     `bun:"role,notnull"`
	TokenHash   string     `bun:"token_hash,notnull"`
	InvitedBy   *string    `bun:"invited_by"`
	CreatedAt   time.Time  `bun:"created_at,notnull"`
	ExpiresAt   time.Time  `bun:"expires_at,notnull"`
	AcceptedAt  *time.Time `bun:"accepted_at"`
	AcceptedBy  *string    `bun:"accepted_by"`
	RevokedAt   *time.Time `bun:"revoked_at"`
}

// inviteRow is an invite read together with the name of its workspace
type inviteRow struct {
	ID            string     `bun:"id"`
	WorkspaceID   string     `bun:"workspace_id"`
	WorkspaceName string     `bun:"workspace_name"`
	Email         string     `bun:"email"`
	Role          string     `bun:"role"`
	InvitedBy     *string    `bun:"invited_by"`
	CreatedAt     time.Time  `bun:"created_at"`
	ExpiresAt     time.Time  `bun:"expires_at"`
	AcceptedAt    *time.Time `bun:"accepted_at"`
	AcceptedBy    *string    `bun:"accepted_by"`
	RevokedAt     *time.Time `bun:"revoked_at"`
}

func (r inviteRow) toDomain() Invite {
	return Invite{
		ID: r.ID, WorkspaceID: ID(r.WorkspaceID), WorkspaceName: r.WorkspaceName, Email: r.Email, Role: Role(r.Role),
		InvitedBy: r.InvitedBy, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		AcceptedAt: r.AcceptedAt, AcceptedBy: r.AcceptedBy, RevokedAt: r.RevokedAt,
	}
}

const inviteColumns = "i.id, i.workspace_id, w.name AS workspace_name, i.email, i.role, i.invited_by, i.created_at, i.expires_at, i.accepted_at, i.accepted_by, i.revoked_at"

type SQLInviteRepository struct {
	db *bun.DB
}

func NewSQLInviteRepository(db *bun.DB) InviteRepository {
	return &SQLInviteRepository{db: db}
}

func (r *SQLInviteRepository) invites(ctx context.Context) *bun.SelectQuery {
	return dbtx.DB(ctx, r.db).NewSelect().
		TableExpr("workspace_invites AS i").
		Join("JOIN workspaces AS w ON w.id = i.workspace_id").
		ColumnExpr(inviteColumns)
}

func (r *SQLInviteRepository) CreateInvite(ctx context.Context, invite *Invite, tokenHash string) error {
	m := &inviteModel{
		ID: invite.ID, WorkspaceID: string(invite.WorkspaceID), Email: invite.Email, Role: string(invite.Role),
		TokenHash: tokenHash, InvitedBy: invite.InvitedBy, CreatedAt: invite.CreatedAt.UTC(), ExpiresAt: invite.ExpiresAt.UTC(),
	}
	_, err := dbtx.DB(ctx, r.db).NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *SQLInviteRepository) ListPending(ctx context.Context, id ID) ([]Invite, error) {
	var rows []inviteRow
	err := r.invites(ctx).
		Where("i.workspace_id = ?", string(id)).
		Where("i.accepted_at IS NULL").Where("i.revoked_at IS NULL").
		Order("i.created_at DESC", "i.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	invites := make([]Invite, len(rows))
	for i, row := range rows {
		invites[i] = row.toDomain()
	}
	return invites, nil
}

func (r *SQLInviteRepository) FindByTokenHashAnyWorkspace(ctx context.Context, tokenHash string) (*Invite, error) {
	var row inviteRow
	err := r.invites(ctx).Where("i.token_hash = ?", tokenHash).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	invite := row.toDomain()
	return &invite, nil
}

func (r *SQLInviteRepository) Revoke(ctx context.Context, id ID, inviteID string, at time.Time) (bool, error) {
	if _, err := uuid.Parse(inviteID); err != nil {
		return false, nil
	}
	res, err := dbtx.DB(ctx, r.db).NewUpdate().Table("workspace_invites").
		Set("revoked_at = ?", at.UTC()).
		Where("workspace_id = ?", string(id)).Where("id = ?", inviteID).
		Where("accepted_at IS NULL").Where("revoked_at IS NULL").
		Exec(ctx)
	return changed(res, err)
}

func (r *SQLInviteRepository) RevokePendingForEmail(ctx context.Context, id ID, email string, at time.Time) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Table("workspace_invites").
		Set("revoked_at = ?", at.UTC()).
		Where("workspace_id = ?", string(id)).Where("email = ?", email).
		Where("accepted_at IS NULL").Where("revoked_at IS NULL").
		Exec(ctx)
	return err
}

func (r *SQLInviteRepository) MarkAccepted(ctx context.Context, inviteID, userID string, at time.Time) (bool, error) {
	res, err := dbtx.DB(ctx, r.db).NewUpdate().Table("workspace_invites").
		Set("accepted_at = ?", at.UTC()).Set("accepted_by = ?", userID).
		Where("id = ?", inviteID).Where("accepted_at IS NULL").Where("revoked_at IS NULL").
		Exec(ctx)
	return changed(res, err)
}
