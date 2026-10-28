package workspace_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContext_CarriesTheWorkspaceAndTheRole(t *testing.T) {
	ctx := workspace.WithContext(context.Background(), "w-1", workspace.RoleAdmin)

	id, role, ok := workspace.FromContext(ctx)

	require.True(t, ok)
	assert.Equal(t, workspace.ID("w-1"), id)
	assert.Equal(t, workspace.RoleAdmin, role)
}

func TestContext_WithoutAWorkspaceHasNone(t *testing.T) {
	_, _, ok := workspace.FromContext(context.Background())

	assert.False(t, ok)
}

func TestRequireRole(t *testing.T) {
	inWorkspace := func(role workspace.Role) context.Context {
		return workspace.WithContext(context.Background(), "w-1", role)
	}
	tests := []struct {
		name string
		ctx  context.Context
		min  workspace.Role
		want error
	}{
		{"member asks for member", inWorkspace(workspace.RoleMember), workspace.RoleMember, nil},
		{"admin asks for member", inWorkspace(workspace.RoleAdmin), workspace.RoleMember, nil},
		{"owner asks for member", inWorkspace(workspace.RoleOwner), workspace.RoleMember, nil},
		{"member asks for admin", inWorkspace(workspace.RoleMember), workspace.RoleAdmin, workspace.ErrForbidden},
		{"admin asks for admin", inWorkspace(workspace.RoleAdmin), workspace.RoleAdmin, nil},
		{"owner asks for admin", inWorkspace(workspace.RoleOwner), workspace.RoleAdmin, nil},
		{"member asks for owner", inWorkspace(workspace.RoleMember), workspace.RoleOwner, workspace.ErrForbidden},
		{"admin asks for owner", inWorkspace(workspace.RoleAdmin), workspace.RoleOwner, workspace.ErrForbidden},
		{"owner asks for owner", inWorkspace(workspace.RoleOwner), workspace.RoleOwner, nil},
		{"no workspace", context.Background(), workspace.RoleMember, workspace.ErrNotFound},
		{"an unknown role holds nothing", inWorkspace("viewer"), workspace.RoleMember, workspace.ErrForbidden},
		{"an unknown role is asked for by nobody", inWorkspace(workspace.RoleOwner), "viewer", workspace.ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, workspace.RequireRole(tt.ctx, tt.min), tt.want)
		})
	}
}
