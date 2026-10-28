package middleware

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	// Provide authentication chain
	// The API key routes take their authentication as an interface, since this package imports them
	if err := utils.Provide(container, func(chain *AuthChain) api_key.RouteAuth { return chain }); err != nil {
		return err
	}
	return utils.Provide(container, func(
		jwtMiddleware *auth.MiddlewareProvider,
		apiKeyMiddleware *api_key.MiddlewareProvider,
		workspaces workspace.Repository,
		users auth.Repository,
		trustedProxies *httpx.TrustedProxies,
		logger *zap.SugaredLogger,
	) *AuthChain {
		return NewAuthChain(jwtMiddleware, apiKeyMiddleware, workspaces, users, trustedProxies, logger)
	})
}
