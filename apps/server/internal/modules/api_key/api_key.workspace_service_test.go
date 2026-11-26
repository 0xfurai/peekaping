package api_key

import (
	"context"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const otherWorkspace = workspace.ID("5b7c1d44-0a43-4e2c-9a8e-6a1c3d2f9b02")

func inRole(id workspace.ID, role workspace.Role) context.Context {
	return workspace.WithContext(context.Background(), id, role)
}

func newTestService(t *testing.T) Service {
	t.Helper()
	return NewService(NewSQLRepository(setupServiceTestDB(t)), zap.NewNop().Sugar())
}

func TestService_EveryMethodNeedsAnAdminOrOwner(t *testing.T) {
	service := newTestService(t)
	created, err := service.Create(adminContext(), &CreateRequest{Name: "Key"})
	require.NoError(t, err)
	rename := "Renamed"
	member := inRole(testWorkspace, workspace.RoleMember)

	_, err = service.Create(member, &CreateRequest{Name: "Nope"})
	assert.ErrorIs(t, err, workspace.ErrForbidden)
	_, err = service.FindAll(member, testWorkspace)
	assert.ErrorIs(t, err, workspace.ErrForbidden)
	_, err = service.FindByID(member, testWorkspace, created.ID)
	assert.ErrorIs(t, err, workspace.ErrForbidden)
	_, err = service.Update(member, testWorkspace, created.ID, &UpdateRequest{Name: &rename})
	assert.ErrorIs(t, err, workspace.ErrForbidden)
	assert.ErrorIs(t, service.Delete(member, testWorkspace, created.ID), workspace.ErrForbidden)

	for _, role := range []workspace.Role{workspace.RoleAdmin, workspace.RoleOwner} {
		_, err = service.FindAll(inRole(testWorkspace, role), testWorkspace)
		assert.NoError(t, err, role)
	}
	stored, err := service.FindByID(adminContext(), testWorkspace, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Key", stored.Name, "the member changed nothing")
}

func TestService_AContextOutsideTheWorkspaceReachesNothing(t *testing.T) {
	service := newTestService(t)

	_, err := service.Create(context.Background(), &CreateRequest{Name: "Nope"})
	assert.ErrorIs(t, err, workspace.ErrNotFound, "no workspace in the context")
	_, err = service.FindAll(context.Background(), testWorkspace)
	assert.ErrorIs(t, err, workspace.ErrNotFound)
	_, err = service.FindAll(adminContext(), otherWorkspace)
	assert.ErrorIs(t, err, workspace.ErrNotFound, "a workspace other than the context's")
}

func TestService_AKeyOfAnotherWorkspaceIsNotFound(t *testing.T) {
	service := newTestService(t)
	mine, err := service.Create(adminContext(), &CreateRequest{Name: "Mine"})
	require.NoError(t, err)
	theirs, err := service.Create(inRole(otherWorkspace, workspace.RoleAdmin), &CreateRequest{Name: "Theirs"})
	require.NoError(t, err)
	rename := "Taken"

	found, err := service.FindByID(adminContext(), testWorkspace, theirs.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
	_, err = service.Update(adminContext(), testWorkspace, theirs.ID, &UpdateRequest{Name: &rename})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, service.Delete(adminContext(), testWorkspace, theirs.ID), ErrNotFound)
	assert.ErrorIs(t, service.Delete(adminContext(), testWorkspace, "no-such-key"), ErrNotFound)

	all, err := service.FindAll(adminContext(), testWorkspace)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, mine.ID, all[0].ID)
	still, err := service.FindByID(inRole(otherWorkspace, workspace.RoleOwner), otherWorkspace, theirs.ID)
	require.NoError(t, err)
	require.NotNil(t, still)
	assert.Equal(t, "Theirs", still.Name, "the other workspace's key is untouched")
}

func TestService_ValidateKeyReturnsTheKeyWithItsWorkspace(t *testing.T) {
	service := newTestService(t)
	created, err := service.Create(inRole(otherWorkspace, workspace.RoleOwner), &CreateRequest{Name: "Theirs"})
	require.NoError(t, err)

	key, err := service.ValidateKey(context.Background(), created.Token)

	require.NoError(t, err)
	assert.Equal(t, otherWorkspace, key.WorkspaceID)
}
