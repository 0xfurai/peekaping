package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-154: invites to a workspace (docs/adr/0005 section 11)

const (
	invitesPath   = "/api/v1/workspace/invites"
	workspacesURL = "/api/v1/workspaces"
)

type inviteResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Role      string  `json:"role"`
	Status    string  `json:"status"`
	InvitedBy *string `json:"invitedBy"`
	CreatedAt string  `json:"createdAt"`
	ExpiresAt string  `json:"expiresAt"`
}

type createdInviteResponse struct {
	inviteResponse
	Token string `json:"token"`
	Link  string `json:"link"`
}

type invitePreviewResponse struct {
	WorkspaceName string `json:"workspaceName"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	Valid         bool   `json:"valid"`
	Status        string `json:"status"`
}

func invitePath(token string) string { return "/api/v1/invites/" + token }

// invite makes an invite as the credentials and returns it
func (a *app) invite(t *testing.T, email, role string, creds credentials) createdInviteResponse {
	t.Helper()
	w := a.request(t, http.MethodPost, invitesPath, map[string]any{"email": email, "role": role}, creds)
	return decode[createdInviteResponse](t, w, http.StatusCreated)
}

func (a *app) preview(t *testing.T, token string) invitePreviewResponse {
	t.Helper()
	return decode[invitePreviewResponse](t, a.request(t, http.MethodGet, invitePath(token), nil), http.StatusOK)
}

func (a *app) accept(t *testing.T, token string, creds credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, invitePath(token)+"/accept", nil, creds)
}

// expire moves the invite's expiry into the past
func (a *app) expire(t *testing.T, inviteID string) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("UPDATE workspace_invites SET expires_at = ? WHERE id = ?", time.Now().UTC().Add(-time.Minute), inviteID)
		return err
	}))
}

func TestInvites_AnOwnerOrAdminInvitesByEmailWithARoleAndGetsASingleUseLinkOnce(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)

		for name, creds := range map[string]credentials{"an owner": a.asAdmin, "an admin": tm.admin, "an API key": a.withAPIKey} {
			t.Run(name, func(t *testing.T) {
				slug := strings.ReplaceAll(name, " ", "-")
				created := a.invite(t, slug+"@Invited.example ", "member", creds)

				assert.Equal(t, strings.ToLower(slug)+"@invited.example", created.Email, "lower-cased and trimmed")
				assert.Equal(t, "member", created.Role)
				assert.Equal(t, "pending", created.Status)
				assert.True(t, strings.HasPrefix(created.Token, "pki_"), created.Token)
				assert.GreaterOrEqual(t, len(created.Token), 4+43, "32 random bytes")
				assert.Equal(t, clientURL+"/invite/"+created.Token, created.Link)
				expires, err := time.Parse(time.RFC3339, created.ExpiresAt)
				require.NoError(t, err)
				createdAt, err := time.Parse(time.RFC3339, created.CreatedAt)
				require.NoError(t, err)
				assert.InDelta(t, 7*24*time.Hour, expires.Sub(createdAt), float64(time.Second), "7 days")
				assert.NotEmpty(t, created.ID)
			})
		}
	})
}

func TestInvites_TheTokenIsStoredOnlyAsAHash(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)

		var rows []map[string]any
		require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
			r, err := db.DB.QueryContext(context.Background(), "SELECT * FROM workspace_invites")
			if err != nil {
				return err
			}
			defer r.Close()
			columns, err := r.Columns()
			if err != nil {
				return err
			}
			for r.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := r.Scan(pointers...); err != nil {
					return err
				}
				row := map[string]any{}
				for i, c := range columns {
					row[c] = values[i]
				}
				rows = append(rows, row)
			}
			return r.Err()
		}))
		require.Len(t, rows, 1)
		stored, err := json.Marshal(rows[0])
		require.NoError(t, err)
		assert.NotContains(t, string(stored), created.Token, "no column holds the token")
		assert.NotContains(t, string(stored), strings.TrimPrefix(created.Token, "pki_"))
		assert.NotEmpty(t, rows[0]["token_hash"])

		// and the list never shows it again
		list := a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin)
		require.Equal(t, http.StatusOK, list.Code)
		assert.NotContains(t, list.Body.String(), created.Token)
		assert.NotContains(t, list.Body.String(), "token")
	})
}

func TestInvites_OnlyAnOwnerInvitesAnOwnerAndAMemberInvitesNobody(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		invite := func(role string, creds credentials) int {
			return a.request(t, http.MethodPost, invitesPath, map[string]any{"email": role + "-invitee@example.com", "role": role}, creds).Code
		}

		assert.Equal(t, http.StatusCreated, invite("owner", a.asAdmin))
		assert.Equal(t, http.StatusCreated, invite("admin", tm.admin))
		assert.Equal(t, http.StatusForbidden, invite("owner", tm.admin), "an admin invites no owner")
		assert.Equal(t, http.StatusForbidden, invite("owner", a.withAPIKey), "nor does a key, which acts as an admin")
		assert.Equal(t, http.StatusForbidden, invite("member", tm.member))
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodGet, invitesPath, nil, tm.member).Code)
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodDelete, invitesPath+"/00000000-0000-0000-0000-000000000000", nil, tm.member).Code)
	})
}

func TestInvites_ABadEmailOrRoleOrAMemberAlreadyInIsRefused(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		newTeam(t, a)
		post := func(body map[string]any) *httpRecorder {
			return a.request(t, http.MethodPost, invitesPath, body, a.asAdmin)
		}

		assert.Equal(t, http.StatusBadRequest, post(map[string]any{"email": "not-an-email", "role": "member"}).Code)
		assert.Equal(t, http.StatusBadRequest, post(map[string]any{"role": "member"}).Code)
		assert.Equal(t, http.StatusBadRequest, post(map[string]any{"email": "x@example.com", "role": "viewer"}).Code)
		assert.Equal(t, http.StatusBadRequest, post(map[string]any{"email": "x@example.com"}).Code)
		assert.Equal(t, http.StatusConflict, post(map[string]any{"email": "MEMBER@example.com", "role": "admin"}).Code, "already a member, whatever the case")
		assert.Empty(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK))
	})
}

func TestInvites_TheListHoldsThePendingOnesAndAMoreRecentInviteForTheSameEmailReplacesTheOlder(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		first := a.invite(t, "bob@example.com", "member", a.asAdmin)
		other := a.invite(t, "carol@example.com", "admin", a.asAdmin)
		second := a.invite(t, "bob@example.com", "admin", a.asAdmin)

		list := decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK)

		require.Len(t, list, 2)
		ids := []string{list[0].ID, list[1].ID}
		assert.ElementsMatch(t, []string{other.ID, second.ID}, ids)
		assert.NotContains(t, ids, first.ID)
		assert.Equal(t, "revoked", a.preview(t, first.Token).Status, "the older link stopped working")
		assert.Equal(t, "pending", a.preview(t, second.Token).Status)
		adminID := a.userID(t, adminEmail)
		for _, invite := range list {
			require.NotNil(t, invite.InvitedBy)
			assert.Equal(t, adminID, *invite.InvitedBy)
		}
	})
}

func TestInvites_AnInviteByAnAPIKeyHasNoInviter(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "member", a.withAPIKey)

		assert.Nil(t, created.InvitedBy)
	})
}

func TestInvites_RevokingStopsTheLinkAndItLeavesTheList(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		tm := newTeam(t, a)
		created := a.invite(t, "bob@example.com", "member", tm.admin)
		bob := a.createUser(t, "bob@example.com")
		require.NotEmpty(t, bob)

		w := a.request(t, http.MethodDelete, invitesPath+"/"+created.ID, nil, tm.admin)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		assert.Empty(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK))
		assert.Equal(t, http.StatusGone, a.accept(t, created.Token, a.loginAs(t, "bob@example.com")).Code)
		preview := a.preview(t, created.Token)
		assert.False(t, preview.Valid)
		assert.Equal(t, "revoked", preview.Status)
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, invitesPath+"/"+created.ID, nil, a.asAdmin).Code, "once")
		for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodDelete, invitesPath+"/"+id, nil, a.asAdmin).Code, id)
		}
	})
}

func TestInvites_APreviewShowsOnlyTheWorkspaceNameTheEmailTheRoleAndWhetherItIsValid(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "Bob@Example.com", "admin", a.asAdmin)

		w := a.request(t, http.MethodGet, invitePath(created.Token), nil)

		preview := decode[invitePreviewResponse](t, w, http.StatusOK)
		assert.Equal(t, invitePreviewResponse{WorkspaceName: "My workspace", Email: "bob@example.com", Role: "admin", Valid: true, Status: "pending"}, preview)
		var raw struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		assert.ElementsMatch(t, []string{"workspaceName", "email", "role", "valid", "status"}, keysOfAny(raw.Data), "nothing else")
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

		for _, token := range []string{"pki_" + strings.Repeat("A", 43), "nope", "pk_" + created.Token, strings.Repeat("x", 500)} {
			assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, invitePath(token), nil).Code, token)
		}
	})
}

func keysOfAny(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestInvites_ASignedInUserWhoseEmailMatchesJoinsWithTheRoleOfTheInvite(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		other := a.createWorkspace(t, "Bobs own")
		a.addMember(t, other, a.createUser(t, "Bob@Example.com"), workspace.RoleOwner)
		bob := a.loginAs(t, "Bob@Example.com")
		created := a.invite(t, "bob@example.com", "admin", a.asAdmin)

		w := a.accept(t, created.Token, bob)

		joined := decode[struct {
			ID, Name, Role string
		}](t, w, http.StatusOK)
		assert.Equal(t, string(a.firstWorkspace(t)), joined.ID)
		assert.Equal(t, "My workspace", joined.Name)
		assert.Equal(t, "admin", joined.Role)
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, bob), http.StatusOK)
		require.Len(t, me.Workspaces, 2)
		assert.Equal(t, "Bobs own", me.Workspaces[0].Name, "the default workspace is the one they joined first")
		assert.Equal(t, "admin", me.Workspaces[1].Role)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, membersPath, nil, bob, inWorkspace(a.firstWorkspace(t))).Code)
		assert.Empty(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK), "a used invite leaves the list")
		assert.Equal(t, "accepted", a.preview(t, created.Token).Status)
	})
}

func TestInvites_AnInviteWorksOnce(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "bob@example.com")
		bob := a.loginAs(t, "bob@example.com")
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		require.Equal(t, http.StatusOK, a.accept(t, created.Token, bob).Code)

		// used again by the same user, and by one who left and tries again
		assert.Equal(t, http.StatusGone, a.accept(t, created.Token, bob).Code)
		require.Equal(t, http.StatusNoContent, a.request(t, http.MethodPost, leavePath, nil, bob).Code)
		again := a.accept(t, created.Token, bob)
		assert.Equal(t, http.StatusGone, again.Code, again.Body.String())
		assert.Contains(t, again.Body.String(), "already used")
		assert.False(t, a.preview(t, created.Token).Valid)
	})
}

func TestInvites_AnAccountWithAnotherEmailIsRefusedAndTheInviteStaysUsable(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "mallory@example.com")
		mallory := a.loginAs(t, "mallory@example.com")
		a.createUser(t, "bob@example.com")
		created := a.invite(t, "bob@example.com", "owner", a.asAdmin)

		w := a.accept(t, created.Token, mallory)

		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, mallory), http.StatusOK)
		assert.Empty(t, me.Workspaces)
		assert.True(t, a.preview(t, created.Token).Valid)
		assert.Equal(t, http.StatusOK, a.accept(t, created.Token, a.loginAs(t, "bob@example.com")).Code, "the right account still joins")
	})
}

func TestInvites_AnExpiredInviteIsRefused(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "bob@example.com")
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		a.expire(t, created.ID)

		w := a.accept(t, created.Token, a.loginAs(t, "bob@example.com"))

		assert.Equal(t, http.StatusGone, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "expired")
		preview := a.preview(t, created.Token)
		assert.False(t, preview.Valid)
		assert.Equal(t, "expired", preview.Status)
		list := decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK)
		require.Len(t, list, 1)
		assert.Equal(t, "expired", list[0].Status, "the inviter sees it expired, and can revoke it")
		assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, invitesPath+"/"+created.ID, nil, a.asAdmin).Code)
	})
}

func TestInvites_AUnknownTokenAnAnonymousCallerAndAMemberAlreadyInAreRefused(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "late@example.com")
		late := a.loginAs(t, "late@example.com")
		created := a.invite(t, "late@example.com", "owner", a.asAdmin)
		a.addMember(t, a.firstWorkspace(t), a.userID(t, "late@example.com"), workspace.RoleMember)
		tm := &team{a: a}

		assert.Equal(t, http.StatusNotFound, a.accept(t, "pki_"+strings.Repeat("A", 43), late).Code)
		assert.Equal(t, http.StatusUnauthorized, a.accept(t, created.Token, func(*http.Request) {}).Code)
		assert.Equal(t, http.StatusUnauthorized, a.accept(t, created.Token, a.withAPIKey).Code, "an API key is nobody's account")
		w := a.accept(t, created.Token, late)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, "member", tm.roleOf(t, a.userID(t, "late@example.com")), "the role is not changed by an invite")
		assert.True(t, a.preview(t, created.Token).Valid, "and the invite is not spent")
	})
}
