// Package workspacehttp turns the workspace of a request, and the errors of its role checks, into
// HTTP answers (docs/adr/0005 section 8). It sits beside the workspace package because that one
// imports no other module.
package workspacehttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
)

// NotFound is the answer for a workspace the caller has no place in, and for an ID that names none
func NotFound() error {
	return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Workspace not found"))
}

// Scope returns the workspace the request runs in, or the 404 answer when it runs in none
func Scope(ctx context.Context) (workspace.ID, error) {
	id, _, ok := workspace.FromContext(ctx)
	if !ok {
		return "", NotFound()
	}
	return id, nil
}

// Fail turns the error of a role check, and of the people of a workspace, into its answer: 404 for
// no workspace, a member or an invite that isn't there, 403 for a role that is too low or an
// invite for another email, 409 for the last owner and for someone who is a member already, 410
// for an invite that can't be used, 400 for a role that doesn't exist or a name that is blank, and
// 403 LIMIT_REACHED for a member or a workspace past a limit, 400 WORKSPACE_NAME_MISMATCH for a
// deletion scheduled with another name, and 404 for cancelling a deletion that isn't scheduled. It returns nil for any other error,
// which the caller maps as it does today.
func Fail(err error) error {
	var reached *limits.ReachedError
	if errors.As(err, &reached) {
		return limits.Fail(reached)
	}
	// The messages of these two come from the error, or are a body of their own
	if errors.Is(err, workspace.ErrInviteUnavailable) {
		return httpx.Fail(http.StatusGone, utils.NewFailResponse(capitalize(err.Error())))
	}
	if errors.Is(err, workspace.ErrNameMismatch) {
		return httpx.Fail(http.StatusBadRequest, NameMismatchResponse{Message: capitalize(err.Error()), Error: NameMismatchErrorBody{Code: "WORKSPACE_NAME_MISMATCH"}})
	}
	for _, failure := range plainFailures {
		if errors.Is(err, failure.err) {
			return httpx.Fail(failure.status, utils.NewFailResponse(failure.message))
		}
	}
	return nil
}

// plainFailures are the errors Fail answers with a fixed message, in the order it tries them
var plainFailures = []struct {
	err     error
	status  int
	message string
}{
	{workspace.ErrNotFound, http.StatusNotFound, "Workspace not found"},
	{workspace.ErrForbidden, http.StatusForbidden, "Your role in this workspace doesn't allow this"},
	{workspace.ErrMemberNotFound, http.StatusNotFound, "Member not found"},
	{workspace.ErrInviteNotFound, http.StatusNotFound, "Invite not found"},
	{workspace.ErrInviteEmailMismatch, http.StatusForbidden, "This invite is for another email address"},
	{workspace.ErrLastOwner, http.StatusConflict, "A workspace needs at least one owner"},
	{workspace.ErrAlreadyMember, http.StatusConflict, "Already a member of this workspace"},
	{workspace.ErrInvalidName, http.StatusBadRequest, "The workspace needs a name"},
	{workspace.ErrDeletionNotScheduled, http.StatusNotFound, "No deletion is scheduled for this workspace"},
	{workspace.ErrInvalidRole, http.StatusBadRequest, "The role must be owner, admin or member"},
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
