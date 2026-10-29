package apitest

import (
	"net/http"
	"strings"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-370: an admin or owner renames the workspace the request runs in

const workspaceURL = "/api/v1/workspace"

// workspaceNameInMe is the name /me shows for the workspace, or "" when it is not listed
func workspaceNameInMe(t *testing.T, a *app, id workspace.ID, creds ...credentials) string {
	t.Helper()
	me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, creds...), http.StatusOK)
	for _, w := range me.Workspaces {
		if w.ID == string(id) {
			return w.Name
		}
	}
	return ""
}

func TestWorkspaceRename_AnOwnerAndAnAdminRenameTheWorkspace(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		for name, tt := range map[string]struct {
			creds credentials
			role  string
		}{"an owner": {a.asAdmin, "owner"}, "an admin": {tm.admin, "admin"}} {
			t.Run(name, func(t *testing.T) {
				w := a.request(t, http.MethodPatch, workspaceURL, map[string]any{"name": "  Renamed by " + tt.role + " "}, tt.creds)

				renamed := decode[workspaceResponse](t, w, http.StatusOK)
				assert.Equal(t, string(tm.id), renamed.ID)
				assert.Equal(t, "Renamed by "+tt.role, renamed.Name, "trimmed")
				assert.Equal(t, tt.role, renamed.Role, "the caller's role there")
				assert.Equal(t, "Renamed by "+tt.role, workspaceNameInMe(t, a, tm.id, a.asAdmin))
				assert.Equal(t, "Renamed by "+tt.role, workspaceNameInMe(t, a, tm.id, tm.member), "every member sees it")
			})
		}
	})
}

func TestWorkspaceRename_AnAPIKeyRenamesLikeAnAdmin(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		id := a.firstWorkspace(t)

		renamed := decode[workspaceResponse](t, a.request(t, http.MethodPatch, workspaceURL, map[string]any{"name": "By key"}, a.withAPIKey), http.StatusOK)

		assert.Equal(t, "By key", renamed.Name)
		assert.Equal(t, "admin", renamed.Role)
		assert.Equal(t, "By key", workspaceNameInMe(t, a, id, a.asAdmin))
	})
}

func TestWorkspaceRename_AMemberIsRefusedAndNothingChanges(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		before := workspaceNameInMe(t, a, tm.id, a.asAdmin)

		w := a.request(t, http.MethodPatch, workspaceURL, map[string]any{"name": "Hijacked"}, tm.member)

		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Equal(t, before, workspaceNameInMe(t, a, tm.id, a.asAdmin))
	})
}

func TestWorkspaceRename_AWorkspaceTheCallerIsNotInIsNotFound(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		first := a.firstWorkspace(t)
		second := a.createWorkspace(t, "Second")
		bob := a.userIn(t, "bob@example.com", second, workspace.RoleOwner)

		w := a.request(t, http.MethodPatch, workspaceURL, map[string]any{"name": "Hijacked"}, bob, inWorkspace(first))

		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		assert.NotEqual(t, "Hijacked", workspaceNameInMe(t, a, first, a.asAdmin))
		// and the one Bob is in is untouched
		assert.Equal(t, "Second", workspaceNameInMe(t, a, second, bob))
	})
}

func TestWorkspaceRename_ABlankOrTooLongNameIsRefused(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		id := a.firstWorkspace(t)
		before := workspaceNameInMe(t, a, id, a.asAdmin)

		for name, body := range map[string]map[string]any{
			"empty":         {"name": ""},
			"blank":         {"name": "   \t "},
			"missing":       {},
			"256 letters":   {"name": strings.Repeat("a", 256)},
			"trimmed blank": {"name": "\n"},
		} {
			t.Run(name, func(t *testing.T) {
				w := a.request(t, http.MethodPatch, workspaceURL, body, a.asAdmin)

				assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			})
		}
		assert.Equal(t, before, workspaceNameInMe(t, a, id, a.asAdmin))
		// 255 is the longest
		w := a.request(t, http.MethodPatch, workspaceURL, map[string]any{"name": strings.Repeat("a", 255)}, a.asAdmin)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}
