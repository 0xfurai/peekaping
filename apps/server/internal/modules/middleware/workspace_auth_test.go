package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// signedIn stands in for the JWT middleware: it signs every request in as user-1
type signedIn struct{}

func (signedIn) Auth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = httpx.WithValue(r, "authType", "jwt")
			r = httpx.WithValue(r, "userId", "user-1")
			next.ServeHTTP(w, r)
		})
	}
}

// keyOf stands in for the API key middleware: it authenticates a key of the workspace
type keyOf workspace.ID

func (k keyOf) Auth() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = httpx.WithValue(r, "authType", "api_key")
			if k != "" {
				r = httpx.WithValue(r, "apiKeyWorkspaceId", workspace.ID(k))
			}
			next.ServeHTTP(w, r)
		})
	}
}

type membershipsStub struct {
	places []workspace.Membership
	err    error
}

func (m membershipsStub) ListMemberships(context.Context, string) ([]workspace.Membership, error) {
	return m.places, m.err
}

type usersStub map[string]*auth.Model

func (u usersStub) FindByID(_ context.Context, id string) (*auth.Model, error) { return u[id], nil }

const (
	wsA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	wsB = "22222222-2222-4222-8222-222222222222"
)

func place(id string, role workspace.Role) workspace.Membership {
	return workspace.Membership{Workspace: workspace.Workspace{ID: workspace.ID(id)}, Role: role}
}

// resolve sends a request through WorkspaceAuth and returns the response and the workspace and
// role the handler saw, if it ran
func resolve(t *testing.T, jwt, key interface{ Auth() httpx.Middleware }, places membershipsStub, headers map[string]string) (*httptest.ResponseRecorder, workspace.ID, workspace.Role, bool) {
	t.Helper()
	chain := middleware.NewAuthChain(jwt, key, places, usersStub{}, nil, zap.NewNop().Sugar())
	var id workspace.ID
	var role workspace.Role
	ran := false
	router := httpx.NewRouter()
	router.Handle("GET", "/workspace", func(w http.ResponseWriter, r *http.Request) {
		id, role, ran = func() (workspace.ID, workspace.Role, bool) { return workspace.FromContext(r.Context()) }()
		w.WriteHeader(http.StatusOK)
	}, chain.WorkspaceAuth())
	req := httptest.NewRequest(http.MethodGet, "/workspace", nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, id, role, ran
}

func TestWorkspaceAuth_AJWTRunsInTheWorkspaceItNamesOrItsFirst(t *testing.T) {
	places := membershipsStub{places: []workspace.Membership{place(wsA, workspace.RoleMember), place(wsB, workspace.RoleOwner)}}
	jwt := map[string]string{"Authorization": "Bearer t"}
	with := func(header string) map[string]string {
		return map[string]string{"Authorization": "Bearer t", "X-Workspace-ID": header}
	}

	_, id, role, ran := resolve(t, signedIn{}, keyOf(""), places, jwt)
	assert.True(t, ran)
	assert.Equal(t, workspace.ID(wsA), id, "the first, with no header")
	assert.Equal(t, workspace.RoleMember, role)

	_, id, role, _ = resolve(t, signedIn{}, keyOf(""), places, with(wsB))
	assert.Equal(t, workspace.ID(wsB), id)
	assert.Equal(t, workspace.RoleOwner, role)

	_, id, _, _ = resolve(t, signedIn{}, keyOf(""), places, with("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"))
	assert.Equal(t, workspace.ID(wsA), id, "the case of the header doesn't matter")
}

func TestWorkspaceAuth_AnythingElseIsNotFound(t *testing.T) {
	places := membershipsStub{places: []workspace.Membership{place(wsA, workspace.RoleMember)}}
	for name, header := range map[string]string{
		"not a member":        wsB,
		"not a UUID":          "wsA",
		"braces":              "{" + wsA + "}",
		"without the hyphens": "aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa",
		"urn":                 "urn:uuid:" + wsA,
	} {
		t.Run(name, func(t *testing.T) {
			w, _, _, ran := resolve(t, signedIn{}, keyOf(""), places, map[string]string{"Authorization": "Bearer t", "X-Workspace-ID": header})
			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.False(t, ran)
			assert.JSONEq(t, `{"message":"Workspace not found","data":null}`, w.Body.String())
		})
	}
	t.Run("a user in no workspace", func(t *testing.T) {
		w, _, _, ran := resolve(t, signedIn{}, keyOf(""), membershipsStub{}, map[string]string{"Authorization": "Bearer t"})
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, ran)
	})
}

func TestWorkspaceAuth_AMembershipListThatFailsIsA500(t *testing.T) {
	w, _, _, ran := resolve(t, signedIn{}, keyOf(""), membershipsStub{err: errors.New("db down")}, map[string]string{"Authorization": "Bearer t"})

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.False(t, ran)
	assert.NotContains(t, w.Body.String(), "db down")
}

func TestWorkspaceAuth_AnAPIKeyRunsAsAnAdminInItsOwnWorkspace(t *testing.T) {
	// The key's workspace wins over the memberships of nobody, and its header may repeat it
	for name, headers := range map[string]map[string]string{
		"no header":         {"X-API-Key": "pk_k"},
		"its own workspace": {"X-API-Key": "pk_k", "X-Workspace-ID": wsA},
		"in upper case":     {"X-API-Key": "pk_k", "X-Workspace-ID": "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"},
	} {
		t.Run(name, func(t *testing.T) {
			_, id, role, ran := resolve(t, signedIn{}, keyOf(wsA), membershipsStub{}, headers)
			assert.True(t, ran)
			assert.Equal(t, workspace.ID(wsA), id)
			assert.Equal(t, workspace.RoleAdmin, role)
		})
	}
	t.Run("another workspace", func(t *testing.T) {
		w, _, _, ran := resolve(t, signedIn{}, keyOf(wsA), membershipsStub{}, map[string]string{"X-API-Key": "pk_k", "X-Workspace-ID": wsB})
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, ran)
	})
	t.Run("a key with no workspace", func(t *testing.T) {
		w, _, _, ran := resolve(t, signedIn{}, keyOf(""), membershipsStub{}, map[string]string{"X-API-Key": "pk_k"})
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, ran)
	})
}

func TestInstanceAdminAuth_ReadsTheFlagAndRefusesAnAPIKey(t *testing.T) {
	serve := func(jwt interface{ Auth() httpx.Middleware }, users usersStub) int {
		chain := middleware.NewAuthChain(jwt, keyOf(wsA), membershipsStub{}, users, nil, zap.NewNop().Sugar())
		router := httpx.NewRouter()
		router.Handle("PUT", "/install", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }, chain.InstanceAdminAuth())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/install", nil))
		return w.Code
	}

	assert.Equal(t, http.StatusOK, serve(signedIn{}, usersStub{"user-1": {InstanceAdmin: true}}))
	assert.Equal(t, http.StatusForbidden, serve(signedIn{}, usersStub{"user-1": {InstanceAdmin: false}}))
	assert.Equal(t, http.StatusForbidden, serve(signedIn{}, usersStub{}), "a user who is gone")
	assert.Equal(t, http.StatusForbidden, serve(keyOf(wsA), usersStub{"user-1": {InstanceAdmin: true}}), "an API key, whoever's flag")
}
