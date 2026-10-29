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
// for an invite that can't be used, 400 for a role that doesn't exist. It returns nil for any
// other error, which the caller maps as it does today.
func Fail(err error) error {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		return NotFound()
	case errors.Is(err, workspace.ErrForbidden):
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse("Your role in this workspace doesn't allow this"))
	case errors.Is(err, workspace.ErrMemberNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Member not found"))
	case errors.Is(err, workspace.ErrInviteNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Invite not found"))
	case errors.Is(err, workspace.ErrInviteUnavailable):
		return httpx.Fail(http.StatusGone, utils.NewFailResponse(capitalize(err.Error())))
	case errors.Is(err, workspace.ErrInviteEmailMismatch):
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse("This invite is for another email address"))
	case errors.Is(err, workspace.ErrLastOwner):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("A workspace needs at least one owner"))
	case errors.Is(err, workspace.ErrAlreadyMember):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("Already a member of this workspace"))
	case errors.Is(err, workspace.ErrInvalidRole):
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("The role must be owner, admin or member"))
	}
	return nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
