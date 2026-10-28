// Package workspacehttp turns the workspace of a request, and the errors of its role checks, into
// HTTP answers (docs/adr/0005 section 8). It sits beside the workspace package because that one
// imports no other module.
package workspacehttp

import (
	"context"
	"errors"
	"net/http"

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

// Fail turns the error of a role check into its answer: 404 for no workspace, 403 for a role that
// is too low. It returns nil for any other error, which the caller maps as it does today.
func Fail(err error) error {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		return NotFound()
	case errors.Is(err, workspace.ErrForbidden):
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse("Your role in this workspace doesn't allow this"))
	}
	return nil
}
