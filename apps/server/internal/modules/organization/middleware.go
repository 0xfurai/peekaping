package organization

import (
	"net/http"
	"vigi/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Middleware struct {
	orgService Service
	logger     *zap.SugaredLogger
}

func NewMiddleware(
	orgService Service,
	logger *zap.SugaredLogger,
) *Middleware {
	return &Middleware{
		orgService: orgService,
		logger:     logger.Named("[organization-middleware]"),
	}
}

// RequireOrganization checks for X-Organization-ID header and verifies membership
func (m *Middleware) RequireOrganization() gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID := c.GetHeader("X-Organization-ID")
		if orgID == "" {
			c.JSON(http.StatusBadRequest, utils.NewFailResponse("X-Organization-ID header is required"))
			c.Abort()
			return
		}

		// UserID is set by the previous AuthChain middleware (JWT or ApiKey)
		userID := c.GetString("userId")
		if userID == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("User not authenticated"))
			c.Abort()
			return
		}

		// Verify membership
		membership, err := m.orgService.FindMembership(c.Request.Context(), orgID, userID)
		if err != nil {
			if err.Error() == "sql: no rows in result set" {
				c.JSON(http.StatusForbidden, utils.NewFailResponse("You are not a member of this organization"))
				c.Abort()
				return
			}
			m.logger.Errorw("Failed to check organization membership", "orgID", orgID, "userID", userID, "error", err)
			c.JSON(http.StatusInternalServerError, utils.NewFailResponse("Internal server error"))
			c.Abort()
			return
		}

		if membership == nil {
			c.JSON(http.StatusForbidden, utils.NewFailResponse("You are not a member of this organization"))
			c.Abort()
			return
		}

		// Set organization context
		c.Set("orgId", orgID)
		c.Set("orgRole", string(membership.Role))

		c.Next()
	}
}
