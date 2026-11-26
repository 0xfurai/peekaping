package apitest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-154: someone without an account signs up through an invite link

const registerPath = "/api/v1/auth/register"

type registered struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func (a *app) signUpThrough(t *testing.T, token, email string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, registerPath, map[string]any{"email": email, "password": adminPassword, "inviteToken": token})
}

func (a *app) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		return db.NewSelect().Table(table).ColumnExpr("count(*)").Scan(context.Background(), &n)
	}))
	return n
}

func TestInviteSignup_JoinsTheInvitingWorkspaceWithTheRoleAndHasNoWorkspaceOfTheirOwnNorInstanceAdmin(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		// the install has a user already: in invite mode the first-user rule would refuse anyone else
		if !cloudRun() {
			require.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, registerPath, map[string]any{"email": "x@example.com", "password": adminPassword}).Code)
		}
		created := a.invite(t, "Bob@Example.com", "admin", a.asAdmin)
		workspacesBefore := a.count(t, "workspaces")

		w := a.signUpThrough(t, created.Token, "bob@example.com")

		session := decode[registered](t, w, http.StatusCreated)
		assert.Equal(t, "bob@example.com", session.User.Email)
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(session.AccessToken)), http.StatusOK)
		assert.False(t, me.User.InstanceAdmin, "no instance admin flag")
		require.Len(t, me.Workspaces, 1, "no workspace of their own")
		assert.Equal(t, string(a.firstWorkspace(t)), me.Workspaces[0].ID)
		assert.Equal(t, "admin", me.Workspaces[0].Role, "the invite's role")
		assert.Equal(t, workspacesBefore, a.count(t, "workspaces"))
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(session.AccessToken)).Code)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": "bob@example.com", "password": adminPassword}).Code, "they can sign in")
		assert.Equal(t, "accepted", a.preview(t, created.Token).Status)
		assert.Empty(t, decode[[]inviteResponse](t, a.request(t, http.MethodGet, invitesPath, nil, a.asAdmin), http.StatusOK))
		assert.Equal(t, http.StatusGone, a.signUpThrough(t, created.Token, "bob@example.com").Code, "once")
	})
}

func TestInviteSignup_TheEmailMayBeTypedInAnyCase(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "owner", a.asAdmin)

		session := decode[registered](t, a.signUpThrough(t, created.Token, "Bob@Example.COM"), http.StatusCreated)

		assert.Equal(t, "Bob@Example.COM", session.User.Email)
		me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(session.AccessToken)), http.StatusOK)
		require.Len(t, me.Workspaces, 1)
		assert.Equal(t, "owner", me.Workspaces[0].Role)
	})
}

func TestInviteSignup_AnotherEmailIsRefusedAndCreatesNobody(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		users, members := a.count(t, "users"), a.count(t, "workspace_members")

		w := a.signUpThrough(t, created.Token, "mallory@example.com")

		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Equal(t, users, a.count(t, "users"))
		assert.Equal(t, members, a.count(t, "workspace_members"))
		assert.True(t, a.preview(t, created.Token).Valid, "and the invite is not spent")
	})
}

func TestInviteSignup_AUsedExpiredRevokedOrUnknownInviteIsRefusedAndCreatesNobody(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		used := a.invite(t, "used@example.com", "member", a.asAdmin)
		require.Equal(t, http.StatusCreated, a.signUpThrough(t, used.Token, "used@example.com").Code)
		expired := a.invite(t, "expired@example.com", "member", a.asAdmin)
		a.expire(t, expired.ID)
		revoked := a.invite(t, "revoked@example.com", "member", a.asAdmin)
		require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, invitesPath+"/"+revoked.ID, nil, a.asAdmin).Code)
		users := a.count(t, "users")

		for _, tt := range []struct {
			name, token, email string
			status             int
		}{
			{"used", used.Token, "used@example.com", http.StatusGone},
			{"used, for a new address", used.Token, "other@example.com", http.StatusGone},
			{"expired", expired.Token, "expired@example.com", http.StatusGone},
			{"revoked", revoked.Token, "revoked@example.com", http.StatusGone},
			{"unknown", "pki_" + "A0123456789012345678901234567890123456789ab", "nobody@example.com", http.StatusNotFound},
		} {
			t.Run(tt.name, func(t *testing.T) {
				w := a.signUpThrough(t, tt.token, tt.email)
				assert.Equal(t, tt.status, w.Code, w.Body.String())
			})
		}
		assert.Equal(t, users, a.count(t, "users"), "none of them made a user")
	})
}

func TestInviteSignup_AnEmailThatHasAnAccountSignsInInstead(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "bob@example.com")
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)

		w := a.signUpThrough(t, created.Token, "bob@example.com")

		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "sign in and accept the invite")
		assert.True(t, a.preview(t, created.Token).Valid)
	})
}

// The invite lower-cases its email and accounts keep the case their owner typed, so an account
// the invite names can be stored in another case. It is the same person: no second account.
func TestInviteSignup_AnAccountWhoseEmailDiffersOnlyInCaseSignsInInsteadAndNoSecondAccountIsMade(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		a.createUser(t, "Alice@Example.com")
		created := a.invite(t, "alice@example.com", "member", a.asAdmin)
		users, members := a.count(t, "users"), a.count(t, "workspace_members")

		for _, typed := range []string{"alice@example.com", "ALICE@example.com", "Alice@Example.com"} {
			w := a.signUpThrough(t, created.Token, typed)

			assert.Equal(t, http.StatusConflict, w.Code, "%s: %s", typed, w.Body.String())
			assert.Contains(t, w.Body.String(), "An account with this email exists: sign in and accept the invite", typed)
		}
		assert.Equal(t, users, a.count(t, "users"))
		assert.Equal(t, members, a.count(t, "workspace_members"))
		assert.True(t, a.preview(t, created.Token).Valid, "and the invite is not spent")
	})
}

func TestInviteSignup_ABadPasswordCreatesNobodyAndKeepsTheInvite(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		users := a.count(t, "users")

		w := a.request(t, http.MethodPost, registerPath, map[string]any{"email": "bob@example.com", "password": "weak", "inviteToken": created.Token})

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Equal(t, users, a.count(t, "users"))
		assert.True(t, a.preview(t, created.Token).Valid)
	})
}

func TestInviteSignup_WithoutATokenRegisterIsAsItWas(t *testing.T) {
	parallel(t)
	forEachDatabaseWith(t, []func(*config.Config){selfHosted}, func(t *testing.T, a *app) {
		w := a.request(t, http.MethodPost, registerPath, map[string]any{"email": "bob@example.com", "password": adminPassword})

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "admin already exists")
	})
}

// A fresh install: the first user still gets the first workspace and the flag
func TestInviteSignup_TheFirstUserStillGetsTheWorkspaceAndTheFlag(t *testing.T) {
	a := newApp(t)

	session := decode[registered](t, a.request(t, http.MethodPost, registerPath, map[string]any{"email": adminEmail, "password": adminPassword}), http.StatusCreated)

	me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(session.AccessToken)), http.StatusOK)
	assert.True(t, me.User.InstanceAdmin)
	require.Len(t, me.Workspaces, 1)
	assert.Equal(t, "owner", me.Workspaces[0].Role)
}

// raceTwice runs both calls at once, each after the other is ready, and waits for both to answer
func raceTwice(first, second func() int) (int, int) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	var a, b int
	wg.Add(2)
	go func() { defer wg.Done(); <-start; a = first() }()
	go func() { defer wg.Done(); <-start; b = second() }()
	close(start)
	wg.Wait()
	return a, b
}

// assertOneWinsTheOtherIsRefused checks that one answer is the success and the other one of the
// refusals, which is the used-invite 410 or the 409 of an address that has an account, or of a
// user who is a member, by then
func assertOneWinsTheOtherIsRefused(t *testing.T, round, success int, codes []int, refusals ...int) {
	t.Helper()
	wins := 0
	for _, code := range codes {
		if code == success {
			wins++
		} else {
			assert.Contains(t, refusals, code, "round %d: the other is refused as the invite was used or the person is in already: %v", round, codes)
		}
	}
	assert.Equal(t, 1, wins, "round %d: exactly one succeeds: %v", round, codes)
}

// Two people sign up with one invite at once: the invite works once, so one of them is made and
// the other is told it was used, or that the address has an account by then. Nothing answers 5xx,
// and one user and one membership are added.
func TestInviteSignup_TwoSignupsWithOneTokenAtOnceMakeOneUserAndOneMembership(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		for round := range 5 {
			email := fmt.Sprintf("racer%d@example.com", round)
			created := a.invite(t, email, "member", a.asAdmin)
			users, members := a.count(t, "users"), a.count(t, "workspace_members")

			first, second := raceTwice(
				func() int { return a.signUpThrough(t, created.Token, email).Code },
				func() int { return a.signUpThrough(t, created.Token, email).Code },
			)

			assertOneWinsTheOtherIsRefused(t, round, http.StatusCreated, []int{first, second}, http.StatusGone, http.StatusConflict)
			assert.Equal(t, users+1, a.count(t, "users"), "round %d: %v", round, []int{first, second})
			assert.Equal(t, members+1, a.count(t, "workspace_members"), "round %d: %v", round, []int{first, second})
			assert.Equal(t, "accepted", a.preview(t, created.Token).Status)
		}
	})
}

// Two sessions of one account accept one invite at once: one joins and the other is told the
// invite was used or that they are a member by then. Nothing answers 5xx, and one membership is
// added.
func TestInvites_TwoAcceptsOfOneTokenByTwoSessionsAtOnceMakeOneMembership(t *testing.T) {
	parallel(t)
	isolationBackends(t, func(t *testing.T, a *app) {
		for round := range 5 {
			email := fmt.Sprintf("sitter%d@example.com", round)
			a.createUser(t, email)
			created := a.invite(t, email, "member", a.asAdmin)
			phone, laptop := a.loginAs(t, email), a.loginAs(t, email)
			members := a.count(t, "workspace_members")

			first, second := raceTwice(
				func() int { return a.accept(t, created.Token, phone).Code },
				func() int { return a.accept(t, created.Token, laptop).Code },
			)

			assertOneWinsTheOtherIsRefused(t, round, http.StatusOK, []int{first, second}, http.StatusGone, http.StatusConflict)
			assert.Equal(t, members+1, a.count(t, "workspace_members"), "round %d: %v", round, []int{first, second})
			assert.Equal(t, "accepted", a.preview(t, created.Token).Status)
		}
	})
}
