package workspacehttp_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspacehttp"
	"peekaping/internal/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// answer is the status and the message of the answer Fail gave
func answer(t *testing.T, err error) (int, string) {
	t.Helper()
	var response *httpx.ResponseError
	require.ErrorAs(t, err, &response)
	body, ok := response.Body.(utils.ApiResponse[any])
	require.True(t, ok, "a failure body")
	assert.Nil(t, body.Data)
	return response.Status, body.Message
}

func TestFail_AnswersEachErrorOfTheWorkspaceAndItsPeople(t *testing.T) {
	tests := []struct {
		err     error
		status  int
		message string
	}{
		{workspace.ErrNotFound, http.StatusNotFound, "Workspace not found"},
		{workspace.ErrForbidden, http.StatusForbidden, "Your role in this workspace doesn't allow this"},
		{workspace.ErrMemberNotFound, http.StatusNotFound, "Member not found"},
		{workspace.ErrInviteNotFound, http.StatusNotFound, "Invite not found"},
		{workspace.ErrInviteUnavailable, http.StatusGone, "This invite can't be used"},
		{workspace.ErrInviteEmailMismatch, http.StatusForbidden, "This invite is for another email address"},
		{workspace.ErrLastOwner, http.StatusConflict, "A workspace needs at least one owner"},
		{workspace.ErrAlreadyMember, http.StatusConflict, "Already a member of this workspace"},
		{workspace.ErrInvalidName, http.StatusBadRequest, "The workspace needs a name"},
		{workspace.ErrInvalidRole, http.StatusBadRequest, "The role must be owner, admin or member"},
	}
	for _, tc := range tests {
		t.Run(tc.err.Error(), func(t *testing.T) {
			status, message := answer(t, workspacehttp.Fail(tc.err))
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.message, message)

			status, message = answer(t, workspacehttp.Fail(fmt.Errorf("while accepting: %w", tc.err)))
			assert.Equal(t, tc.status, status, "a wrapped error is answered the same")
			if errors.Is(tc.err, workspace.ErrInviteUnavailable) {
				return // its message is the text of the whole error (see the next test)
			}
			assert.Equal(t, tc.message, message)
		})
	}
}

func TestFail_AnUnusableInviteSaysWhatTheErrorSays(t *testing.T) {
	err := fmt.Errorf("the invite is spent: %w", workspace.ErrInviteUnavailable)

	status, message := answer(t, workspacehttp.Fail(err))

	assert.Equal(t, http.StatusGone, status)
	assert.Equal(t, "The invite is spent: this invite can't be used", message, "the text of the whole error, with a capital")
}

func TestFail_AnswersALimitWithItsCode(t *testing.T) {
	reached := &limits.ReachedError{Code: "LIMIT_REACHED", Limit: limits.Members, Max: 3}

	for _, err := range []error{reached, fmt.Errorf("inviting: %w", reached)} {
		answered := workspacehttp.Fail(err)

		var response *httpx.ResponseError
		require.ErrorAs(t, answered, &response)
		assert.Equal(t, http.StatusForbidden, response.Status)
		body, ok := response.Body.(limits.ReachedResponse)
		require.True(t, ok)
		assert.Equal(t, reached, body.Error)
		assert.Equal(t, reached.Error(), body.Message)
	}
}

func TestFail_AnswersTheFirstOfSeveralErrors(t *testing.T) {
	reached := &limits.ReachedError{Code: "LIMIT_REACHED", Limit: limits.Members, Max: 3}
	tests := []struct {
		name   string
		err    error
		status int
		first  string
	}{
		{"a limit beats a missing workspace", errors.Join(workspace.ErrNotFound, reached), http.StatusForbidden, ""},
		{"a missing workspace beats a role", errors.Join(workspace.ErrForbidden, workspace.ErrNotFound), http.StatusNotFound, "Workspace not found"},
		{"a role beats a missing member", errors.Join(workspace.ErrMemberNotFound, workspace.ErrForbidden), http.StatusForbidden, "Your role in this workspace doesn't allow this"},
		{"a missing member beats a missing invite", errors.Join(workspace.ErrInviteNotFound, workspace.ErrMemberNotFound), http.StatusNotFound, "Member not found"},
		{"a missing invite beats an unusable one", errors.Join(workspace.ErrInviteUnavailable, workspace.ErrInviteNotFound), http.StatusNotFound, "Invite not found"},
		{"an unusable invite beats another email", errors.Join(workspace.ErrInviteEmailMismatch, workspace.ErrInviteUnavailable), http.StatusGone, ""},
		{"another email beats the last owner", errors.Join(workspace.ErrLastOwner, workspace.ErrInviteEmailMismatch), http.StatusForbidden, "This invite is for another email address"},
		{"the last owner beats a member already", errors.Join(workspace.ErrAlreadyMember, workspace.ErrLastOwner), http.StatusConflict, "A workspace needs at least one owner"},
		{"a member already beats a blank name", errors.Join(workspace.ErrInvalidName, workspace.ErrAlreadyMember), http.StatusConflict, "Already a member of this workspace"},
		{"a blank name beats a role", errors.Join(workspace.ErrInvalidRole, workspace.ErrInvalidName), http.StatusBadRequest, "The workspace needs a name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var response *httpx.ResponseError
			require.ErrorAs(t, workspacehttp.Fail(tc.err), &response)
			assert.Equal(t, tc.status, response.Status)
			if tc.first != "" {
				_, message := answer(t, workspacehttp.Fail(tc.err))
				assert.Equal(t, tc.first, message)
			}
		})
	}
}

func TestFail_LeavesAnyOtherErrorToTheCaller(t *testing.T) {
	assert.NoError(t, workspacehttp.Fail(nil))
	assert.NoError(t, workspacehttp.Fail(errors.New("database is closed")))
	assert.NoError(t, workspacehttp.Fail(fmt.Errorf("wrapped: %w", errors.New("other"))))
}

func TestNotFound(t *testing.T) {
	status, message := answer(t, workspacehttp.NotFound())
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "Workspace not found", message)
}
