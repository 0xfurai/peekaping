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

	err := c.orgService.AddMember(ctx, orgID, &dto)
	if err != nil {
		c.logger.Errorw("Failed to add member", "orgId", orgID, "error", err)
		ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
		return
	}

	ctx.JSON(http.StatusOK, utils.NewSuccessResponse[any]("Member added successfully", nil))
}

// @Router		/organizations/{id}/members [get]
// @Summary		List organization members
// @Tags			Organizations
// @Produce		json
// @Security  JwtAuth
// @Security  ApiKeyAuth
// @Param     id   path    string  true  "Organization ID"
// @Success		200	{object}	utils.ApiResponse[[]OrganizationUser]
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

	var response []OrganizationMemberResponseDto
	for _, member := range members {
		dto := OrganizationMemberResponseDto{
			UserID:   member.UserID,
			Role:     member.Role,
			JoinedAt: member.CreatedAt.Format(time.RFC3339),
		}
		if member.Organization != nil {
			dto.OrganizationName = member.Organization.Name
		}
		if member.User != nil {
			dto.User = &UserResponseDto{
				ID:    member.User.ID,
				Email: member.User.Email,
				Name:  "", // Placeholder until User has Name field
			}
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
