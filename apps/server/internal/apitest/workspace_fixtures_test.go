package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

// Until invites exist, the only way to put a user in a second workspace is to write it through the
// repositories. These helpers do, in the app's own container, and log the user in through the API.

// createUser adds a user with the admin's password and no workspace, and returns their ID.
func (a *app) createUser(t *testing.T, email string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.MinCost)
	require.NoError(t, err)
	var id string
	require.NoError(t, a.container.Invoke(func(users auth.Repository) error {
		created, err := users.Create(context.Background(), &auth.Model{Email: email, Password: string(hash), Active: true})
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	}))
	return id
}

// setInstanceAdmin sets the flag of a user directly in the database
func (a *app) setInstanceAdmin(t *testing.T, userID string, on bool) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("UPDATE users SET instance_admin = ? WHERE id = ?", on, userID)
		return err
	}))
}

// createWorkspace adds a workspace with no member. The user who joins it first has it as their
// default one, if it is their first.
func (a *app) createWorkspace(t *testing.T, name string) workspace.ID {
	t.Helper()
	var id workspace.ID
	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository) error {
		created, err := workspaces.Create(context.Background(), name)
		if err != nil {
			return err
		}
		id = created.ID
		return nil
	}))
	return id
}

// addMember makes the user a member of the workspace. Memberships made later are joined later:
// the helper waits a moment so that two joins never share a timestamp.
func (a *app) addMember(t *testing.T, id workspace.ID, userID string, role workspace.Role) {
	t.Helper()
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository) error {
		return workspaces.AddMember(context.Background(), id, userID, role)
	}))
}

// userID is the ID of the user with this email
func (a *app) userID(t *testing.T, email string) string {
	t.Helper()
	var id string
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table("users").Column("id").Where("email = ?", email).Scan(context.Background(), &id)
	}))
	return id
}

// firstWorkspace is the install's oldest workspace, the one the admin registered into
func (a *app) firstWorkspace(t *testing.T) workspace.ID {
	t.Helper()
	var id workspace.ID
	require.NoError(t, a.container.Invoke(func(workspaces workspace.Repository) error {
		oldest, err := workspaces.FindOldest(context.Background())
		if err != nil {
			return err
		}
		id = oldest.ID
		return nil
	}))
	return id
}

// loginAs logs the user in through the API and returns the credentials of their session
func (a *app) loginAs(t *testing.T, email string) credentials {
	t.Helper()
	session := decode[struct {
		AccessToken string `json:"accessToken"`
	}](t, a.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": adminPassword}), http.StatusOK)
	return func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+session.AccessToken) }
}

// userIn creates a user who is in the workspace with the role, and returns their credentials
func (a *app) userIn(t *testing.T, email string, id workspace.ID, role workspace.Role) credentials {
	t.Helper()
	a.addMember(t, id, a.createUser(t, email), role)
	return a.loginAs(t, email)
}

// inWorkspace names the workspace a request runs in
func inWorkspace(id workspace.ID) credentials {
	return withHeader("X-Workspace-ID", string(id))
}
