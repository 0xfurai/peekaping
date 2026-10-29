package admin

import (
	"context"
	"errors"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/utils"

	"go.uber.org/zap"
)

// maxPageSize is the most users or workspaces one page holds
const maxPageSize = 100

type Controller struct {
	service  Service
	settings *InstallSettings
	logger   *zap.SugaredLogger
}

func NewController(service Service, settings *InstallSettings, logger *zap.SugaredLogger) *Controller {
	return &Controller{service: service, settings: settings, logger: logger.Named("[admin-controller]")}
}

// actor is the ID of the instance admin the request comes from. The route's middleware let only a
// signed-in instance admin through, so a request with none is refused.
func actor(ctx context.Context) (string, error) {
	id, _ := httpx.Value(ctx, "userId").(string)
	if id == "" {
		return "", httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
	}
	return id, nil
}

// fail turns the error of the service into its answer: 404 for a user that is not there, 409 for
// the rules (not oneself, the last instance admin, the last owner of a workspace), and 500 for
// anything else
func (c *Controller) fail(err error) error {
	var lastOwner *LastOwnerError
	switch {
	case errors.Is(err, ErrUserNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("User not found"))
	case errors.Is(err, ErrWorkspaceNotFound):
		return httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Workspace not found"))
	case errors.Is(err, ErrSelf):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("You can't do this to your own account"))
	case errors.Is(err, ErrLastInstanceAdmin):
		return httpx.Fail(http.StatusConflict, utils.NewFailResponse("The install needs at least one instance admin"))
	case errors.As(err, &lastOwner):
		workspaces := make([]LastOwnerWorkspace, len(lastOwner.Workspaces))
		for i, ref := range lastOwner.Workspaces {
			workspaces[i] = LastOwnerWorkspace{ID: string(ref.ID), Name: ref.Name}
		}
		return httpx.Fail(http.StatusConflict, LastOwnerErrorResponse{Error: LastOwnerErrorBody{Code: "LAST_OWNER", Workspaces: workspaces}})
	}
	c.logger.Errorw("An admin request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

func (c *Controller) ListUsers(ctx context.Context, input *ListUsersInput) (*httpx.Output[utils.ApiResponse[*UsersPageResponse]], error) {
	r := input.Request
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
	}
	limit, err := utils.GetQueryInt(r, "limit", 20)
	if err != nil || limit < 1 || limit > maxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
	}
	users, total, err := c.service.ListUsers(ctx, r.URL.Query().Get("q"), page, limit)
	if err != nil {
		return nil, c.fail(err)
	}
	out := make([]*UserResponse, len(users))
	for i, u := range users {
		out[i] = userResponse(u)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Users retrieved successfully", &UsersPageResponse{Users: out, Total: total, Page: page, Limit: limit}))
}

func (c *Controller) UpdateUser(ctx context.Context, input *UpdateUserInput) (*httpx.Output[utils.ApiResponse[*UserResponse]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	body, err := input.Decode()
	if err != nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	if body.InstanceAdmin == nil && body.Active == nil {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Name what to change: instanceAdmin or active"))
	}
	user, err := c.service.UpdateUser(ctx, actorID, input.Request.PathValue("id"), UserUpdate{InstanceAdmin: body.InstanceAdmin, Active: body.Active})
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("User updated successfully", userResponse(user)))
}

func (c *Controller) CreateResetLink(ctx context.Context, input *CreateResetLinkInput) (*httpx.Output[utils.ApiResponse[*ResetLinkResponse]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	link, err := c.service.CreateResetLink(ctx, actorID, input.Request.PathValue("id"))
	if err != nil {
		return nil, c.fail(err)
	}
	out, err := httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Reset link created successfully", &ResetLinkResponse{Link: link.Link, ExpiresAt: link.ExpiresAt}))
	if err != nil {
		return nil, err
	}
	// The link is a secret that is shown once
	noStore := "no-store"
	out.CacheControl = &noStore
	return out, nil
}

func (c *Controller) DisableTwoFA(ctx context.Context, input *DisableTwoFAInput) (*httpx.Output[utils.ApiResponse[*UserResponse]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	user, err := c.service.DisableTwoFA(ctx, actorID, input.Request.PathValue("id"))
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("2FA turned off successfully", userResponse(user)))
}

func (c *Controller) DeleteUser(ctx context.Context, input *DeleteUserInput) (*httpx.Output[httpx.NoBody], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.DeleteUser(ctx, actorID, input.Request.PathValue("id")); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

func (c *Controller) ListWorkspaces(ctx context.Context, input *ListWorkspacesInput) (*httpx.Output[utils.ApiResponse[*WorkspacesPageResponse]], error) {
	r := input.Request
	page, err := utils.GetQueryInt(r, "page", 0)
	if err != nil || page < 0 {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid page parameter"))
	}
	limit, err := utils.GetQueryInt(r, "limit", 20)
	if err != nil || limit < 1 || limit > maxPageSize {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid limit parameter"))
	}
	workspaces, total, err := c.service.ListWorkspaces(ctx, r.URL.Query().Get("q"), page, limit)
	if err != nil {
		return nil, c.fail(err)
	}
	out := make([]*WorkspaceResponse, len(workspaces))
	for i, w := range workspaces {
		out[i] = workspaceResponse(w)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Workspaces retrieved successfully", &WorkspacesPageResponse{Workspaces: out, Total: total, Page: page, Limit: limit}))
}

func (c *Controller) UpdateWorkspace(ctx context.Context, input *UpdateWorkspaceInput) (*httpx.Output[utils.ApiResponse[*WorkspaceResponse]], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	body, err := input.Decode()
	if err != nil || (body.Suspended == nil && body.Limits == nil && !body.ClearLimits) {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Name what to change: suspended, limits or clearLimits"))
	}
	if body.Limits != nil && body.ClearLimits {
		return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Send limits or clearLimits, not both"))
	}
	id := input.Request.PathValue("id")
	var updated *Workspace
	if body.Limits != nil || body.ClearLimits {
		var own *limits.Limits
		if body.Limits != nil {
			l, err := body.Limits.limits()
			if err != nil {
				return nil, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
			}
			own = &l
		}
		if updated, err = c.service.SetWorkspaceLimits(ctx, actorID, id, own); err != nil {
			return nil, c.fail(err)
		}
	}
	if body.Suspended != nil {
		if updated, err = c.service.SetWorkspaceSuspended(ctx, actorID, id, *body.Suspended); err != nil {
			return nil, c.fail(err)
		}
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Workspace updated successfully", workspaceResponse(updated)))
}

func (c *Controller) DeleteWorkspace(ctx context.Context, input *DeleteWorkspaceInput) (*httpx.Output[httpx.NoBody], error) {
	actorID, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.DeleteWorkspace(ctx, actorID, input.Request.PathValue("id")); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}
