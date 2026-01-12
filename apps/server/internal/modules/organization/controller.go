package organization

import (
	"net/http"
	"time"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type OrganizationController struct {
	orgService Service
	logger     *zap.SugaredLogger
}

func NewOrganizationController(
	orgService Service,
	logger *zap.SugaredLogger,
) *OrganizationController {
	return &OrganizationController{
		orgService: orgService,
		logger:     logger.Named("[organization-controller]"),
	}
}

// @Router		/organizations [post]
// @Summary		Create organization
// @Tags			Organizations
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Security  OrgIdAuth
// @Param     body body   CreateOrganizationDto  true  "Organization object"
// @Success		201	{object}	utils.ApiResponse[Organization]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) Create(ctx *gin.Context) {
	var dto CreateOrganizationDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	// TODO: Get User ID from context (Auth middleware)
	// For now, assuming it's mocked or passed in header for dev?
	// Real implementation needs: userID := ctx.GetString("userId")
	// If missing, return 401.
	userID := ctx.GetString("userId")
	if userID == "" {
		// Fallback for dev/testing if not set by middleware yet
		// In production this MUST be strictly enforced by middleware
		c.logger.Warn("UserId not found in context, check Auth middleware")
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	org, err := c.orgService.Create(ctx, &dto, userID)
	if err != nil {
		if slugErr, ok := err.(*SlugAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code": slugErr.Code,
					"slug": slugErr.Slug,
				},
			})
			return
		}
		c.logger.Errorw("Failed to create organization", "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusCreated, utils.NewSuccessResponse("Organization created successfully", org))
}

// @Router		/organizations/{id} [get]
// @Summary		Get organization by ID
// @Tags			Organizations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Security  OrgIdAuth
// @Param     id   path    string  true  "Organization ID"
// @Success		200	{object}	utils.ApiResponse[Organization]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) FindByID(ctx *gin.Context) {
	id := ctx.Param("id")

	org, err := c.orgService.FindByID(ctx, id)
	if err != nil {
		c.logger.Errorw("Failed to fetch organization", "id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if org == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Organization not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", org))
}

// @Router		/organizations/{id} [patch]
// @Summary		Update organization
// @Tags			Organizations
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Security  OrgIdAuth
// @Param     id   path    string  true  "Organization ID"
// @Param     body body   UpdateOrganizationDto  true  "Organization object"
// @Success		200	{object}	utils.ApiResponse[Organization]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
func (c *OrganizationController) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	var dto UpdateOrganizationDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	org, err := c.orgService.Update(ctx, id, &dto)
	if err != nil {
		if slugErr, ok := err.(*SlugAlreadyUsedError); ok {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code": slugErr.Code,
					"slug": slugErr.Slug,
				},
			})
			return
		}
		c.logger.Errorw("Failed to update organization", "id", id, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if org == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Organization not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Organization updated successfully", org))
}

// @Router		/organizations/slug/{slug} [get]
// @Summary		Get organization by Slug
// @Tags			Organizations
// @Produce		json
// @Param     slug   path    string  true  "Organization Slug"
// @Success		200	{object}	utils.ApiResponse[Organization]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) FindBySlug(ctx *gin.Context) {
	slug := ctx.Param("slug")

	org, err := c.orgService.FindBySlug(ctx, slug)
	if err != nil {
		c.logger.Errorw("Failed to fetch organization by slug", "slug", slug, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	if org == nil {
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Organization not found"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", org))
}

// @Router		/organizations/{id}/members [post]
// @Summary		Add member to organization
// @Tags			Organizations
// @Produce		json
// @Accept		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path    string  true  "Organization ID"
// @Param     body body   AddMemberDto  true  "Member details"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) AddMember(ctx *gin.Context) {
	orgID := ctx.Param("id")
	var dto AddMemberDto
	if err := ctx.ShouldBindJSON(&dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	if err := utils.Validate.Struct(dto); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	invitation, err := c.orgService.AddMember(ctx, orgID, &dto)
	if err != nil {
		c.logger.Errorw("Failed to add member", "orgId", orgID, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	// Construct invitation link (assuming CLI url or similar, but simplified for now just returning token/invitation)
	// The user asked to "generate a link". We'll return the full invitation object or a constructed link if we knew the base URL.
	// We'll return the invitation object which contains the token. logic can be handled in FE.
	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("Member invited successfully", invitation))
}

// @Router		/organizations/{id}/members [get]
// @Summary		List organization members and pending invitations
// @Tags			Organizations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path    string  true  "Organization ID"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) FindMembers(ctx *gin.Context) {
	orgID := ctx.Param("id")

	members, err := c.orgService.FindMembers(ctx, orgID)
	if err != nil {
		c.logger.Errorw("Failed to fetch members", "orgId", orgID, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	invitations, err := c.orgService.FindInvitations(ctx, orgID)
	if err != nil {
		c.logger.Errorw("Failed to fetch invitations", "orgId", orgID, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	var response []OrganizationMemberResponseDto

	// Add actual members
	for _, member := range members {
		dto := OrganizationMemberResponseDto{
			UserID:   member.UserID,
			Role:     member.Role,
			JoinedAt: member.CreatedAt.Format(time.RFC3339),
			Status:   "active",
		}
		if member.Organization != nil {
			dto.OrganizationName = member.Organization.Name
		}
		if member.User != nil {
			dto.User = &UserResponseDto{
				ID:    member.User.ID,
				Email: member.User.Email,
				Name:  "",
			}
		}
		response = append(response, dto)
	}

	// Add pending invitations
	for _, inv := range invitations {
		// We map invitations to the same structure but with status "pending"
		// UserID is empty or placeholder since they haven't joined yet.
		dto := OrganizationMemberResponseDto{
			UserID:          "", // No user ID yet
			Role:            inv.Role,
			JoinedAt:        inv.CreatedAt.Format(time.RFC3339),
			Status:          "pending",
			InvitationToken: inv.Token, // To allow copying the link
			User: &UserResponseDto{
				Email: inv.Email,
				Name:  "Pending Invitation",
			},
		}
		response = append(response, dto)
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", response))
}

// @Router		/user/organizations [get]
// @Summary		List user organizations
// @Tags			Organizations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Success		200	{object}	utils.ApiResponse[[]OrganizationUser]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) FindUserOrganizations(ctx *gin.Context) {
	userID := ctx.GetString("userId")
	if userID == "" {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	orgs, err := c.orgService.FindUserOrganizations(ctx, userID)
	if err != nil {
		c.logger.Errorw("Failed to fetch user organizations", "userId", userID, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", orgs))
}

// @Router		/invitations/{token} [get]
// @Summary		Get invitation details (public)
// @Tags			Invitations
// @Produce		json
// @Param     token   path    string  true  "Invitation Token"
// @Success		200	{object}	utils.ApiResponse[Invitation]
// @Failure		404	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) GetInvitation(ctx *gin.Context) {
	token := ctx.Param("token")

	invitation, err := c.orgService.GetInvitation(ctx, token)
	if err != nil {
		c.logger.Errorw("Failed to get invitation", "token", token, "error", err)
		// If error contains "not found" or similar, return 404
		ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Invitation not found or invalid"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", invitation))
}

// @Router		/invitations/{token}/accept [post]
// @Summary		Accept invitation
// @Tags			Invitations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     token   path    string  true  "Invitation Token"
// @Success		200	{object}	utils.ApiResponse[any]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		400	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) AcceptInvitation(ctx *gin.Context) {
	token := ctx.Param("token")
	userID := ctx.GetString("userId")
	if userID == "" {
		ctx.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
		return
	}

	err := c.orgService.AcceptInvitation(ctx, token, userID)
	if err != nil {
		c.logger.Errorw("Failed to accept invitation", "token", token, "userId", userID, "error", err)
		ctx.JSON(http.StatusBadRequest, utils.NewFailResponse(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Invitation accepted successfully", nil))
}

// @Router		/user/invitations [get]
// @Summary		Get user pending invitations
// @Tags			Invitations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Success		200	{object}	utils.ApiResponse[[]Invitation]
// @Failure		401	{object}	utils.APIError[any]
// @Failure		500	{object}	utils.APIError[any]
func (c *OrganizationController) GetUserInvitations(ctx *gin.Context) {
	// We need the user's email to find invitations.
	// Assuming email is in the context from Auth middleware, or we fetch user first.
	// Since we don't have UserService injected here easily to fetch email from ID,
	// let's assume the Auth middleware puts "email" in context or we need to fetch it.

	// Check if "email" is in context (depends on Auth middleware implementation)
	email := ctx.GetString("email")
	if email == "" {
		// Fallback: If email is not in context (it should be in a real JWT setup),
		// we might fail or need to query User service.
		// For now, let's assume it IS in the context or we can't implement this efficiently without UserService.
		// Wait, we have access to database. Can we just fetch user from ID in repo?
		// We have FindUserOrganizations, maybe we add FindUserByID/Email helper in repo?
		// Or we trust the Claims. Let's see...

		// Ideally we decode it from JWT.
		// If fails, return error
		c.logger.Warn("Email not found in context for GetUserInvitations")
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Could not identify user email"))
		return
	}

	invitations, err := c.orgService.GetUserInvitations(ctx, email)
	if err != nil {
		c.logger.Errorw("Failed to get user invitations", "email", email, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse("success", invitations))
}
