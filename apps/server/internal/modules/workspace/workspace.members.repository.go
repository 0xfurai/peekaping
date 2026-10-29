package workspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"peekaping/internal/dbtx"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// MemberRepository stores the people of a workspace: who is a member and with which role. Every
// method joins the transaction its ctx carries.
type MemberRepository interface {
	// ListMembers returns the workspace's members with their emails, the first to join first
	ListMembers(ctx context.Context, id ID) ([]Member, error)
	// FindMember returns the user's place in the workspace, or nil when they have none
	FindMember(ctx context.Context, id ID, userID string) (*Member, error)
	// UpdateRole gives the member the role. It returns false when the user is not a member.
	UpdateRole(ctx context.Context, id ID, userID string, role Role) (bool, error)
	// RemoveMember ends the user's membership. It returns false when the user is not a member.
	RemoveMember(ctx context.Context, id ID, userID string) (bool, error)
	// LockOwners returns the user IDs of the workspace's owners. On PostgreSQL it holds their rows
	// until the transaction ends, so that two owners can't each be demoted by a change that saw the
	// other still in place.
	LockOwners(ctx context.Context, id ID) ([]string, error)
	// UserEmail returns the email of the user's account, or "" for a user that doesn't exist
	UserEmail(ctx context.Context, userID string) (string, error)
	// EmailVerified is whether the user's account has a verified email address. It is false for a
	// user that doesn't exist.
	EmailVerified(ctx context.Context, userID string) (bool, error)
	// LockEmailVerified is EmailVerified that, on PostgreSQL, holds the user's row until the
	// transaction ends: a verification that is in flight is waited for, and one that starts after
	// waits for this transaction, so it finds what this transaction made
	LockEmailVerified(ctx context.Context, userID string) (bool, error)
	// HasMemberWithEmail is whether a member of the workspace has the email, ignoring case
	HasMemberWithEmail(ctx context.Context, id ID, email string) (bool, error)
}

type SQLMemberRepository struct {
	db *bun.DB
}

func NewSQLMemberRepository(db *bun.DB) MemberRepository {
	return &SQLMemberRepository{db: db}
}

type memberRow struct {
	UserID   string    `bun:"user_id"`
	Email    string    `bun:"email"`
	Role     string    `bun:"role"`
	JoinedAt time.Time `bun:"joined_at"`
}

func (r memberRow) toDomain() Member {
	return Member{UserID: r.UserID, Email: r.Email, Role: Role(r.Role), JoinedAt: r.JoinedAt}
}

func (r *SQLMemberRepository) membersOf(ctx context.Context, id ID) *bun.SelectQuery {
	return dbtx.DB(ctx, r.db).NewSelect().
		TableExpr("workspace_members AS m").
		Join("JOIN users AS u ON u.id = m.user_id").
		ColumnExpr("m.user_id, u.email, m.role, m.joined_at").
		Where("m.workspace_id = ?", string(id))
}

func (r *SQLMemberRepository) ListMembers(ctx context.Context, id ID) ([]Member, error) {
	var rows []memberRow
	if err := r.membersOf(ctx, id).Order("m.joined_at", "m.user_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	members := make([]Member, len(rows))
	for i, row := range rows {
		members[i] = row.toDomain()
	}
	return members, nil
}

func (r *SQLMemberRepository) FindMember(ctx context.Context, id ID, userID string) (*Member, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, nil
	}
	var row memberRow
	err := r.membersOf(ctx, id).Where("m.user_id = ?", userID).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	member := row.toDomain()
	return &member, nil
}

func (r *SQLMemberRepository) UpdateRole(ctx context.Context, id ID, userID string, role Role) (bool, error) {
	res, err := dbtx.DB(ctx, r.db).NewUpdate().Table("workspace_members").
		Set("role = ?", string(role)).
		Where("workspace_id = ?", string(id)).Where("user_id = ?", userID).
		Exec(ctx)
	return changed(res, err)
}

func (r *SQLMemberRepository) RemoveMember(ctx context.Context, id ID, userID string) (bool, error) {
	res, err := dbtx.DB(ctx, r.db).NewDelete().Table("workspace_members").
		Where("workspace_id = ?", string(id)).Where("user_id = ?", userID).
		Exec(ctx)
	return changed(res, err)
}

func (r *SQLMemberRepository) LockOwners(ctx context.Context, id ID) ([]string, error) {
	q := dbtx.DB(ctx, r.db).NewSelect().Table("workspace_members").Column("user_id").
		Where("workspace_id = ?", string(id)).Where("role = ?", string(RoleOwner)).Order("user_id")
	if r.db.Dialect().Name() == dialect.PG {
		q = q.For("UPDATE")
	}
	var owners []string
	if err := q.Scan(ctx, &owners); err != nil {
		return nil, err
	}
	return owners, nil
}

func (r *SQLMemberRepository) HasMemberWithEmail(ctx context.Context, id ID, email string) (bool, error) {
	return r.membersOf(ctx, id).Where("lower(u.email) = ?", strings.ToLower(email)).Exists(ctx)
}

func changed(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *SQLMemberRepository) UserEmail(ctx context.Context, userID string) (string, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return "", nil
	}
	var email string
	err := dbtx.DB(ctx, r.db).NewSelect().Table("users").Column("email").Where("id = ?", userID).Scan(ctx, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return email, err
}

func (r *SQLMemberRepository) EmailVerified(ctx context.Context, userID string) (bool, error) {
	return r.emailVerified(ctx, userID, false)
}

func (r *SQLMemberRepository) LockEmailVerified(ctx context.Context, userID string) (bool, error) {
	return r.emailVerified(ctx, userID, true)
}

func (r *SQLMemberRepository) emailVerified(ctx context.Context, userID string, lock bool) (bool, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return false, nil
	}
	var verifiedAt *time.Time
	q := dbtx.DB(ctx, r.db).NewSelect().Table("users").Column("email_verified_at").Where("id = ?", userID)
	if lock && r.db.Dialect().Name() == dialect.PG {
		q = q.For("UPDATE")
	}
	err := q.Scan(ctx, &verifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return verifiedAt != nil, err
}
