package middleware_test

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

// The header is checked for its shape before the credential decides who is asking, so an API key
// that sends a header that isn't a UUID gets the same 404 as a user does.
func TestWorkspaceAuth_AHeaderThatIsNotAUUIDIsRefusedForAnAPIKeyToo(t *testing.T) {
	for name, header := range map[string]string{
		"not a UUID":          "wsA",
		"braces":              "{" + wsA + "}",
		"without the hyphens": "aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa",
		"urn":                 "urn:uuid:" + wsA,
	} {
		t.Run(name, func(t *testing.T) {
			w, _, _, ran := resolve(t, signedIn{}, keyOf(wsA), membershipsStub{}, map[string]string{"X-API-Key": "pk_k", "X-Workspace-ID": header})

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.False(t, ran)
			assert.JSONEq(t, `{"message":"Workspace not found","data":null}`, w.Body.String())
		})
	}
}

// An API key doesn't ask who its holder is: the memberships are not read, and the role is admin
// whatever they say
func TestWorkspaceAuth_AnAPIKeyIgnoresTheMembershipsOfAUser(t *testing.T) {
	places := membershipsStub{places: []workspace.Membership{place(wsB, workspace.RoleOwner)}}

	_, id, role, ran := resolve(t, signedIn{}, keyOf(wsA), places, map[string]string{"X-API-Key": "pk_k"})

	assert.True(t, ran)
	assert.Equal(t, workspace.ID(wsA), id)
	assert.Equal(t, workspace.RoleAdmin, role)
}

// A key whose workspace is stored in capitals still matches the header in lower case
func TestWorkspaceAuth_TheHeaderMatchesAWorkspaceInAnyCase(t *testing.T) {
	const upperA = "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"
	t.Run("a key", func(t *testing.T) {
		_, id, role, ran := resolve(t, signedIn{}, keyOf(upperA), membershipsStub{}, map[string]string{"X-API-Key": "pk_k", "X-Workspace-ID": wsA})
		assert.True(t, ran)
		assert.Equal(t, workspace.ID(upperA), id)
		assert.Equal(t, workspace.RoleAdmin, role)
	})
	t.Run("a member", func(t *testing.T) {
		places := membershipsStub{places: []workspace.Membership{place(wsB, workspace.RoleOwner), place(upperA, workspace.RoleMember)}}
		_, id, role, ran := resolve(t, signedIn{}, keyOf(""), places, map[string]string{"Authorization": "Bearer t", "X-Workspace-ID": wsA})
		assert.True(t, ran)
		assert.Equal(t, workspace.ID(upperA), id)
		assert.Equal(t, workspace.RoleMember, role)
	})
}
