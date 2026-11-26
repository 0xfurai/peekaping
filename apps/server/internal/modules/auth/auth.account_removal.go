package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"peekaping/internal/dbtx"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"go.uber.org/zap"
)

var (
	// ErrLastInstanceAdmin is for a deletion that would leave the install with no instance admin who
	// can sign in (one with the flag and an active account). It answers 409.
	ErrLastInstanceAdmin = errors.New("the install needs at least one instance admin")
	// ErrLastOwner is for a deletion of a user who is the last owner of a workspace. The error that
	// wraps it, LastOwnerError, names the workspaces. It answers 409.
	ErrLastOwner = errors.New("the user is the last owner of a workspace")
)

// LastOwnerError is the ErrLastOwner of a user who is the only owner of the Workspaces
type LastOwnerError struct {
	Workspaces []WorkspaceRef
}

func (e *LastOwnerError) Error() string {
	names := make([]string, len(e.Workspaces))
	for i, w := range e.Workspaces {
		names[i] = fmt.Sprintf("%q", w.Name)
	}
	return fmt.Sprintf("%s: %s", ErrLastOwner, strings.Join(names, ", "))
}

func (e *LastOwnerError) Is(target error) bool { return target == ErrLastOwner }

// WorkspaceRef names a workspace
type WorkspaceRef struct {
	ID   workspace.ID
	Name string
}

// LastOwnerErrorResponse is the 409 for deleting a user who is the last owner of a workspace. Like
// a push token that another monitor holds, it carries a code, and it lists the workspaces so that a
// client does not read their names out of a message.
type LastOwnerErrorResponse struct {
	Error LastOwnerErrorBody `json:"error"`
}

// LastOwnerErrorBody is the code of a LastOwnerErrorResponse and the workspaces the user is the
// only owner of
type LastOwnerErrorBody struct {
	Code       string               `json:"code" example:"LAST_OWNER"`
	Workspaces []LastOwnerWorkspace `json:"workspaces"`
}

// LastOwnerWorkspace is a workspace the user is the last owner of
type LastOwnerWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FailAccountRemoval turns the refusals of AccountRemover.Remove into their 409 answers: the last
// instance admin, and the last owner of workspaces, which lists them. It returns nil for any other
// error, which the caller maps as it does today. An instance admin deleting a user and a user
// deleting their own account answer the same.
func FailAccountRemoval(err error) error {
	var lastOwner *LastOwnerError
	switch {
	case errors.Is(err, ErrLastInstanceAdmin):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("The install needs at least one instance admin"))
	case errors.As(err, &lastOwner):
		workspaces := make([]LastOwnerWorkspace, len(lastOwner.Workspaces))
		for i, ref := range lastOwner.Workspaces {
			workspaces[i] = LastOwnerWorkspace{ID: string(ref.ID), Name: ref.Name}
		}
		return httpx.Fail(http.StatusConflict, LastOwnerErrorResponse{Error: LastOwnerErrorBody{Code: "LAST_OWNER", Workspaces: workspaces}})
	}
	return nil
}

// AccountRemover deletes a user's account, for an instance admin who deletes a user and for a user
// who deletes their own. Both go through the same checks (docs/adr/0005 section 10): the install
// keeps an instance admin who can sign in, and no workspace that is not scheduled for deletion is
// left with no owner.
type AccountRemover struct {
	db         *bun.DB
	transactor dbtx.Transactor
	bus        events.EventBus
	logger     *zap.SugaredLogger
}

func NewAccountRemover(db *bun.DB, transactor dbtx.Transactor, bus events.EventBus, logger *zap.SugaredLogger) *AccountRemover {
	return &AccountRemover{db: db, transactor: transactor, bus: bus, logger: logger.Named("[account-removal]")}
}

// Remove deletes the user: ErrUserNotFound, ErrLastInstanceAdmin, or a LastOwnerError while they
// are the only owner of a workspace that is not scheduled for deletion. Their memberships and
// account links go with them. Once that is stored it publishes SessionsEnded, so that their open
// sockets close, and workspace.MemberLeft for each place they had.
func (r *AccountRemover) Remove(ctx context.Context, userID string) error {
	// An ID that is no UUID names no user, and PostgreSQL refuses to compare it with one
	if _, err := uuid.Parse(userID); err != nil {
		return ErrUserNotFound
	}
	var left []workspace.ID
	err := r.transactor.WithTx(ctx, func(ctx context.Context) error {
		if err := r.check(ctx, userID); err != nil {
			return err
		}
		var err error
		if left, err = r.workspacesOf(ctx, userID); err != nil {
			return err
		}
		_, err = dbtx.DB(ctx, r.db).NewDelete().Table("users").Where("id = ?", userID).Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	r.announceSessionsEnded(ctx, userID)
	r.announceLeft(ctx, userID, left)
	return nil
}

// check refuses the removal of the last active instance admin, and of the last owner of a workspace
// that is not scheduled for deletion. The active instance admins are locked first, so that two
// changes that race for the last one take the same lock.
func (r *AccountRemover) check(ctx context.Context, userID string) error {
	admins, err := LockActiveInstanceAdmins(ctx, r.db)
	if err != nil {
		return err
	}
	var user struct {
		InstanceAdmin bool `bun:"instance_admin"`
		Active        bool `bun:"active"`
	}
	err = dbtx.DB(ctx, r.db).NewSelect().Table("users").Column("instance_admin", "active").Where("id = ?", userID).Scan(ctx, &user)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	// A disabled instance admin is not counted: they can't sign in
	if user.InstanceAdmin && user.Active && len(admins) <= 1 {
		return ErrLastInstanceAdmin
	}
	lastOwned, err := r.WorkspacesOwnedAloneAnyWorkspace(ctx, userID)
	if err != nil {
		return err
	}
	if len(lastOwned) > 0 {
		return &LastOwnerError{Workspaces: lastOwned}
	}
	return nil
}

// LockActiveInstanceAdmins returns the IDs of the instance admins who can sign in: those with the
// flag whose account is active. On PostgreSQL it holds their rows until the transaction ends, so
// that two admins can't each lose the flag, or be disabled or deleted, by a change that saw the
// other still in place.
func LockActiveInstanceAdmins(ctx context.Context, db *bun.DB) ([]string, error) {
	q := dbtx.DB(ctx, db).NewSelect().Table("users").Column("id").Where("instance_admin = ?", true).Where("active = ?", true).Order("id")
	if db.Dialect().Name() == dialect.PG {
		q = q.For("UPDATE")
	}
	var ids []string
	if err := q.Scan(ctx, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// WorkspacesOwnedAloneAnyWorkspace returns the workspaces the user is the only owner of, except the
// ones scheduled for deletion: those are deleted whether or not they have an owner. On PostgreSQL it
// first holds the owners of the user's workspaces until the transaction ends, so that two deletions
// of the two owners of a workspace can't both pass.
func (r *AccountRemover) WorkspacesOwnedAloneAnyWorkspace(ctx context.Context, userID string) ([]WorkspaceRef, error) {
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
		Where("w.deletion_scheduled_at IS NULL").
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

// workspacesOf are the IDs of the workspaces the user is a member of
func (r *AccountRemover) workspacesOf(ctx context.Context, userID string) ([]workspace.ID, error) {
	var ids []workspace.ID
	err := dbtx.DB(ctx, r.db).NewSelect().Table("workspace_members").Column("workspace_id").
		Where("user_id = ?", userID).Order("joined_at", "workspace_id").Scan(ctx, &ids)
	return ids, err
}

// announceSessionsEnded tells the rest of the install that the user's sessions ended, once that is
// stored, so that their open sockets close. A bus that fails is logged and not returned: the
// deletion stands.
func (r *AccountRemover) announceSessionsEnded(ctx context.Context, userID string) {
	if err := events.Publish(ctx, r.bus, SessionsEnded, SessionsEndedEvent{UserID: userID}); err != nil {
		r.logger.Errorw("Failed to publish that the sessions of a user ended", "userId", userID, "error", err)
	}
}

// announceLeft tells the rest of the install that the user's places in the workspaces are gone,
// once that is stored. A bus that fails is logged and not returned: the deletion stands.
func (r *AccountRemover) announceLeft(ctx context.Context, userID string, left []workspace.ID) {
	for _, id := range left {
		if err := events.Publish(ctx, r.bus, workspace.MemberLeft, workspace.MemberLeftEvent{WorkspaceID: id, UserID: userID}); err != nil {
			r.logger.Errorw("Failed to publish that a member left a workspace", "workspaceId", id, "userId", userID, "error", err)
		}
	}
}
