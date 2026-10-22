package middleware

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	// Provide authentication chain
	return utils.Provide(container, func(
		jwtMiddleware *auth.MiddlewareProvider,
		apiKeyMiddleware *api_key.MiddlewareProvider,
		trustedProxies *httpx.TrustedProxies,
		logger *zap.SugaredLogger,
	) *AuthChain {
		return NewAuthChain(jwtMiddleware, apiKeyMiddleware, trustedProxies, logger)
	})
}
