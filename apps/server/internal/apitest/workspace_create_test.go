package apitest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-154: any signed-in user makes another workspace and owns it

type workspaceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func TestCreateWorkspace_AnySignedInUserOwnsTheWorkspaceTheyMakeWithTheDefaultSettings(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		a.createUser(t, "loner@example.com")
		loner := a.loginAs(t, "loner@example.com")

		for name, tt := range map[string]struct {
			creds credentials
			email string
		}{
			"the first user":     {a.asAdmin, adminEmail},
			"a plain member":     {tm.member, "member@example.com"},
			"a user in no place": {loner, "loner@example.com"},
		} {
			t.Run(name, func(t *testing.T) {
				w := a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "  Side project "}, tt.creds)

				made := decode[workspaceResponse](t, w, http.StatusCreated)
				assert.Equal(t, "Side project", made.Name, "trimmed")
				assert.Equal(t, "owner", made.Role)
				me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, tt.creds), http.StatusOK)
				var found bool
				for _, m := range me.Workspaces {
					if m.ID == made.ID {
						found = true
						assert.Equal(t, "owner", m.Role)
						assert.Equal(t, "Side project", m.Name)
					}
				}
				assert.True(t, found, "it appears in /me")

				inNew := inWorkspace(workspace.ID(made.ID))
				members := decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, tt.creds, inNew), http.StatusOK)
				require.Len(t, members, 1, "they are its only member")
				assert.Equal(t, tt.email, members[0].Email)
				assert.Equal(t, "owner", members[0].Role)
				// the settings of a new workspace
				retention := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, tt.creds, inNew), http.StatusOK)
				assert.Equal(t, "180", retention.Value)
				assert.Equal(t, map[string]string{"KEEP_DATA_PERIOD_DAYS": "180", "cert_expiry_notify_days": "[7,14,21]"}, a.storedSettings(t, workspace.ID(made.ID)))
				// and nothing of any other workspace's
				monitors := decode[[]map[string]any](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, tt.creds, inNew), http.StatusOK)
				assert.Empty(t, monitors)
			})
		}
		// the workspace they were already in is as it was
		assert.Len(t, decode[[]memberResponse](t, a.request(t, http.MethodGet, membersPath, nil, a.asAdmin), http.StatusOK), 4)
	})
}

func TestCreateWorkspace_TheOthersDefaultStaysTheOneTheyJoinedFirst(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		made := decode[workspaceResponse](t, a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "Second"}, a.asAdmin), http.StatusCreated)

		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin), http.StatusOK)

		require.Len(t, me.Workspaces, 2)
		assert.Equal(t, "My workspace", me.Workspaces[0].Name)
		assert.Equal(t, made.ID, me.Workspaces[1].ID)
		require.NotNil(t, me.DefaultWorkspaceID)
		assert.Equal(t, me.Workspaces[0].ID, *me.DefaultWorkspaceID)
	})
}

func TestCreateWorkspace_ABadNameOrCallerIsRefused(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		before := a.count(t, "workspaces")
		for name, body := range map[string]any{
			"no name": map[string]any{}, "empty": map[string]any{"name": ""}, "too long": map[string]any{"name": strings.Repeat("x", 256)},
			"only spaces": map[string]any{"name": "   \t "}, "only a newline": map[string]any{"name": "\n"},
		} {
			assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, workspacesURL, body, a.asAdmin).Code, name)
		}
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "X"}).Code)
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, workspacesURL, map[string]any{"name": "X"}, a.withAPIKey).Code, "a key belongs to a workspace and is nobody's account")
		assert.Equal(t, before, a.count(t, "workspaces"))
	})
}

// storedSettings are the settings a workspace holds, by key
func (a *app) storedSettings(t *testing.T, id workspace.ID) map[string]string {
	t.Helper()
	settings := map[string]string{}
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		var rows []struct {
			Key   string `bun:"key"`
			Value string `bun:"value"`
		}
		if err := db.NewSelect().Table("workspace_settings").Column("key", "value").Where("workspace_id = ?", string(id)).Scan(context.Background(), &rows); err != nil {
			return err
		}
		for _, r := range rows {
			settings[r.Key] = r.Value
		}
		return nil
	}))
	return settings
}
