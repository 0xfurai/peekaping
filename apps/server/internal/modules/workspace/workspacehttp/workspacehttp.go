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
// deletion scheduled with another name, and 404 for cancelling a deletion that isn't scheduled. It
// returns nil for any other error, which the caller maps as it does today.
func Fail(err error) error {
	var reached *limits.ReachedError
	if errors.As(err, &reached) {
		return limits.Fail(reached)
	}
	if errors.Is(err, workspace.ErrNameMismatch) {
		return httpx.Fail(http.StatusBadRequest, NameMismatchResponse{Message: capitalize(err.Error()), Error: NameMismatchErrorBody{Code: "WORKSPACE_NAME_MISMATCH"}})
	}
	for _, answer := range answers {
		if errors.Is(err, answer.target) {
			return httpx.Fail(answer.status, utils.NewFailResponse(answer.message(err)))
		}
	}
	return nil
}

// answers is what Fail says for each error, the first that err is
var answers = []struct {
	target  error
	status  int
	message func(err error) string
}{
	{workspace.ErrNotFound, http.StatusNotFound, said("Workspace not found")},
	{workspace.ErrForbidden, http.StatusForbidden, said("Your role in this workspace doesn't allow this")},
	{workspace.ErrMemberNotFound, http.StatusNotFound, said("Member not found")},
	{workspace.ErrInviteNotFound, http.StatusNotFound, said("Invite not found")},
	{workspace.ErrInviteUnavailable, http.StatusGone, func(err error) string { return capitalize(err.Error()) }},
	{workspace.ErrInviteEmailMismatch, http.StatusForbidden, said("This invite is for another email address")},
	{workspace.ErrLastOwner, http.StatusConflict, said("A workspace needs at least one owner")},
	{workspace.ErrAlreadyMember, http.StatusConflict, said("Already a member of this workspace")},
	{workspace.ErrInvalidName, http.StatusBadRequest, said("The workspace needs a name")},
	{workspace.ErrDeletionNotScheduled, http.StatusNotFound, said("No deletion is scheduled for this workspace")},
	{workspace.ErrInvalidRole, http.StatusBadRequest, said("The role must be owner, admin or member")},
}

// said is the message that does not depend on the error
func said(message string) func(error) string {
	return func(error) string { return message }
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
