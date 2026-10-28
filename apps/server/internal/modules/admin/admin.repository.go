package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// Repository reads and changes users for the admin area. Every method joins the transaction its ctx
// carries (see internal/dbtx).
type Repository interface {
	// ListUsers returns a page of users, the oldest first, with how many users match: those whose
	// email contains q, ignoring case, or all of them for an empty q. Memberships are not filled.
	ListUsers(ctx context.Context, q string, page, limit int) (users []*User, total int, err error)
	// FindUser returns the user, memberships included, or nil when there is none with the ID
	FindUser(ctx context.Context, id string) (*User, error)
	// MembershipsAnyWorkspace fills the memberships of the users
	MembershipsAnyWorkspace(ctx context.Context, users []*User) error
	// LockInstanceAdmins returns the IDs of the instance admins. On PostgreSQL it holds their rows
	// until the transaction ends, so that two admins can't each lose the flag by a change that saw
	// the other still in place.
	LockInstanceAdmins(ctx context.Context) ([]string, error)
	SetInstanceAdmin(ctx context.Context, id string, on bool) error
	// SetActive disables or enables the user. Disabling also ends the sessions issued before
	// endSessionsAt, which the caller passes then and nil otherwise.
	SetActive(ctx context.Context, id string, active bool, endSessionsAt *time.Time) error
	// ClearTwoFA turns the user's 2FA off and forgets its secret
	ClearTwoFA(ctx context.Context, id string) error
	// WorkspacesOwnedAloneAnyWorkspace returns the workspaces the user is the only owner of. On PostgreSQL it
	// first holds the owners of the user's workspaces until the transaction ends, so that two
	// deletions of the two owners of a workspace can't both pass.
	WorkspacesOwnedAloneAnyWorkspace(ctx context.Context, userID string) ([]WorkspaceRef, error)
	// DeleteUser deletes the user. Their memberships and account links go with them.
	DeleteUser(ctx context.Context, id string) error
}

type SQLRepository struct {
	db *bun.DB
}

func NewSQLRepository(db *bun.DB) Repository {
	return &SQLRepository{db: db}
}

type userRow struct {
	ID              string     `bun:"id"`
	Email           string     `bun:"email"`
	Active          bool       `bun:"active"`
	InstanceAdmin   bool       `bun:"instance_admin"`
	TwoFAStatus     bool       `bun:"twofa_status"`
	TwoFASecret     string     `bun:"twofa_secret"`
	EmailVerifiedAt *time.Time `bun:"email_verified_at"`
	CreatedAt       time.Time  `bun:"created_at"`
	LastLoginAt     *time.Time `bun:"last_login_at"`
}

func (r userRow) toDomain() *User {
	return &User{
		ID:            r.ID,
		Email:         r.Email,
		Active:        r.Active,
		InstanceAdmin: r.InstanceAdmin,
		// What the login checks: a status without a secret is not a second factor
		TwoFactor:     r.TwoFAStatus && r.TwoFASecret != "",
		EmailVerified: r.EmailVerifiedAt != nil,
		CreatedAt:     r.CreatedAt,
		LastLoginAt:   r.LastLoginAt,
		Memberships:   []Membership{},
	}
}

const userColumns = "id, email, active, instance_admin, twofa_status, twofa_secret, email_verified_at, created_at, last_login_at"

// likeEscaper makes a search text literal in a LIKE pattern
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (r *SQLRepository) ListUsers(ctx context.Context, q string, page, limit int) ([]*User, int, error) {
	query := dbtx.DB(ctx, r.db).NewSelect().Table("users")
	if q = strings.TrimSpace(q); q != "" {
		query = query.Where(`lower(email) LIKE ? ESCAPE '\'`, "%"+likeEscaper.Replace(strings.ToLower(q))+"%")
	}
	var rows []userRow
	total, err := query.ColumnExpr(userColumns).Order("created_at", "id").Limit(limit).Offset(page*limit).ScanAndCount(ctx, &rows)
	if err != nil {
		return nil, 0, err
	}
	users := make([]*User, len(rows))
	for i, row := range rows {
		users[i] = row.toDomain()
	}
	return users, total, nil
}

func (r *SQLRepository) FindUser(ctx context.Context, id string) (*User, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil
	}
	var row userRow
	err := dbtx.DB(ctx, r.db).NewSelect().Table("users").ColumnExpr(userColumns).Where("id = ?", id).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	user := row.toDomain()
	if err := r.MembershipsAnyWorkspace(ctx, []*User{user}); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *SQLRepository) MembershipsAnyWorkspace(ctx context.Context, users []*User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, len(users))
	byID := make(map[string]*User, len(users))
	for i, u := range users {
		ids[i] = u.ID
		byID[u.ID] = u
	}
	var rows []struct {
		UserID        string `bun:"user_id"`
		WorkspaceID   string `bun:"workspace_id"`
		WorkspaceName string `bun:"name"`
		Role          string `bun:"role"`
	}
	err := dbtx.DB(ctx, r.db).NewSelect().
		TableExpr("workspace_members AS m").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		ColumnExpr("m.user_id, m.workspace_id, w.name, m.role").
		Where("m.user_id IN (?)", bun.In(ids)).
		Order("m.joined_at", "m.workspace_id").
		Scan(ctx, &rows)
	if err != nil {
		return err
	}
	for _, row := range rows {
		u := byID[row.UserID]
		u.Memberships = append(u.Memberships, Membership{WorkspaceID: workspace.ID(row.WorkspaceID), WorkspaceName: row.WorkspaceName, Role: workspace.Role(row.Role)})
	}
	return nil
}

func (r *SQLRepository) LockInstanceAdmins(ctx context.Context) ([]string, error) {
	q := dbtx.DB(ctx, r.db).NewSelect().Table("users").Column("id").Where("instance_admin = ?", true).Order("id")
	if r.db.Dialect().Name() == dialect.PG {
		q = q.For("UPDATE")
	}
	var ids []string
	if err := q.Scan(ctx, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *SQLRepository) SetInstanceAdmin(ctx context.Context, id string, on bool) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Table("users").
		Set("instance_admin = ?", on).Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepository) SetActive(ctx context.Context, id string, active bool, endSessionsAt *time.Time) error {
	q := dbtx.DB(ctx, r.db).NewUpdate().Table("users").
		Set("active = ?", active).Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", id)
	if endSessionsAt != nil {
		q = q.Set("tokens_valid_after = ?", endSessionsAt.UTC())
	}
	_, err := q.Exec(ctx)
	return err
}

func (r *SQLRepository) ClearTwoFA(ctx context.Context, id string) error {
	_, err := dbtx.DB(ctx, r.db).NewUpdate().Table("users").
		Set("twofa_secret = ?", "").Set("twofa_status = ?", false).Set("twofa_last_token = ?", "").
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", id).Exec(ctx)
	return err
}

func (r *SQLRepository) WorkspacesOwnedAloneAnyWorkspace(ctx context.Context, userID string) ([]WorkspaceRef, error) {
	db := dbtx.DB(ctx, r.db)
	if r.db.Dialect().Name() == dialect.PG {
		// Lock the owners of every workspace the user owns, in a fixed order, as the roles of a
		// workspace's members do (workspace.MemberRepository.LockOwners)
		_, err := db.ExecContext(ctx, `SELECT o.user_id FROM workspace_members o
			WHERE o.role = 'owner' AND o.workspace_id IN (SELECT workspace_id FROM workspace_members WHERE user_id = ? AND role = 'owner')
			ORDER BY o.workspace_id, o.user_id FOR UPDATE`, userID)
		if err != nil {
			return nil, err
		}
	}
	var rows []struct {
		ID   string `bun:"id"`
		Name string `bun:"name"`
	}
	err := db.NewSelect().
		TableExpr("workspace_members AS m").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		ColumnExpr("w.id, w.name").
		Where("m.user_id = ?", userID).Where("m.role = ?", string(workspace.RoleOwner)).
		Where("(SELECT COUNT(*) FROM workspace_members o WHERE o.workspace_id = m.workspace_id AND o.role = ?) = 1", string(workspace.RoleOwner)).
		Order("w.created_at", "w.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	refs := make([]WorkspaceRef, len(rows))
	for i, row := range rows {
		refs[i] = WorkspaceRef{ID: workspace.ID(row.ID), Name: row.Name}
	}
	return refs, nil
}

func (r *SQLRepository) DeleteUser(ctx context.Context, id string) error {
	_, err := dbtx.DB(ctx, r.db).NewDelete().Table("users").Where("id = ?", id).Exec(ctx)
	return err
}
