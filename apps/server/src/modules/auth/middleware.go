package auth

import (
	"net/http"
	"peekaping/src/modules/user_workspace"
	"peekaping/src/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

// MiddlewareProvider holds all middleware functions
type MiddlewareProvider struct {
	tokenMaker           *TokenMaker
	userWorkspaceService user_workspace.Service
}

// NewMiddlewareProvider creates a new middleware provider
func NewMiddlewareProvider(tokenMaker *TokenMaker, userWorkspaceService user_workspace.Service) *MiddlewareProvider {
	return &MiddlewareProvider{
		tokenMaker:           tokenMaker,
		userWorkspaceService: userWorkspaceService,
	}
}

// Auth is a middleware that verifies the JWT access token
func (p *MiddlewareProvider) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Add Bearer prefix if not present
		if !strings.HasPrefix(authHeader, "Bearer ") {
			authHeader = "Bearer " + authHeader
		}

		// Check if the header has the Bearer prefix
		fields := strings.Fields(authHeader)
		if len(fields) != 2 || fields[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid authorization header format"))
			c.Abort()
			return
		}

		// Extract the token
		accessToken := fields[1]

		// Verify the token
		claims, err := p.tokenMaker.VerifyToken(c, accessToken, "access")
		if err != nil {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired token"))
			c.Abort()
			return
		}

		// Check if it's an access token
		if claims.Type != "access" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid token type"))
			c.Abort()
			return
		}

		// Set user information in the context
		c.Set("userId", claims.UserID)
		c.Set("email", claims.Email)

		c.Next()
	}
}

// AuthWithWorkspace is a middleware that verifies JWT token and validates workspace access
func (p *MiddlewareProvider) AuthWithWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		// First run the standard auth middleware logic
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Authorization header is required"))
			c.Abort()
			return
		}

		// Add Bearer prefix if not present
		if !strings.HasPrefix(authHeader, "Bearer ") {
			authHeader = "Bearer " + authHeader
		}

		// Check if the header has the Bearer prefix
		fields := strings.Fields(authHeader)
		if len(fields) != 2 || fields[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid authorization header format"))
			c.Abort()
			return
		}

		// Extract the token
		accessToken := fields[1]

		// Verify the token
		claims, err := p.tokenMaker.VerifyToken(c, accessToken, "access")
		if err != nil {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid or expired token"))
			c.Abort()
			return
		}

		// Check if it's an access token
		if claims.Type != "access" {
			c.JSON(http.StatusUnauthorized, utils.NewFailResponse("Invalid token type"))
			c.Abort()
			return
		}

		// Get the workspace header
		workspaceID := c.GetHeader("x-peekaping-workspace")
		if workspaceID == "" {
			c.JSON(http.StatusBadRequest, utils.NewFailResponse("x-peekaping-workspace header is required"))
			c.Abort()
			return
		}

		// Validate user has access to the workspace
		userID := claims.UserID
		hasAccess, err := p.userWorkspaceService.IsUserInWorkspace(c.Request.Context(), userID, workspaceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to validate workspace access"))
			c.Abort()
			return
		}

		if !hasAccess {
			c.JSON(http.StatusForbidden, utils.NewFailResponse("User does not have access to this workspace"))
			c.Abort()
			return
		}

		// Set user and workspace information in the context
		c.Set("userId", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("workspaceId", workspaceID)

		c.Next()
	}
}
