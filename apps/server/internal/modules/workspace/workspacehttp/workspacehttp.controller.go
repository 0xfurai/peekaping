package workspacehttp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/ratelimit"
	"peekaping/internal/utils"
	"peekaping/internal/validation"

	"go.uber.org/zap"
)

// Controller answers the routes for a workspace's people, its invites and new workspaces
type Controller struct {
	service workspace.Service
	// usage reports the workspace's limits and what it has of each (PP-160)
	usage  *limits.Usage
	logger *zap.SugaredLogger
	now    func() time.Time

	// The limits on POST /workspace/invites, which sends an email to any address: per caller (a
	// user, or the API keys of a workspace) and per recipient. They count in this process's memory (see package ratelimit).
	inviteByCaller, inviteByRecipient *ratelimit.Limiter
}

// The most invites per hour that one caller makes, and that one address gets
const (
	maxInvitesPerCaller    = 20
	maxInvitesPerRecipient = 3
)

func NewController(service workspace.Service, usage *limits.Usage, logger *zap.SugaredLogger) *Controller {
	return &Controller{
		service: service, usage: usage, logger: logger.Named("[workspace-controller]"), now: time.Now,
		inviteByCaller:    ratelimit.New(maxInvitesPerCaller, time.Hour),
		inviteByRecipient: ratelimit.New(maxInvitesPerRecipient, time.Hour),
	}
}

// inviteCaller is what the per-caller limit counts an invite under: the user, across all their
// workspaces, or for an API key its workspace. An admin makes as many keys as they like, so every
// key of a workspace shares one count.
func inviteCaller(ctx context.Context) string {
	if id, _ := httpx.Value(ctx, "userId").(string); id != "" {
		return "user:" + id
	}
	if id, _ := httpx.Value(ctx, "apiKeyId").(string); id != "" {
		// The key runs in its own workspace, which the invite was authorized in
		workspaceID, _, _ := workspace.FromContext(ctx)
		return "workspace:" + string(workspaceID)
	}
	return "unknown"
}

// fail turns the error of the service into its answer, and a failure that is not one of the rules
// into 500
func (c *Controller) fail(err error) error {
	if answer := Fail(err); answer != nil {
		return answer
	}
	c.logger.Errorw("A workspace request failed", "error", err)
	return httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
}

// signedInUser is the ID of the user a JWT request comes from
func signedInUser(ctx context.Context) (string, error) {
	id, _ := httpx.Value(ctx, "userId").(string)
	if id == "" {
		return "", httpx.Fail(http.StatusUnauthorized, utils.NewFailResponse("Unauthorized"))
	}
	return id, nil
}

// decode reads and validates the body of a request
func decode[B any](input interface{ Decode() (B, error) }) (B, error) {
	body, err := input.Decode()
	if err != nil {
		return body, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request body"))
	}
	if n, ok := any(&body).(interface{ normalize() }); ok {
		n.normalize()
	}
	if err := validation.Validate.Struct(body); err != nil {
		return body, httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Invalid request: "+err.Error()))
	}
	return body, nil
}

// MARK: Members

func (c *Controller) ListMembers(ctx context.Context, _ *ListMembersInput) (*httpx.Output[utils.ApiResponse[[]*MemberResponse]], error) {
	members, err := c.service.ListMembers(ctx)
	if err != nil {
		return nil, c.fail(err)
	}
	out := make([]*MemberResponse, len(members))
	for i, m := range members {
		out[i] = memberResponse(m)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Members retrieved successfully", out))
}

// GetLimits answers the workspace's limits, where they come from and what it has of each, for any
// member of it and its API keys
func (c *Controller) GetLimits(ctx context.Context, _ *GetLimitsInput) (*httpx.Output[utils.ApiResponse[*limits.Report]], error) {
	id, err := Scope(ctx)
	if err != nil {
		return nil, err
	}
	// An API key has no user, and owns no workspace
	userID, _ := httpx.Value(ctx, "userId").(string)
	report, err := c.usage.Report(ctx, id, userID)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Limits retrieved successfully", report))
}

func (c *Controller) ChangeRole(ctx context.Context, input *ChangeRoleInput) (*httpx.Output[utils.ApiResponse[*MemberResponse]], error) {
	body, err := decode(input)
	if err != nil {
		return nil, err
	}
	member, err := c.service.ChangeRole(ctx, input.Request.PathValue("userId"), workspace.Role(body.Role))
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Role changed successfully", memberResponse(*member)))
}

func (c *Controller) RemoveMember(ctx context.Context, input *RemoveMemberInput) (*httpx.Output[httpx.NoBody], error) {
	if err := c.service.RemoveMember(ctx, input.Request.PathValue("userId")); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

func (c *Controller) Leave(ctx context.Context, _ *LeaveInput) (*httpx.Output[httpx.NoBody], error) {
	userID, err := signedInUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.Leave(ctx, userID); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

// MARK: Deletion

func (c *Controller) ScheduleDeletion(ctx context.Context, input *ScheduleDeletionInput) (*httpx.Output[utils.ApiResponse[*ScheduledDeletionResponse]], error) {
	userID, err := signedInUser(ctx)
	if err != nil {
		return nil, err
	}
	body, err := decode(input)
	if err != nil {
		return nil, err
	}
	at, err := c.service.ScheduleDeletion(ctx, userID, body.Name)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Workspace deletion scheduled successfully", &ScheduledDeletionResponse{DeletionScheduledAt: at}))
}

func (c *Controller) CancelDeletion(ctx context.Context, _ *CancelDeletionInput) (*httpx.Output[httpx.NoBody], error) {
	userID, err := signedInUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.CancelDeletion(ctx, userID); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

// MARK: Invites

func (c *Controller) ListInvites(ctx context.Context, _ *ListInvitesInput) (*httpx.Output[utils.ApiResponse[[]InviteResponse]], error) {
	invites, err := c.service.ListInvites(ctx)
	if err != nil {
		return nil, c.fail(err)
	}
	now := c.now()
	out := make([]InviteResponse, len(invites))
	for i, invite := range invites {
		out[i] = inviteResponse(invite, now)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Invites retrieved successfully", out))
}

func (c *Controller) CreateInvite(ctx context.Context, input *CreateInviteInput) (*httpx.Output[utils.ApiResponse[*CreatedInviteResponse]], error) {
	body, err := decode(input)
	if err != nil {
		return nil, err
	}
	// A caller that may not invite is refused before the limits, so that it uses up no address's
	// quota: that would keep the admins of other workspaces from inviting the address
	if err := workspace.RequireInvite(ctx, workspace.Role(body.Role)); err != nil {
		return nil, c.fail(err)
	}
	// Every invite emails its address, so a caller and an address are each limited
	if allowed, retryAfter := c.inviteByCaller.Allow(inviteCaller(ctx)); !allowed {
		return nil, httpx.TooManyRequests("Too many invites, try again later", retryAfter)
	}
	if allowed, retryAfter := c.inviteByRecipient.Allow(strings.ToLower(strings.TrimSpace(body.Email))); !allowed {
		return nil, httpx.TooManyRequests("This address was invited too often, try again later", retryAfter)
	}
	// An API key has no user: its invites have no inviter
	userID, _ := httpx.Value(ctx, "userId").(string)
	created, err := c.service.CreateInvite(ctx, userID, body.Email, workspace.Role(body.Role))
	if err != nil {
		return nil, c.fail(err)
	}
	out := &CreatedInviteResponse{InviteResponse: inviteResponse(created.Invite, c.now()), Token: created.Token, Link: created.Link}
	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Invite created successfully", out))
}

func (c *Controller) RevokeInvite(ctx context.Context, input *RevokeInviteInput) (*httpx.Output[httpx.NoBody], error) {
	if err := c.service.RevokeInvite(ctx, input.Request.PathValue("id")); err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusNoContent, httpx.NoBody{})
}

func (c *Controller) PreviewInvite(ctx context.Context, input *PreviewInviteInput) (*httpx.Output[utils.ApiResponse[*InvitePreviewResponse]], error) {
	preview, err := c.service.PreviewInvite(ctx, input.Request.PathValue("token"))
	if err != nil {
		return nil, c.fail(err)
	}
	out := &InvitePreviewResponse{
		WorkspaceName: preview.WorkspaceName, Email: preview.Email, Role: string(preview.Role),
		Valid: preview.Status == workspace.InvitePending, Status: string(preview.Status),
	}
	reply, err := httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Invite retrieved successfully", out))
	if err != nil {
		return nil, err
	}
	// The answer is for the holder of one token and names an email
	noStore := "no-store"
	reply.CacheControl = &noStore
	return reply, nil
}

func (c *Controller) AcceptInvite(ctx context.Context, input *AcceptInviteInput) (*httpx.Output[utils.ApiResponse[*WorkspaceResponse]], error) {
	userID, err := signedInUser(ctx)
	if err != nil {
		return nil, err
	}
	joined, err := c.service.AcceptInvite(ctx, input.Request.PathValue("token"), userID)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Invite accepted successfully", workspaceResponse(joined)))
}

// MARK: Workspaces

func (c *Controller) CreateWorkspace(ctx context.Context, input *CreateWorkspaceInput) (*httpx.Output[utils.ApiResponse[*WorkspaceResponse]], error) {
	userID, err := signedInUser(ctx)
	if err != nil {
		return nil, err
	}
	body, err := decode(input)
	if err != nil {
		return nil, err
	}
	created, err := c.service.CreateWorkspace(ctx, userID, body.Name)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusCreated, utils.NewSuccessResponse("Workspace created successfully", workspaceResponse(created)))
}

func (c *Controller) RenameWorkspace(ctx context.Context, input *RenameWorkspaceInput) (*httpx.Output[utils.ApiResponse[*WorkspaceResponse]], error) {
	body, err := decode(input)
	if err != nil {
		return nil, err
	}
	renamed, err := c.service.RenameWorkspace(ctx, body.Name)
	if err != nil {
		return nil, c.fail(err)
	}
	return httpx.Reply(http.StatusOK, utils.NewSuccessResponse("Workspace renamed successfully", workspaceResponse(renamed)))
}
